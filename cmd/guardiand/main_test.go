package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/dispatcher"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/results"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type countingListStore struct {
	guardianapi.Store

	mu        sync.Mutex
	listCalls map[string]int
}

func (s *countingListStore) ListDir(ctx context.Context, logicalDir string) ([]guardianapi.DirEntry, error) {
	s.mu.Lock()
	if s.listCalls == nil {
		s.listCalls = make(map[string]int)
	}
	s.listCalls[logicalDir]++
	s.mu.Unlock()
	return s.Store.ListDir(ctx, logicalDir)
}

func (s *countingListStore) ResetCounts() {
	s.mu.Lock()
	s.listCalls = make(map[string]int)
	s.mu.Unlock()
}

func (s *countingListStore) ListCount(logicalDir string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listCalls[logicalDir]
}

func TestProcessLiveResultFilesSkipsCompletedTerminalResults(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := dispatcher.NewDispatcher(store, "guardiand")
	processor := results.NewProcessor(store, dispatch)

	finishedAt := time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC)
	deploymentRevision := revisions.DeploymentRevisionID("partition-rev-v1", "intent-v1", finishedAt)

	seedJSONFile(t, ctx, store, paths.IntentState("payments", "api"), statedomain.IntentState{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "IntentState",
		Partition:          "payments",
		Intent:             "api",
		Status:             statedomain.StatusHealthy,
		IntentVersionID:    "intent-v1",
		IntentSpecHash:     "intent-spec-hash-v1",
		PartitionRevision:  "partition-rev-v1",
		DeploymentRevision: deploymentRevision,
		TargetPusher:       "local",
		Target:             targetdomain.Placement{Cluster: "local"},
		AssetVersionIDs:    map[string]string{"backend": "asset-v1"},
		AssetVersions:      map[string]string{"backend": "backend:v1"},
		Outputs:            map[string]string{"backend.id": "payments-api"},
		Drift:              &taskdomain.DriftReport{Status: "InSync", Summary: "apply completed"},
		LastTaskID:         "task-apply-1",
		Timestamps: statedomain.StateTimestamps{
			LastQueuedAt: finishedAt,
			LastApplyAt:  finishedAt,
		},
	})

	seedRawFile(t, ctx, store, paths.IntentManifest("payments", "api"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  targetPusher: local
  target:
    cluster: local
  assets:
    - type: Compute
      name: backend
      properties:
        image: api:v1
`))

	seedJSONFile(t, ctx, store, paths.QueueResult("local", "task-apply-1"), taskdomain.TaskResult{
		APIVersion: "guardian/v1alpha1",
		Kind:       "TaskResult",
		TaskID:     "task-apply-1",
		Op:         taskdomain.OpApply,
		Status:     taskdomain.ResultSucceeded,
		Partition:  "payments",
		Intent:     "api",
		Pusher:     "local",
		Outputs:    map[string]string{"backend.id": "payments-api"},
		FinishedAt: finishedAt,
	})

	before, err := store.ListDir(ctx, paths.StateEventsDir("payments"))
	if err != nil {
		before = nil
	}

	if _, err := processLiveResultFiles(ctx, store, processor, []string{"local"}, "test"); err != nil {
		t.Fatalf("processLiveResultFiles() error = %v", err)
	}

	after, err := store.ListDir(ctx, paths.StateEventsDir("payments"))
	if err != nil {
		after = nil
	}
	if len(after) != len(before) {
		t.Fatalf("completed terminal result was reprocessed; event count changed from %d to %d", len(before), len(after))
	}

	archiveEntries, err := store.ListDir(ctx, paths.ArchiveIntentRoot("payments", "api"))
	if err == nil && len(archiveEntries) != 0 {
		t.Fatalf("completed terminal result was reprocessed; found archive entries: %+v", archiveEntries)
	}
	if _, err := store.ReadFile(ctx, paths.QueueTask("local", "task-apply-1")); err == nil {
		t.Fatalf("periodic scan should not recreate queue task files for completed results")
	}
}

func TestProcessLiveResultFilesProcessesActiveResults(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := dispatcher.NewDispatcher(store, "guardiand")
	processor := results.NewProcessor(store, dispatch)

	finishedAt := time.Date(2026, time.May, 6, 12, 5, 0, 0, time.UTC)

	seedJSONFile(t, ctx, store, paths.IntentState("payments", "api"), statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         "payments",
		Intent:            "api",
		Status:            statedomain.StatusApplying,
		IntentVersionID:   "intent-v1",
		IntentSpecHash:    "intent-spec-hash-v1",
		PartitionRevision: "partition-rev-v1",
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "local"},
		AssetVersionIDs:   map[string]string{"backend": "asset-v1"},
		AssetVersions:     map[string]string{"backend": "backend:v1"},
		Outputs:           map[string]string{},
		LastTaskID:        "task-apply-2",
		Timestamps: statedomain.StateTimestamps{
			LastQueuedAt: finishedAt,
			LastApplyAt:  finishedAt,
		},
	})

	seedRawFile(t, ctx, store, paths.IntentManifest("payments", "api"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  targetPusher: local
  target:
    cluster: local
  assets:
    - type: Compute
      name: backend
      properties:
        image: api:v1
`))

	seedJSONFile(t, ctx, store, paths.QueueResult("local", "task-apply-2"), taskdomain.TaskResult{
		APIVersion: "guardian/v1alpha1",
		Kind:       "TaskResult",
		TaskID:     "task-apply-2",
		Op:         taskdomain.OpApply,
		Status:     taskdomain.ResultSucceeded,
		Partition:  "payments",
		Intent:     "api",
		Pusher:     "local",
		Outputs:    map[string]string{"backend.id": "payments-api"},
		FinishedAt: finishedAt,
	})

	if _, err := processLiveResultFiles(ctx, store, processor, []string{"local"}, "test"); err != nil {
		t.Fatalf("processLiveResultFiles() error = %v", err)
	}

	updatedState, err := store.ReadFile(ctx, paths.IntentState("payments", "api"))
	if err != nil {
		t.Fatalf("ReadFile(intent state) error = %v", err)
	}
	var state statedomain.IntentState
	if err := json.Unmarshal(updatedState, &state); err != nil {
		t.Fatalf("Unmarshal(intent state) error = %v", err)
	}
	if state.Status != statedomain.StatusHealthy {
		t.Fatalf("status = %q, want %q", state.Status, statedomain.StatusHealthy)
	}
	if state.DeploymentRevision == "" {
		t.Fatalf("expected deployment revision after processing active result")
	}

	events, err := store.ListDir(ctx, paths.StateEventsDir("payments"))
	if err != nil || len(events) == 0 {
		t.Fatalf("expected deployment event after processing active result, events=%v err=%v", events, err)
	}
	archiveEntries, err := store.ListDir(ctx, paths.ArchiveIntentRoot("payments", "api"))
	if err != nil || len(archiveEntries) == 0 {
		t.Fatalf("expected archive entries after processing active result, entries=%v err=%v", archiveEntries, err)
	}
}

func TestProcessLiveResultFilesUsesIntentStateReference(t *testing.T) {
	ctx := context.Background()
	baseStore := memory.New()
	store := &countingListStore{Store: baseStore}
	dispatch := dispatcher.NewDispatcher(store, "guardiand")
	processor := results.NewProcessor(store, dispatch)

	finishedAt := time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)
	if err := dispatch.WriteIntentState(ctx, &statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         "payments",
		Intent:            "api",
		Status:            statedomain.StatusApplying,
		IntentVersionID:   "intent-v1",
		IntentSpecHash:    "intent-spec-hash-v1",
		PartitionRevision: "partition-rev-v1",
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "local"},
		AssetVersionIDs:   map[string]string{"backend": "asset-v1"},
		AssetVersions:     map[string]string{"backend": "backend:v1"},
		LastTaskID:        "task-apply-3",
		Timestamps: statedomain.StateTimestamps{
			LastQueuedAt: finishedAt,
			LastApplyAt:  finishedAt,
		},
	}); err != nil {
		t.Fatalf("WriteIntentState() error = %v", err)
	}

	seedRawFile(t, ctx, store, paths.IntentManifest("payments", "api"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  targetPusher: local
  target:
    cluster: local
  assets:
    - type: Compute
      name: backend
      properties:
        image: api:v1
`))
	seedJSONFile(t, ctx, store, paths.QueueResult("local", "task-apply-3"), taskdomain.TaskResult{
		APIVersion: "guardian/v1alpha1",
		Kind:       "TaskResult",
		TaskID:     "task-apply-3",
		Op:         taskdomain.OpApply,
		Status:     taskdomain.ResultSucceeded,
		Partition:  "payments",
		Intent:     "api",
		Pusher:     "local",
		Outputs:    map[string]string{"backend.id": "payments-api"},
		FinishedAt: finishedAt,
	})

	store.ResetCounts()
	if _, err := processLiveResultFiles(ctx, store, processor, []string{"local"}, "test"); err != nil {
		t.Fatalf("processLiveResultFiles() error = %v", err)
	}
	if got := store.ListCount(paths.PartitionsRoot()); got != 0 {
		t.Fatalf("expected result scan to avoid listing all partitions, got %d calls", got)
	}
}

func TestPartitionWorkerIndexIsStableAndBounded(t *testing.T) {
	for _, partition := range []string{"a", "b", "payments", "orders"} {
		index := partitionWorkerIndex(partition, 8)
		if index < 0 || index >= 8 {
			t.Fatalf("partitionWorkerIndex(%q, 8) = %d, out of range", partition, index)
		}
		if again := partitionWorkerIndex(partition, 8); again != index {
			t.Fatalf("partitionWorkerIndex(%q) not stable: %d then %d", partition, index, again)
		}
	}
	if got := partitionWorkerIndex("anything", 1); got != 0 {
		t.Fatalf("partitionWorkerIndex(..., 1) = %d, want 0", got)
	}
}

func TestProcessLiveResultFilesProcessesManyResults(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := dispatcher.NewDispatcher(store, "guardiand")
	processor := results.NewProcessor(store, dispatch)

	const n = 40
	finishedAt := time.Date(2026, time.May, 6, 12, 30, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		partition := fmt.Sprintf("p%02d", i)
		taskID := fmt.Sprintf("task-%02d", i)
		seedJSONFile(t, ctx, store, paths.IntentState(partition, "api"), statedomain.IntentState{
			APIVersion:        "guardian/v1alpha1",
			Kind:              "IntentState",
			Partition:         partition,
			Intent:            "api",
			Status:            statedomain.StatusDestroying,
			IntentVersionID:   "intent-v1",
			IntentSpecHash:    "hash-v1",
			PartitionRevision: "partition-rev-v1",
			TargetPusher:      "local",
			Target:            targetdomain.Placement{Cluster: "local"},
			LastTaskID:        taskID,
			Timestamps:        statedomain.StateTimestamps{LastQueuedAt: finishedAt},
		})
		seedJSONFile(t, ctx, store, paths.QueueResult("local", taskID), taskdomain.TaskResult{
			APIVersion: "guardian/v1alpha1",
			Kind:       "TaskResult",
			TaskID:     taskID,
			Op:         taskdomain.OpDestroy,
			Status:     taskdomain.ResultSucceeded,
			Partition:  partition,
			Intent:     "api",
			Pusher:     "local",
			FinishedAt: finishedAt,
		})
	}

	count, err := processLiveResultFiles(ctx, store, processor, []string{"local"}, "test")
	if err != nil {
		t.Fatalf("processLiveResultFiles() error = %v", err)
	}
	if count != n {
		t.Fatalf("processed count = %d, want %d", count, n)
	}
	for i := 0; i < n; i++ {
		partition := fmt.Sprintf("p%02d", i)
		taskID := fmt.Sprintf("task-%02d", i)
		if _, err := store.ReadFile(ctx, paths.QueueResult("local", taskID)); err == nil {
			t.Fatalf("result for %s was not cleaned up", partition)
		}
		state, err := common.LoadIntentState(ctx, store, partition, "api")
		if err != nil {
			t.Fatalf("LoadIntentState(%s) error = %v", partition, err)
		}
		if state.Status != statedomain.StatusDestroyed {
			t.Fatalf("%s status = %q, want Destroyed", partition, state.Status)
		}
	}
}

func BenchmarkProcessLiveResultFiles(b *testing.B) {
	const n = 200
	finishedAt := time.Date(2026, time.May, 6, 12, 30, 0, 0, time.UTC)
	b.ReportMetric(float64(n), "results/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ctx := context.Background()
		store := memory.New()
		dispatch := dispatcher.NewDispatcher(store, "guardiand")
		processor := results.NewProcessor(store, dispatch)
		for j := 0; j < n; j++ {
			partition := fmt.Sprintf("p%03d", j)
			taskID := fmt.Sprintf("task-%03d", j)
			seedJSONFileB(b, ctx, store, paths.IntentState(partition, "api"), statedomain.IntentState{
				APIVersion:        "guardian/v1alpha1",
				Kind:              "IntentState",
				Partition:         partition,
				Intent:            "api",
				Status:            statedomain.StatusDestroying,
				IntentVersionID:   "intent-v1",
				IntentSpecHash:    "hash-v1",
				PartitionRevision: "partition-rev-v1",
				TargetPusher:      "local",
				Target:            targetdomain.Placement{Cluster: "local"},
				LastTaskID:        taskID,
				Timestamps:        statedomain.StateTimestamps{LastQueuedAt: finishedAt},
			})
			seedJSONFileB(b, ctx, store, paths.QueueResult("local", taskID), taskdomain.TaskResult{
				APIVersion: "guardian/v1alpha1",
				Kind:       "TaskResult",
				TaskID:     taskID,
				Op:         taskdomain.OpDestroy,
				Status:     taskdomain.ResultSucceeded,
				Partition:  partition,
				Intent:     "api",
				Pusher:     "local",
				FinishedAt: finishedAt,
			})
		}
		b.StartTimer()
		if _, err := processLiveResultFiles(ctx, store, processor, []string{"local"}, "bench"); err != nil {
			b.Fatalf("processLiveResultFiles() error = %v", err)
		}
	}
}

func seedJSONFileB(b *testing.B, ctx context.Context, store guardianapi.WriteStore, logicalPath string, value any) {
	b.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		b.Fatalf("Marshal(%s) error = %v", logicalPath, err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "bench fixture"},
	}); err != nil {
		b.Fatalf("UpsertFiles(%s) error = %v", logicalPath, err)
	}
}

func seedJSONFile(t *testing.T, ctx context.Context, store guardianapi.WriteStore, logicalPath string, value any) {
	t.Helper()

	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal(%s) error = %v", logicalPath, err)
	}
	seedRawFile(t, ctx, store, logicalPath, content)
}

func seedRawFile(t *testing.T, ctx context.Context, store guardianapi.WriteStore, logicalPath string, content []byte) {
	t.Helper()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: logicalPath,
			Content:     content,
		}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed fixture"},
	}); err != nil {
		t.Fatalf("UpsertFiles(%s) error = %v", logicalPath, err)
	}
}
