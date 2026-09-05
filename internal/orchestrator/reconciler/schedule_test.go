package reconciler

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/reugn/go-quartz/quartz"
)

// ─── parseTrigger ─────────────────────────────────────────────────────────────

func TestParseTriggerDuration(t *testing.T) {
	trig, err := parseTrigger("10m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := trig.(*quartz.SimpleTrigger); !ok {
		t.Errorf("expected SimpleTrigger, got %T", trig)
	}
}

func TestParseTriggerCron(t *testing.T) {
	trig, err := parseTrigger("0 0/10 * * * ?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := trig.(*quartz.CronTrigger); !ok {
		t.Errorf("expected CronTrigger, got %T", trig)
	}
}

func TestParseTriggerErrors(t *testing.T) {
	cases := []struct {
		input   string
		wantErr string
	}{
		{"not-valid", "neither a valid cron expression nor a Go duration"},
		{"0s", "must be positive"},
		{"-1m", "must be positive"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			_, err := parseTrigger(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.input)
			}
			if tc.wantErr != "" && !containsStr(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// ─── partitionOffset ──────────────────────────────────────────────────────────

func TestPartitionOffsetZeroJitter(t *testing.T) {
	if got := partitionOffset("any-name", 0, time.Hour); got != 0 {
		t.Errorf("jitter=0: got %v, want 0", got)
	}
}

func TestPartitionOffsetDeterministic(t *testing.T) {
	a := partitionOffset("my-partition", 20, time.Hour)
	b := partitionOffset("my-partition", 20, time.Hour)
	if a != b {
		t.Errorf("not deterministic: first=%v second=%v", a, b)
	}
}

func TestPartitionOffsetBounded(t *testing.T) {
	const jitterPct = 20
	maxInterval := time.Hour
	upperBound := time.Duration(float64(maxInterval) * jitterPct / 100.0)
	for _, name := range []string{"a", "b", "my-partition", "foo-bar", "z"} {
		got := partitionOffset(name, jitterPct, maxInterval)
		if got < 0 || got >= upperBound {
			t.Errorf("partition=%s offset %v out of [0, %v)", name, got, upperBound)
		}
	}
}

func TestPartitionOffsetSpread(t *testing.T) {
	// Different partition names should (very likely) produce different offsets.
	seen := make(map[time.Duration]bool)
	for i := 0; i < 30; i++ {
		seen[partitionOffset(fmt.Sprintf("partition-%d", i), 50, time.Hour)] = true
	}
	if len(seen) < 20 {
		t.Errorf("poor spread: %d distinct offsets from 30 partitions", len(seen))
	}
}

// ─── intervalDuration ────────────────────────────────────────────────────────

func TestIntervalDurationValid(t *testing.T) {
	if got := intervalDuration("5m"); got != 5*time.Minute {
		t.Errorf("got %v, want 5m", got)
	}
}

func TestIntervalDurationInvalid(t *testing.T) {
	if got := intervalDuration("not-a-duration"); got != 0 {
		t.Errorf("expected 0 for invalid input, got %v", got)
	}
}

// ─── offsetTrigger ────────────────────────────────────────────────────────────

func TestOffsetTriggerFirstFireAnchorsToNow(t *testing.T) {
	inner := quartz.NewSimpleTrigger(10 * time.Minute)
	offset := 2 * time.Minute
	trig := &offsetTrigger{inner: inner, offset: offset}

	before := time.Now().Add(offset - time.Second)
	got, err := trig.NextFireTime(0) // prev=0 simulates stale/zero value
	after := time.Now().Add(offset + time.Second)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotTime := time.Unix(0, got)
	if gotTime.Before(before) || gotTime.After(after) {
		t.Errorf("first fire = %v, want between %v and %v", gotTime, before, after)
	}
}

func TestOffsetTriggerDelegatesAfterFirstFire(t *testing.T) {
	inner := quartz.NewSimpleTrigger(10 * time.Minute)
	trig := &offsetTrigger{inner: inner, offset: time.Minute}

	first, _ := trig.NextFireTime(0)
	second, err := trig.NextFireTime(first)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// SimpleTrigger returns prev + interval
	expected := first + (10 * time.Minute).Nanoseconds()
	if second != expected {
		t.Errorf("second fire = %v, want %v", second, expected)
	}
}

func TestOffsetTriggerOnlyOneFirstFire(t *testing.T) {
	// Concurrent calls must result in exactly one "first fire" result and
	// all others delegating to the inner trigger.
	inner := quartz.NewSimpleTrigger(10 * time.Minute)
	const offset = time.Minute
	trig := &offsetTrigger{inner: inner, offset: offset}

	const n = 20
	results := make([]int64, n)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			v, _ := trig.NextFireTime(0)
			results[idx] = v
		}(i)
	}
	wg.Wait()

	// Count results that fall in the "now + offset" window (±2s).
	now := time.Now()
	firstCount := 0
	for _, r := range results {
		rt := time.Unix(0, r)
		if rt.After(now.Add(offset-2*time.Second)) && rt.Before(now.Add(offset+2*time.Second)) {
			firstCount++
		}
	}
	if firstCount != 1 {
		t.Errorf("expected exactly 1 first-fire result, got %d", firstCount)
	}
}

// ─── agingBoost / effectiveTime ───────────────────────────────────────────────

func TestAgingBoostValues(t *testing.T) {
	if priorityCritical.agingBoost() != 2*time.Hour {
		t.Errorf("Critical boost = %v, want 2h", priorityCritical.agingBoost())
	}
	if priorityHigh.agingBoost() != time.Hour {
		t.Errorf("High boost = %v, want 1h", priorityHigh.agingBoost())
	}
	if priorityNormal.agingBoost() != 0 {
		t.Errorf("Normal boost = %v, want 0", priorityNormal.agingBoost())
	}
}

func TestItemHeapOrdering(t *testing.T) {
	now := time.Now()
	h := &itemHeap{}
	heap.Init(h)
	heap.Push(h, &reconcileItem{partition: "normal", prio: priorityNormal, submittedAt: now})
	heap.Push(h, &reconcileItem{partition: "critical", prio: priorityCritical, submittedAt: now})
	heap.Push(h, &reconcileItem{partition: "high", prio: priorityHigh, submittedAt: now})

	want := []string{"critical", "high", "normal"}
	for i, w := range want {
		got := heap.Pop(h).(*reconcileItem).partition
		if got != w {
			t.Errorf("pop[%d]: got %q, want %q", i, got, w)
		}
	}
}

func TestItemHeapAgingBoostNormalBeatsHighAfterTwoHours(t *testing.T) {
	// A Normal item that has waited >2h should beat a freshly submitted High item.
	now := time.Now()
	aged := &reconcileItem{
		partition:   "aged-normal",
		prio:        priorityNormal,
		submittedAt: now.Add(-2*time.Hour - time.Second),
	}
	fresh := &reconcileItem{
		partition:   "fresh-high",
		prio:        priorityHigh,
		submittedAt: now,
	}
	if aged.effectiveTime() >= fresh.effectiveTime() {
		t.Errorf(
			"aged Normal effectiveTime (%v) should be less than fresh High (%v)",
			aged.effectiveTime(), fresh.effectiveTime(),
		)
	}
}

// ─── priorityPool ─────────────────────────────────────────────────────────────

func TestPriorityPoolBasic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := newPriorityPool(2)
	pool.start(ctx)

	var ran int32
	done := make(chan struct{})
	pool.Submit("p", priorityNormal, func(ctx context.Context) error {
		atomic.AddInt32(&ran, 1)
		close(done)
		return nil
	})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("work never executed")
	}
	pool.Shutdown()
	if atomic.LoadInt32(&ran) != 1 {
		t.Errorf("expected 1 execution, got %d", ran)
	}
}

func TestPriorityPoolOrdering(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := newPriorityPool(1) // single slot to enforce serialised execution
	pool.start(ctx)

	// Occupy the single slot with a blocker.
	slotTaken := make(chan struct{})
	release := make(chan struct{})
	pool.Submit("blocker", priorityNormal, func(ctx context.Context) error {
		close(slotTaken)
		<-release
		return nil
	})
	<-slotTaken // slot is now full; dispatch is blocking on the semaphore

	// Enqueue all three pending items while the slot is held.
	done := make(chan string, 3)
	pool.Submit("b-normal", priorityNormal, func(ctx context.Context) error {
		done <- "b-normal"
		return nil
	})
	pool.Submit("d-high", priorityHigh, func(ctx context.Context) error {
		done <- "d-high"
		return nil
	})
	pool.Submit("c-critical", priorityCritical, func(ctx context.Context) error {
		done <- "c-critical"
		return nil
	})

	close(release) // let blocker finish → dispatch pops from heap

	want := []string{"c-critical", "d-high", "b-normal"}
	for i, w := range want {
		select {
		case got := <-done:
			if got != w {
				t.Errorf("execution[%d]: got %q, want %q", i, got, w)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for execution[%d]", i)
		}
	}
	pool.Shutdown()
}

func TestPriorityPoolPriorityUpgrade(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := newPriorityPool(1)
	pool.start(ctx)

	slotTaken := make(chan struct{})
	release := make(chan struct{})
	pool.Submit("blocker", priorityNormal, func(ctx context.Context) error {
		close(slotTaken)
		<-release
		return nil
	})
	<-slotTaken

	done := make(chan string, 2)
	// Submit "target" as Normal, then "other" also Normal.
	pool.Submit("target", priorityNormal, func(ctx context.Context) error {
		done <- "target"
		return nil
	})
	pool.Submit("other", priorityNormal, func(ctx context.Context) error {
		done <- "other"
		return nil
	})
	// Upgrade "target" to Critical — should now beat "other".
	// The fn argument is ignored (existing fn is preserved; only priority changes).
	pool.Submit("target", priorityCritical, func(ctx context.Context) error {
		done <- "target-upgraded-fn" // should never be sent
		return nil
	})

	close(release)

	first := <-done
	if first != "target" {
		t.Errorf("expected upgraded 'target' to run first, got %q", first)
	}
	pool.Shutdown()
}

func TestPriorityPoolDeduplicationWhileRunning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := newPriorityPool(1)
	pool.start(ctx)

	var executions int32
	started := make(chan struct{})
	release := make(chan struct{})

	pool.Submit("p", priorityNormal, func(ctx context.Context) error {
		close(started)
		<-release
		atomic.AddInt32(&executions, 1)
		return nil
	})
	<-started

	// Same partition submitted while it is running — must be skipped.
	pool.Submit("p", priorityNormal, func(ctx context.Context) error {
		atomic.AddInt32(&executions, 1)
		return nil
	})

	close(release)
	pool.Shutdown()

	if n := atomic.LoadInt32(&executions); n != 1 {
		t.Errorf("expected 1 execution, got %d", n)
	}
}

func TestPriorityPoolShutdownWaitsForInflight(t *testing.T) {
	ctx := context.Background()
	pool := newPriorityPool(2)
	pool.start(ctx)

	var ran int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})

	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("p%d", i)
		pool.Submit(name, priorityNormal, func(ctx context.Context) error {
			started <- struct{}{}
			<-release
			atomic.AddInt32(&ran, 1)
			return nil
		})
	}
	<-started
	<-started // both workers are running

	shutdownDone := make(chan struct{})
	go func() {
		pool.Shutdown()
		close(shutdownDone)
	}()

	close(release)

	select {
	case <-shutdownDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown() timed out")
	}
	if n := atomic.LoadInt32(&ran); n != 2 {
		t.Errorf("expected 2 executions, got %d", n)
	}
}

func TestPriorityPoolShutdownRejectsNewWork(t *testing.T) {
	ctx := context.Background()
	pool := newPriorityPool(1)
	pool.start(ctx)
	pool.Shutdown()

	var ran int32
	pool.Submit("p", priorityNormal, func(ctx context.Context) error {
		atomic.AddInt32(&ran, 1)
		return nil
	})
	// Brief pause; if closed correctly, the fn should never run.
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&ran) != 0 {
		t.Error("pool accepted work after Shutdown()")
	}
}

func TestPriorityPoolContextCancellationStopsDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pool := newPriorityPool(1)
	pool.start(ctx)

	cancel() // cancel before submitting any work

	// Shutdown should return promptly because the dispatch goroutine exits.
	done := make(chan struct{})
	go func() {
		pool.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown() blocked after context cancel")
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
