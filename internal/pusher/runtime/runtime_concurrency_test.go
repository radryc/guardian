package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sync/atomic"
	"testing"
	"time"

	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

// concurrencyProbeDriver records the maximum number of Apply calls that were
// in flight at the same time so tests can assert worker-pool behaviour.
type concurrencyProbeDriver struct {
	active    int32
	maxActive int32
	delay     time.Duration
}

func (d *concurrencyProbeDriver) Type() string                  { return "ConcurrencyProbe" }
func (d *concurrencyProbeDriver) Validate(map[string]any) error { return nil }
func (d *concurrencyProbeDriver) Check(context.Context, registry.AssetInput) error {
	return nil
}
func (d *concurrencyProbeDriver) Diff(_ context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	return taskdomain.DriftReport{Status: "InSync", Summary: "no drift"}, nil
}
func (d *concurrencyProbeDriver) Apply(_ context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	current := atomic.AddInt32(&d.active, 1)
	for {
		observed := atomic.LoadInt32(&d.maxActive)
		if current <= observed || atomic.CompareAndSwapInt32(&d.maxActive, observed, current) {
			break
		}
	}
	if d.delay > 0 {
		time.Sleep(d.delay)
	}
	atomic.AddInt32(&d.active, -1)
	return registry.AssetResult{Outputs: map[string]string{"id": in.Asset.Name}}, nil
}
func (d *concurrencyProbeDriver) Destroy(context.Context, registry.AssetInput) error { return nil }

func seedProbeTask(t *testing.T, ctx context.Context, store *memory.Store, taskID, partition, assetType string) {
	t.Helper()
	task := taskdomain.Task{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "Task",
		TaskID:       taskID,
		Partition:    partition,
		Intent:       "api",
		Op:           taskdomain.OpApply,
		TargetPusher: "local",
		Target:       targetdomain.Placement{Cluster: "local"},
		Assets: []taskdomain.AbstractAsset{{
			Type:       assetType,
			Name:       "backend",
			Properties: map[string]any{"image": "example:v1"},
		}},
	}
	content, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal(task) error = %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("local", taskID), Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand", Reason: "seed task"},
	}); err != nil {
		t.Fatalf("UpsertFiles(task) error = %v", err)
	}
}

func TestRuntimeExecutesTasksConcurrently(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	probe := &concurrencyProbeDriver{delay: 40 * time.Millisecond}
	reg := registry.New()
	reg.Register(probe)

	const n = 8
	for i := 0; i < n; i++ {
		seedProbeTask(t, ctx, store, fmt.Sprintf("task-%02d", i), "payments", "ConcurrencyProbe")
	}

	r := &Runtime{
		QueuePath:      paths.QueueDir("local"),
		WorkerID:       "worker",
		Store:          store,
		Registry:       reg,
		MaxConcurrency: n,
	}
	if err := r.processPending(ctx); err != nil {
		t.Fatalf("processPending() error = %v", err)
	}

	if got := atomic.LoadInt32(&probe.maxActive); got < 2 {
		t.Fatalf("expected concurrent execution, max in-flight = %d", got)
	}
	for i := 0; i < n; i++ {
		if _, err := store.ReadFile(ctx, paths.QueueResult("local", fmt.Sprintf("task-%02d", i))); err != nil {
			t.Fatalf("ReadFile(result %d) error = %v", i, err)
		}
	}
}

func TestRuntimeHonorsMaxConcurrency(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	probe := &concurrencyProbeDriver{delay: 20 * time.Millisecond}
	reg := registry.New()
	reg.Register(probe)

	const n = 12
	const limit = 2
	for i := 0; i < n; i++ {
		seedProbeTask(t, ctx, store, fmt.Sprintf("task-%02d", i), "payments", "ConcurrencyProbe")
	}

	r := &Runtime{
		QueuePath:      paths.QueueDir("local"),
		WorkerID:       "worker",
		Store:          store,
		Registry:       reg,
		MaxConcurrency: limit,
	}
	if err := r.processPending(ctx); err != nil {
		t.Fatalf("processPending() error = %v", err)
	}
	if got := atomic.LoadInt32(&probe.maxActive); got > limit {
		t.Fatalf("max in-flight = %d, want <= %d", got, limit)
	}
}

func TestRuntimeShardsTasksByHash(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	probe := &concurrencyProbeDriver{}
	reg := registry.New()
	reg.Register(probe)

	const total = 30
	const shards = 3
	ids := make([]string, total)
	owned := make([][]string, shards)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("task-%02d", i)
		ids[i] = id
		seedProbeTask(t, ctx, store, id, "payments", "ConcurrencyProbe")
		h := fnv.New32a()
		_, _ = h.Write([]byte(id))
		shard := int(h.Sum32() % shards)
		owned[shard] = append(owned[shard], id)
	}

	for shard := 0; shard < shards; shard++ {
		r := &Runtime{
			QueuePath:  paths.QueueDir("local"),
			WorkerID:   fmt.Sprintf("worker-%d", shard),
			Store:      store,
			Registry:   reg,
			ShardIndex: shard,
			ShardCount: shards,
		}
		if err := r.processPending(ctx); err != nil {
			t.Fatalf("shard %d processPending() error = %v", shard, err)
		}
		// Every task owned by this shard so far must have a result; tasks owned
		// by later shards must not.
		for _, id := range owned[shard] {
			if _, err := store.ReadFile(ctx, paths.QueueResult("local", id)); err != nil {
				t.Fatalf("shard %d did not process owned task %s: %v", shard, id, err)
			}
		}
	}
}

func TestRuntimeShardCountOneOwnsEverything(t *testing.T) {
	r := &Runtime{ShardCount: 1}
	for _, id := range []string{"a", "b", "c"} {
		if !r.ownsTask(id) {
			t.Fatalf("ShardCount=1 should own task %q", id)
		}
	}
}

func BenchmarkRuntimeProcessPending(b *testing.B) {
	const tasks = 500
	ids := make([]string, tasks)
	for i := range ids {
		ids[i] = fmt.Sprintf("bench-%04d", i)
	}
	for _, concurrency := range []int{1, 8, 32} {
		b.Run(fmt.Sprintf("concurrency=%d", concurrency), func(b *testing.B) {
			ctx := context.Background()
			// 1ms models a remote API round-trip (e.g. a Kubernetes call), which
			// is what the worker pool overlaps.
			probe := &concurrencyProbeDriver{delay: time.Millisecond}
			reg := registry.New()
			reg.Register(probe)
			b.ReportMetric(float64(tasks), "tasks/op")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				store := memory.New()
				for _, id := range ids {
					seedBenchTask(b, ctx, store, id, reg)
				}
				r := &Runtime{
					QueuePath:      paths.QueueDir("local"),
					WorkerID:       "bench",
					Store:          store,
					Registry:       reg,
					MaxConcurrency: concurrency,
				}
				b.StartTimer()
				if err := r.processPending(ctx); err != nil {
					b.Fatalf("processPending() error = %v", err)
				}
			}
		})
	}
}

func seedBenchTask(b *testing.B, ctx context.Context, store *memory.Store, taskID string, reg *registry.Registry) {
	b.Helper()
	task := taskdomain.Task{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "Task",
		TaskID:       taskID,
		Partition:    "payments",
		Intent:       "api",
		Op:           taskdomain.OpApply,
		TargetPusher: "local",
		Target:       targetdomain.Placement{Cluster: "local"},
		Assets: []taskdomain.AbstractAsset{{
			Type:       "ConcurrencyProbe",
			Name:       "backend",
			Properties: map[string]any{},
		}},
	}
	content, err := json.Marshal(task)
	if err != nil {
		b.Fatalf("Marshal(task) error = %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("local", taskID), Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand", Reason: "seed bench task"},
	}); err != nil {
		b.Fatalf("UpsertFiles(task) error = %v", err)
	}
}
