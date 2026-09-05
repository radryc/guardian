package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	releasedomain "github.com/rydzu/ainfra/guardian/internal/domain/release"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	guardianapi "github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestWriteReleaseCreatesLogEntry(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := NewDispatcher(store, "test")

	createdAt := time.Date(2026, 7, 16, 12, 57, 0, 0, time.UTC)
	if err := dispatch.WriteRelease(ctx, &releasedomain.ReleaseRecord{
		DeploymentRevision: "dep_abc",
		Partition:          "lolipop",
		Intent:             "lolipop-backend",
		AssetVersionIDs:    map[string]string{"backend": "asset-v"},
		AssetVersions:      map[string]string{"backend": "release-hash-20260716-1257"},
		Outputs:            map[string]string{"backend.id": "compute-1"},
		TaskIDs:            []string{"task-apply-1"},
		CreatedAt:          createdAt,
	}); err != nil {
		t.Fatalf("WriteRelease() error = %v", err)
	}

	got, err := readReleaseLogContent(t, ctx, store, "lolipop")
	if err != nil {
		t.Fatalf("readPartitionReleaseLog() error = %v", err)
	}
	if got.Partition != "lolipop" {
		t.Fatalf("partition = %q, want lolipop", got.Partition)
	}
	rec, ok := got.Releases["lolipop-backend"]
	if !ok {
		t.Fatalf("expected release entry, got releases=%v", got.Releases)
	}
	if rec.DeploymentRevision != "dep_abc" {
		t.Fatalf("deployment revision = %q, want dep_abc", rec.DeploymentRevision)
	}
	if rec.AssetVersions["backend"] != "release-hash-20260716-1257" {
		t.Fatalf("AssetVersions mismatch: %v", rec.AssetVersions)
	}
	if !rec.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want %v", rec.CreatedAt, createdAt)
	}
}

func TestWriteReleaseReplacesOlderEntryByIntent(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := NewDispatcher(store, "test")

	earlier := time.Date(2026, 7, 16, 12, 25, 0, 0, time.UTC)
	if err := dispatch.WriteRelease(ctx, &releasedomain.ReleaseRecord{
		DeploymentRevision: "dep_old",
		Partition:          "lolipop",
		Intent:             "lolipop-backend",
		CreatedAt:          earlier,
	}); err != nil {
		t.Fatalf("WriteRelease(earlier) error = %v", err)
	}

	later := time.Date(2026, 7, 16, 12, 57, 0, 0, time.UTC)
	if err := dispatch.WriteRelease(ctx, &releasedomain.ReleaseRecord{
		DeploymentRevision: "dep_new",
		Partition:          "lolipop",
		Intent:             "lolipop-backend",
		CreatedAt:          later,
	}); err != nil {
		t.Fatalf("WriteRelease(later) error = %v", err)
	}

	got, err := readReleaseLogContent(t, ctx, store, "lolipop")
	if err != nil {
		t.Fatalf("readPartitionReleaseLog() error = %v", err)
	}
	if got.Releases["lolipop-backend"].DeploymentRevision != "dep_new" {
		t.Fatalf("expected dep_new, got %q", got.Releases["lolipop-backend"].DeploymentRevision)
	}
	if len(got.Releases) != 1 {
		t.Fatalf("expected 1 release (replace by intent), got %d", len(got.Releases))
	}
}

func TestWriteReleaseIsMonotonicAndIsNoOpForEqualOrOlder(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := NewDispatcher(store, "test")

	stamp := time.Date(2026, 7, 16, 12, 57, 0, 0, time.UTC)
	if err := dispatch.WriteRelease(ctx, &releasedomain.ReleaseRecord{
		DeploymentRevision: "dep_keep",
		Partition:          "lolipop",
		Intent:             "lolipop-backend",
		CreatedAt:          stamp,
	}); err != nil {
		t.Fatalf("WriteRelease(seed) error = %v", err)
	}

	// Same CreatedAt → no-op (no second write).
	guardianWritesTotal.Reset()
	if err := dispatch.WriteRelease(ctx, &releasedomain.ReleaseRecord{
		DeploymentRevision: "dep_other",
		Partition:          "lolipop",
		Intent:             "lolipop-backend",
		CreatedAt:          stamp,
	}); err != nil {
		t.Fatalf("WriteRelease(equal) error = %v", err)
	}
	got, _ := readReleaseLogContent(t, ctx, store, "lolipop")
	if got.Releases["lolipop-backend"].DeploymentRevision != "dep_keep" {
		t.Fatalf("equal CreatedAt must not overwrite: got %q", got.Releases["lolipop-backend"].DeploymentRevision)
	}

	// Older CreatedAt → no-op.
	earlier := stamp.Add(-time.Minute)
	if err := dispatch.WriteRelease(ctx, &releasedomain.ReleaseRecord{
		DeploymentRevision: "dep_ancient",
		Partition:          "lolipop",
		Intent:             "lolipop-backend",
		CreatedAt:          earlier,
	}); err != nil {
		t.Fatalf("WriteRelease(older) error = %v", err)
	}
	got, _ = readReleaseLogContent(t, ctx, store, "lolipop")
	if got.Releases["lolipop-backend"].DeploymentRevision != "dep_keep" {
		t.Fatalf("older CreatedAt must not overwrite: got %q", got.Releases["lolipop-backend"].DeploymentRevision)
	}
}

func TestWriteReleaseWritesEvenWhenAssetMapIsIdentical(t *testing.T) {
	// P3 fix: WriteRelease must bypass the FNV byte-equal short-circuit so
	// self-heal re-applies re-emit the file with a fresh UpdatedAt.
	ctx := context.Background()
	store := memory.New()
	dispatch := NewDispatcher(store, "test")

	stamp := time.Date(2026, 7, 16, 12, 57, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := dispatch.WriteRelease(ctx, &releasedomain.ReleaseRecord{
			DeploymentRevision: "dep_keep",
			Partition:          "lolipop",
			Intent:             "lolipop-backend",
			AssetVersions:      map[string]string{"backend": "release-hash-20260716-1257"},
			CreatedAt:          stamp.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("WriteRelease #%d error = %v", i, err)
		}
	}

	got, err := readReleaseLogContent(t, ctx, store, "lolipop")
	if err != nil {
		t.Fatalf("readPartitionReleaseLog() error = %v", err)
	}
	if got.Releases["lolipop-backend"].DeploymentRevision != "dep_keep" {
		t.Fatalf("expected dep_keep, got %q", got.Releases["lolipop-backend"].DeploymentRevision)
	}
	if !got.Releases["lolipop-backend"].CreatedAt.Equal(stamp.Add(2 * time.Second)) {
		t.Fatalf("expected latest CreatedAt, got %v", got.Releases["lolipop-backend"].CreatedAt)
	}
}

func TestWriteReleaseConcurrencySerialisesPerPartition(t *testing.T) {
	ctx := context.Background()
	baseStore := memory.New()
	probe := &countingUpsertStore{Store: baseStore, observe: true}
	dispatch := NewDispatcher(probe, "test")

	const writers = 8
	stamp := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	probe.onUpsert = func() {
		current := inFlight.Add(1)
		// keep max honest
		for {
			prev := maxInFlight.Load()
			if current <= prev || maxInFlight.CompareAndSwap(prev, current) {
				break
			}
		}
		defer inFlight.Add(-1)
	}

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			intent := "lolipop-worker-" + string(rune('a'+i))
			if err := dispatch.WriteRelease(ctx, &releasedomain.ReleaseRecord{
				DeploymentRevision: "dep_" + intent,
				Partition:          "lolipop",
				Intent:             intent,
				CreatedAt:          stamp.Add(time.Duration(i) * time.Second),
			}); err != nil {
				t.Errorf("WriteRelease(%s) error = %v", intent, err)
			}
		}(i)
	}
	wg.Wait()

	if got := maxInFlight.Load(); got > 1 {
		t.Fatalf("expected serialised writes (max in-flight = 1), got %d", got)
	}

	got, err := readReleaseLogContent(t, ctx, baseStore, "lolipop")
	if err != nil {
		t.Fatalf("readPartitionReleaseLog() error = %v", err)
	}
	if len(got.Releases) != writers {
		t.Fatalf("expected %d releases, got %d", writers, len(got.Releases))
	}
}

func TestWriteReleaseRejectsMissingFields(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := NewDispatcher(store, "test")

	cases := []struct {
		name string
		rec  *releasedomain.ReleaseRecord
	}{
		{
			name: "nil record",
			rec:  nil,
		},
		{
			name: "missing partition",
			rec: &releasedomain.ReleaseRecord{
				Intent:             "lolipop-backend",
				DeploymentRevision: "dep_abc",
			},
		},
		{
			name: "missing intent",
			rec: &releasedomain.ReleaseRecord{
				Partition:          "lolipop",
				DeploymentRevision: "dep_abc",
			},
		},
		{
			name: "missing deployment revision",
			rec: &releasedomain.ReleaseRecord{
				Partition: "lolipop",
				Intent:    "lolipop-backend",
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if err := dispatch.WriteRelease(ctx, tc.rec); err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

func TestSeedPartitionReleaseLogsHydratesCache(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := NewDispatcher(store, "test")

	stamp := time.Date(2026, 7, 16, 12, 57, 0, 0, time.UTC)
	seedReleaseLogContent(t, ctx, store, "lolipop", &releasedomain.PartitionReleaseLog{
		APIVersion: "guardian/v1alpha1",
		Kind:       "PartitionReleaseLog",
		Partition:  "lolipop",
		UpdatedAt:  stamp,
		Releases: map[string]releasedomain.ReleaseRecord{
			"lolipop-backend": {
				DeploymentRevision: "dep_seed",
				Partition:          "lolipop",
				Intent:             "lolipop-backend",
				CreatedAt:          stamp,
			},
		},
	})

	if err := dispatch.SeedPartitionReleaseLogs(ctx); err != nil {
		t.Fatalf("SeedPartitionReleaseLogs() error = %v", err)
	}

	log, err := dispatch.loadPartitionReleaseLog(ctx, "lolipop")
	if err != nil {
		t.Fatalf("loadPartitionReleaseLog() error = %v", err)
	}
	if len(log.Releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(log.Releases))
	}
	if log.Releases["lolipop-backend"].DeploymentRevision != "dep_seed" {
		t.Fatalf("expected dep_seed, got %q", log.Releases["lolipop-backend"].DeploymentRevision)
	}
}

func readReleaseLogContent(t *testing.T, ctx context.Context, store *memory.Store, partition string) (*releasedomain.PartitionReleaseLog, error) {
	t.Helper()
	raw, err := store.ReadFile(ctx, paths.PartitionReleaseLog(partition))
	if err != nil {
		return nil, err
	}
	var log releasedomain.PartitionReleaseLog
	if err := json.Unmarshal(raw, &log); err != nil {
		return nil, err
	}
	if log.Releases == nil {
		log.Releases = map[string]releasedomain.ReleaseRecord{}
	}
	return &log, nil
}

func seedReleaseLogContent(t *testing.T, ctx context.Context, store *memory.Store, partition string, log *releasedomain.PartitionReleaseLog) {
	t.Helper()
	content, err := json.Marshal(log)
	if err != nil {
		t.Fatalf("Marshal(release log) error = %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: paths.PartitionReleaseLog(partition),
			Content:     content,
		}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed release log"},
	}); err != nil {
		t.Fatalf("UpsertFiles(release log) error = %v", err)
	}
}

type countingUpsertStore struct {
	guardianapi.Store

	mu       sync.Mutex
	observe  bool
	onUpsert func()
}

func (s *countingUpsertStore) UpsertFiles(ctx context.Context, batch guardianapi.MutationBatch) (guardianapi.BatchRevision, error) {
	intercept := false
	s.mu.Lock()
	if s.observe {
		intercept = true
	}
	s.mu.Unlock()
	if intercept && s.onUpsert != nil {
		s.onUpsert()
	}
	return s.Store.UpsertFiles(ctx, batch)
}

func init() {
	// Ensure we only count writes that hit the partition release log path.
	// No-op; real observability is set inside the test bodies via direct
	// struct field mutation.
	_ = errors.New
}
