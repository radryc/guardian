package dispatcher

import (
	"context"
	"sync"
	"testing"
	"time"

	statedomain "github.com/radryc/guardian/internal/domain/state"
	"github.com/radryc/guardian/internal/orchestrator/common"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func TestUpdateIntentStateMergesLatestObservation(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := NewDispatcher(store, "test")

	base := &statedomain.IntentState{
		APIVersion: "guardian/v1alpha1",
		Kind:       "IntentState",
		Partition:  "demo",
		Intent:     "app",
		Status:     statedomain.StatusDiffing,
		Outputs:    map[string]string{},
		Timestamps: statedomain.StateTimestamps{LastDiffAt: time.Now().UTC().Add(-time.Minute)},
	}
	if err := dispatch.WriteIntentState(ctx, base); err != nil {
		t.Fatalf("WriteIntentState: %v", err)
	}

	// The observed collector persists a fresh inventory after the task writer
	// loaded base.
	fresh := time.Now().UTC()
	observed := statedomain.CloneIntentState(base)
	observed.Resources = []statedomain.ResourceObservation{{LogicalID: "Cluster", AssetName: "res-cluster"}}
	observed.Timestamps.LastObservedAt = fresh
	if err := dispatch.WriteIntentState(ctx, observed); err != nil {
		t.Fatalf("WriteIntentState(observed): %v", err)
	}

	// The task writer merges its stale snapshot.
	incoming := statedomain.CloneIntentState(base)
	incoming.Status = statedomain.StatusHealthy
	updated, err := dispatch.UpdateIntentState(ctx, "demo", "app", func(latest *statedomain.IntentState) error {
		statedomain.MergeIntentState(latest, base, incoming)
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateIntentState: %v", err)
	}

	if updated.Status != statedomain.StatusHealthy {
		t.Fatalf("status = %q, want %q", updated.Status, statedomain.StatusHealthy)
	}
	if len(updated.Resources) != 1 || updated.Resources[0].AssetName != "res-cluster" {
		t.Fatalf("resources = %#v, want observation preserved", updated.Resources)
	}
	if !updated.Timestamps.LastObservedAt.Equal(fresh) {
		t.Fatalf("lastObservedAt = %s, want %s", updated.Timestamps.LastObservedAt, fresh)
	}

	persisted, err := common.LoadIntentState(ctx, store, "demo", "app")
	if err != nil {
		t.Fatalf("LoadIntentState: %v", err)
	}
	if len(persisted.Resources) != 1 || persisted.Status != statedomain.StatusHealthy {
		t.Fatalf("persisted = %#v, want merged state", persisted)
	}
}

func TestUpdateIntentStateSerializesConcurrentWriters(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := NewDispatcher(store, "test")

	base := &statedomain.IntentState{
		APIVersion: "guardian/v1alpha1",
		Kind:       "IntentState",
		Partition:  "demo",
		Intent:     "app",
		Status:     statedomain.StatusDiffing,
		Outputs:    map[string]string{},
	}
	if err := dispatch.WriteIntentState(ctx, base); err != nil {
		t.Fatalf("WriteIntentState: %v", err)
	}

	observedAt := time.Now().UTC()
	observation := statedomain.CloneIntentState(base)
	observation.Resources = []statedomain.ResourceObservation{{LogicalID: "Cluster", AssetName: "res-cluster"}}
	observation.Timestamps.LastObservedAt = observedAt

	task := statedomain.CloneIntentState(base)
	task.Status = statedomain.StatusHealthy

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := dispatch.UpdateIntentState(ctx, "demo", "app", func(latest *statedomain.IntentState) error {
			statedomain.MergeIntentState(latest, base, observation)
			return nil
		})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := dispatch.UpdateIntentState(ctx, "demo", "app", func(latest *statedomain.IntentState) error {
			statedomain.MergeIntentState(latest, base, task)
			return nil
		})
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("UpdateIntentState: %v", err)
		}
	}

	persisted, err := common.LoadIntentState(ctx, store, "demo", "app")
	if err != nil {
		t.Fatalf("LoadIntentState: %v", err)
	}
	if persisted.Status != statedomain.StatusHealthy {
		t.Fatalf("status = %q, want %q", persisted.Status, statedomain.StatusHealthy)
	}
	if len(persisted.Resources) != 1 || !persisted.Timestamps.LastObservedAt.Equal(observedAt) {
		t.Fatalf("persisted = %#v, want observation preserved", persisted)
	}
}

type conflictStore struct {
	guardianapi.Store
	mu            sync.Mutex
	conflicts     int
	readConflicts int
}

func (s *conflictStore) ReadFileVersioned(ctx context.Context, logicalPath string) ([]byte, guardianapi.FileVersion, bool, error) {
	s.mu.Lock()
	if s.readConflicts > 0 {
		s.readConflicts--
		s.mu.Unlock()
		return nil, guardianapi.FileVersion{}, false, guardianapi.ErrConflict
	}
	s.mu.Unlock()
	reader, ok := s.Store.(guardianapi.VersionedReader)
	if !ok {
		return nil, guardianapi.FileVersion{}, false, nil
	}
	return reader.ReadFileVersioned(ctx, logicalPath)
}

func (s *conflictStore) UpsertFiles(ctx context.Context, batch guardianapi.MutationBatch) (guardianapi.BatchRevision, error) {
	s.mu.Lock()
	if s.conflicts > 0 {
		s.conflicts--
		s.mu.Unlock()
		return guardianapi.BatchRevision{}, guardianapi.ErrConflict
	}
	s.mu.Unlock()
	return s.Store.UpsertFiles(ctx, batch)
}

func TestUpdateIntentStateCASRetriesConflict(t *testing.T) {
	ctx := context.Background()
	baseStore := memory.New()
	store := &conflictStore{Store: baseStore, conflicts: 2}
	dispatch := NewDispatcher(store, "test")

	state, err := dispatch.UpdateIntentState(ctx, "demo", "app", func(latest *statedomain.IntentState) error {
		latest.Status = statedomain.StatusHealthy
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateIntentState: %v", err)
	}
	if state.Status != statedomain.StatusHealthy {
		t.Fatalf("status = %q, want healthy", state.Status)
	}
	persisted, err := common.LoadIntentState(ctx, baseStore, "demo", "app")
	if err != nil {
		t.Fatalf("LoadIntentState: %v", err)
	}
	if persisted.Status != statedomain.StatusHealthy {
		t.Fatalf("persisted status = %q, want healthy", persisted.Status)
	}
}

func TestUpdateIntentStateCASRetriesReadConflict(t *testing.T) {
	ctx := context.Background()
	baseStore := memory.New()
	store := &conflictStore{Store: baseStore, readConflicts: 2}
	dispatch := NewDispatcher(store, "test")

	state, err := dispatch.UpdateIntentState(ctx, "demo", "app", func(latest *statedomain.IntentState) error {
		latest.Status = statedomain.StatusHealthy
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateIntentState: %v", err)
	}
	if state.Status != statedomain.StatusHealthy {
		t.Fatalf("status = %q, want healthy", state.Status)
	}
	persisted, err := common.LoadIntentState(ctx, baseStore, "demo", "app")
	if err != nil {
		t.Fatalf("LoadIntentState: %v", err)
	}
	if persisted.Status != statedomain.StatusHealthy {
		t.Fatalf("persisted status = %q, want healthy", persisted.Status)
	}
}

func TestDeleteIntentStateCASConflictRetries(t *testing.T) {
	ctx := context.Background()
	baseStore := memory.New()
	store := &deleteConflictStore{Store: baseStore}
	dispatch := NewDispatcher(store, "test")

	if _, err := dispatch.UpdateIntentState(ctx, "demo", "app", func(latest *statedomain.IntentState) error {
		latest.Status = statedomain.StatusHealthy
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := dispatch.DeleteIntentState(ctx, "demo", "app", "", "test"); err != nil {
		t.Fatalf("DeleteIntentState: %v", err)
	}
	if _, err := common.LoadIntentState(ctx, baseStore, "demo", "app"); err == nil {
		t.Fatal("intent state still present after delete")
	}
}

type deleteConflictStore struct {
	guardianapi.Store
	mu        sync.Mutex
	conflicts int
}

func (s *deleteConflictStore) DeletePaths(ctx context.Context, batch guardianapi.DeleteBatch) (guardianapi.BatchRevision, error) {
	s.mu.Lock()
	if s.conflicts == 0 {
		s.conflicts = 1
		s.mu.Unlock()
		return guardianapi.BatchRevision{}, guardianapi.ErrConflict
	}
	s.mu.Unlock()
	return s.Store.DeletePaths(ctx, batch)
}
