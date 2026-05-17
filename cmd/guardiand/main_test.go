package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
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

	if err := processLiveResultFiles(ctx, store, processor, []string{"local"}, "test"); err != nil {
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

	if err := processLiveResultFiles(ctx, store, processor, []string{"local"}, "test"); err != nil {
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

func TestProcessLiveResultFilesUsesPartitionRuntimeSnapshot(t *testing.T) {
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
	if err := processLiveResultFiles(ctx, store, processor, []string{"local"}, "test"); err != nil {
		t.Fatalf("processLiveResultFiles() error = %v", err)
	}
	if got := store.ListCount(paths.StateIntentsDir("payments")); got != 0 {
		t.Fatalf("expected runtime-backed scan to avoid listing %s, got %d calls", paths.StateIntentsDir("payments"), got)
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
