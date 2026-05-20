package reconciler

import (
	"container/heap"
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/reugn/go-quartz/quartz"
)

// ─── Priority ─────────────────────────────────────────────────────────────────

type priority int

const (
	priorityCritical priority = 0 // active rollout: Applying / Destroying
	priorityHigh     priority = 1 // unhealthy: Drifted / *Failed / DriftedLocked
	priorityNormal   priority = 2 // everything else
)

// agingBoost returns a virtual time subtracted from submittedAt when ordering
// heap items. Items waiting long enough in Normal/High become as urgent as
// freshly submitted Critical/High items respectively.
func (p priority) agingBoost() time.Duration {
	switch p {
	case priorityCritical:
		return 2 * time.Hour
	case priorityHigh:
		return 1 * time.Hour
	default:
		return 0
	}
}

// ─── Heap item ────────────────────────────────────────────────────────────────

type reconcileItem struct {
	partition   string
	prio        priority
	submittedAt time.Time
	fn          func(context.Context) error
	index       int // maintained by heap.Interface
}

// effectiveTime is the virtual clock value used for ordering; lower = more urgent.
func (it *reconcileItem) effectiveTime() int64 {
	return it.submittedAt.UnixNano() - it.prio.agingBoost().Nanoseconds()
}

type itemHeap []*reconcileItem

func (h itemHeap) Len() int           { return len(h) }
func (h itemHeap) Less(i, j int) bool { return h[i].effectiveTime() < h[j].effectiveTime() }
func (h itemHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *itemHeap) Push(x any) {
	n := len(*h)
	it := x.(*reconcileItem)
	it.index = n
	*h = append(*h, it)
}

func (h *itemHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	it.index = -1
	*h = old[:n-1]
	return it
}

// ─── Priority pool ────────────────────────────────────────────────────────────

// priorityPool is a bounded-concurrency pool that orders work by priority and
// age. A single dispatch goroutine acquires a semaphore slot BEFORE popping
// from the heap so the highest-priority item available at execution time is
// always chosen, preventing priority inversion.
type priorityPool struct {
	mu           sync.Mutex
	cond         *sync.Cond
	h            itemHeap
	pendingItems map[string]*reconcileItem // partition → queued-but-not-running
	running      map[string]bool           // partitions currently executing
	sem          chan struct{}
	closed       bool
	wg           sync.WaitGroup
}

func newPriorityPool(parallelism int) *priorityPool {
	if parallelism <= 0 {
		parallelism = 1
	}
	p := &priorityPool{
		pendingItems: make(map[string]*reconcileItem),
		running:      make(map[string]bool),
		sem:          make(chan struct{}, parallelism),
	}
	p.cond = sync.NewCond(&p.mu)
	heap.Init(&p.h)
	return p
}

func (p *priorityPool) start(ctx context.Context) {
	p.wg.Add(1)
	go p.dispatch(ctx)
}

// Submit enqueues a partition reconcile. If the partition is already queued
// its priority is upgraded if the new priority is higher. If already running
// the call is a no-op (the partition lock serializes; the next scheduled
// trigger will pick up any new drift).
func (p *priorityPool) Submit(partition string, prio priority, fn func(context.Context) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	if p.running[partition] {
		return
	}
	if existing, ok := p.pendingItems[partition]; ok {
		if prio < existing.prio {
			existing.prio = prio
			heap.Fix(&p.h, existing.index)
		}
		return
	}
	item := &reconcileItem{
		partition:   partition,
		prio:        prio,
		submittedAt: time.Now(),
		fn:          fn,
	}
	p.pendingItems[partition] = item
	heap.Push(&p.h, item)
	p.cond.Signal()
}

// dispatch is the single goroutine that feeds items to worker goroutines.
func (p *priorityPool) dispatch(ctx context.Context) {
	defer p.wg.Done()
	for {
		// Phase 1: wait until work is available.
		p.mu.Lock()
		for p.h.Len() == 0 && !p.closed {
			p.cond.Wait()
		}
		if p.closed {
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()

		// Phase 2: acquire a worker slot (blocks when pool is at capacity).
		// The mutex must NOT be held here to avoid deadlocking Submit.
		select {
		case p.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}

		// Phase 3: pop the highest-priority item now that we own a slot.
		// The heap may have changed while waiting for the slot.
		p.mu.Lock()
		if p.h.Len() == 0 {
			<-p.sem // nothing left; give the slot back and loop
			p.mu.Unlock()
			continue
		}
		item := heap.Pop(&p.h).(*reconcileItem)
		delete(p.pendingItems, item.partition)
		p.running[item.partition] = true
		p.mu.Unlock()

		// Phase 4: run in a dedicated goroutine so dispatch picks up the next
		// highest-priority item immediately without waiting.
		p.wg.Add(1)
		go func(it *reconcileItem) {
			defer p.wg.Done()
			defer func() {
				<-p.sem
				p.mu.Lock()
				delete(p.running, it.partition)
				p.mu.Unlock()
				p.cond.Signal() // wake dispatch if it is sleeping in Phase 1
			}()
			if err := it.fn(ctx); err != nil && ctx.Err() == nil {
				log.Printf("reconciler: pool worker error partition=%s: %v", it.partition, err)
			}
		}(item)
	}
}

// Shutdown stops accepting new work and waits for all in-flight workers.
func (p *priorityPool) Shutdown() {
	p.mu.Lock()
	p.closed = true
	p.cond.Broadcast()
	p.mu.Unlock()
	p.wg.Wait()
}

// ─── Trigger helpers ──────────────────────────────────────────────────────────

// offsetTrigger wraps any quartz.Trigger and delays the first fire by a fixed
// offset. Subsequent fires delegate to the inner trigger unchanged. The fired
// flag is guarded atomically for concurrent safety.
type offsetTrigger struct {
	inner  quartz.Trigger
	offset time.Duration
	fired  uint32 // atomic: 0 = first call pending, 1 = done
}

func (t *offsetTrigger) NextFireTime(prev int64) (int64, error) {
	if atomic.CompareAndSwapUint32(&t.fired, 0, 1) {
		// Anchor to real-now, ignoring prev, so a stale or zero value from
		// quartz internals doesn't produce a wrong first fire time.
		return time.Now().UnixNano() + t.offset.Nanoseconds(), nil
	}
	return t.inner.NextFireTime(prev)
}

func (t *offsetTrigger) Description() string {
	return fmt.Sprintf("offsetTrigger(offset=%s,inner=%s)", t.offset, t.inner.Description())
}

// parseTrigger parses s as a quartz cron expression first, then as a Go
// duration. Returns an error only if neither format is valid.
func parseTrigger(s string) (quartz.Trigger, error) {
	if ct, err := quartz.NewCronTrigger(s); err == nil {
		return ct, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, fmt.Errorf("interval %q is neither a valid cron expression nor a Go duration: %w", s, err)
	}
	if d <= 0 {
		return nil, fmt.Errorf("interval must be positive, got %q", s)
	}
	return quartz.NewSimpleTrigger(d), nil
}

// partitionOffset returns a deterministic, hash-derived initial-fire offset for
// a partition. This spreads scheduled jobs across the first interval window so
// all partitions don't fire simultaneously on startup.
func partitionOffset(name string, jitterPct int, maxInterval time.Duration) time.Duration {
	if jitterPct <= 0 || maxInterval <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	frac := float64(h.Sum32()%1000) / 1000.0 // 0.000 … 0.999
	jitter := float64(maxInterval) * float64(jitterPct) / 100.0
	return time.Duration(frac * jitter)
}

// intervalDuration parses s as a Go duration and returns it, or 0 on error.
func intervalDuration(s string) time.Duration {
	d, _ := time.ParseDuration(s)
	return d
}

// ─── Quartz job implementations ───────────────────────────────────────────────

// partitionJob submits a single partition to the priority pool on each trigger
// fire, using the current intent statuses to determine priority.
type partitionJob struct {
	name string
	r    *Reconciler
}

func (j *partitionJob) Execute(ctx context.Context) error {
	prio := j.r.partitionJobPriority(ctx, j.name)
	j.r.pool.Submit(j.name, prio, func(ctx context.Context) error {
		return j.r.ReconcilePartition(ctx, j.name, false)
	})
	return nil
}

func (j *partitionJob) Description() string { return "partition-reconcile(" + j.name + ")" }

// discoveryJob reconciles the set of scheduled quartz jobs against the live
// partition list, adding jobs for new partitions and removing jobs for deleted
// ones.
type discoveryJob struct {
	r *Reconciler
}

func (j *discoveryJob) Execute(ctx context.Context) error {
	return j.r.syncPartitionSchedules(ctx)
}

func (j *discoveryJob) Description() string { return "partition-discovery" }
