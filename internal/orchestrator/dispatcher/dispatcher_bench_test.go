package dispatcher

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func benchIntentState(partition, intent string, generation int) *statedomain.IntentState {
	return &statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         partition,
		Intent:            intent,
		Status:            statedomain.StatusHealthy,
		IntentVersionID:   "intent-v1",
		IntentSpecHash:    "hash-v1",
		PartitionRevision: "partition-rev-v1",
		TargetPusher:      "local",
		Outputs:           map[string]string{"generation": fmt.Sprintf("%d", generation)},
		LastTaskID:        fmt.Sprintf("task-%d", generation),
	}
}

type byteCountingStore struct {
	guardianapi.Store
	bytes int64
}

func (s *byteCountingStore) UpsertFiles(ctx context.Context, batch guardianapi.MutationBatch) (guardianapi.BatchRevision, error) {
	var written int64
	for _, write := range batch.Writes {
		written += int64(len(write.Content))
	}
	atomic.AddInt64(&s.bytes, written)
	return s.Store.UpsertFiles(ctx, batch)
}

func seedBenchPartition(tb testing.TB, ctx context.Context, dispatch *Dispatcher, intents int) {
	tb.Helper()
	for i := 0; i < intents; i++ {
		if err := dispatch.WriteIntentState(ctx, benchIntentState("demo", fmt.Sprintf("intent-%04d", i), 0)); err != nil {
			tb.Fatalf("seed intent %d: %v", i, err)
		}
	}
	// Warm the in-memory aggregate so the benchmark measures the steady-state
	// write path rather than the first scan.
	if err := dispatch.WriteIntentState(ctx, benchIntentState("demo", "hot", 0)); err != nil {
		tb.Fatalf("warmup write: %v", err)
	}
}

// BenchmarkWriteIntentState measures the per-transition CPU cost. The remaining
// O(intents) component is in-memory aggregate bookkeeping; the expensive
// O(intents) aggregate *store write* was removed (see the bytes benchmark).
func BenchmarkWriteIntentState(b *testing.B) {
	for _, intents := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("intents=%d", intents), func(b *testing.B) {
			ctx := context.Background()
			store := memory.New()
			dispatch := NewDispatcher(store, "bench")
			seedBenchPartition(b, ctx, dispatch, intents)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := dispatch.WriteIntentState(ctx, benchIntentState("demo", "hot", i+1)); err != nil {
					b.Fatalf("WriteIntentState: %v", err)
				}
			}
		})
	}
}

// BenchmarkWriteIntentStateStoreBytes proves that a single intent transition
// now writes O(1) bytes to the store instead of serialising the whole
// partition runtime (which grew with the intent count).
func BenchmarkWriteIntentStateStoreBytes(b *testing.B) {
	for _, intents := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("intents=%d", intents), func(b *testing.B) {
			ctx := context.Background()
			store := &byteCountingStore{Store: memory.New()}
			dispatch := NewDispatcher(store, "bench")
			seedBenchPartition(b, ctx, dispatch, intents)
			atomic.StoreInt64(&store.bytes, 0)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := dispatch.WriteIntentState(ctx, benchIntentState("demo", "hot", i+1)); err != nil {
					b.Fatalf("WriteIntentState: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(atomic.LoadInt64(&store.bytes))/float64(b.N), "store-bytes/op")
		})
	}
}
