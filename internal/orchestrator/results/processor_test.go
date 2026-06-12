package results

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/dispatcher"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestProcessorApplySuccessArchivesDeployment(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := dispatcher.NewDispatcher(store, "guardiand")
	processor := NewProcessor(store, dispatch)

	initialState := statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         "payments",
		Intent:            "api",
		Status:            statedomain.StatusApplying,
		IntentVersionID:   "intent-v1",
		IntentSpecHash:    "spec-hash-v1",
		PartitionRevision: "partition-rev-v1",
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "local"},
		AssetVersionIDs:   map[string]string{"backend": "asset-v1"},
		AssetVersions:     map[string]string{"backend": "v1.2.3-abc123-20260429"},
		Outputs:           map[string]string{},
	}
	writeJSONFile(t, ctx, store, paths.IntentState("payments", "api"), initialState)
	intentManifest := []byte(`
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
`)
	seedRawFile(t, ctx, store, paths.IntentManifest("payments", "api"), intentManifest)

	task := taskdomain.Task{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "Task",
		TaskID:            "task-apply-1",
		Partition:         "payments",
		Intent:            "api",
		Op:                taskdomain.OpApply,
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "local"},
		PartitionRevision: "partition-rev-v1",
		IntentVersionID:   "intent-v1",
	}
	finishedAt := time.Now().UTC()
	writeJSONFile(t, ctx, store, paths.QueueTask("local", task.TaskID), task)
	writeJSONFile(t, ctx, store, paths.QueueClaim("local", task.TaskID), taskdomain.ClaimFile{
		TaskID:       task.TaskID,
		WorkerID:     "worker-1",
		ClaimedAt:    finishedAt,
		LeaseSeconds: 300,
	})
	var logBuf bytes.Buffer
	prevLogWriter := log.Writer()
	prevLogFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	defer log.SetOutput(prevLogWriter)
	defer log.SetFlags(prevLogFlags)
	if err := processor.ProcessResult(ctx, &taskdomain.TaskResult{
		APIVersion: "guardian/v1alpha1",
		Kind:       "TaskResult",
		TaskID:     task.TaskID,
		Op:         taskdomain.OpApply,
		Status:     taskdomain.ResultSucceeded,
		Partition:  "payments",
		Intent:     "api",
		Pusher:     "local",
		Outputs:    map[string]string{"backend.id": "compute-payments-api"},
		Logs:       []taskdomain.LogEntry{{Timestamp: finishedAt, Level: "info", Asset: "backend", Message: "apply succeeded"}},
		FinishedAt: finishedAt,
	}); err != nil {
		t.Fatalf("ProcessResult() error = %v", err)
	}

	updatedStateContent, err := store.ReadFile(ctx, paths.IntentState("payments", "api"))
	if err != nil {
		t.Fatalf("ReadFile(intent state) error = %v", err)
	}
	var updatedState statedomain.IntentState
	if err := json.Unmarshal(updatedStateContent, &updatedState); err != nil {
		t.Fatalf("Unmarshal(intent state) error = %v", err)
	}
	if updatedState.Status != statedomain.StatusHealthy {
		t.Fatalf("unexpected status: %s", updatedState.Status)
	}
	if updatedState.DeploymentRevision == "" {
		t.Fatalf("expected deployment revision to be set")
	}
	if updatedState.Outputs["backend.id"] != "compute-payments-api" {
		t.Fatalf("unexpected outputs: %v", updatedState.Outputs)
	}

	archiveContent, err := store.ReadFile(ctx, paths.ArchiveState("payments", "api", updatedState.DeploymentRevision))
	if err != nil {
		t.Fatalf("ReadFile(archive) error = %v", err)
	}
	var record historydomain.DeploymentRecord
	if err := json.Unmarshal(archiveContent, &record); err != nil {
		t.Fatalf("Unmarshal(archive) error = %v", err)
	}
	if record.DeploymentRevision != updatedState.DeploymentRevision {
		t.Fatalf("archive deployment revision = %q, want %q", record.DeploymentRevision, updatedState.DeploymentRevision)
	}
	if len(record.TaskIDs) != 1 || record.TaskIDs[0] != task.TaskID {
		t.Fatalf("unexpected task ids: %v", record.TaskIDs)
	}
	if record.Outputs["backend.id"] != "compute-payments-api" {
		t.Fatalf("unexpected archive outputs: %v", record.Outputs)
	}
	if record.AssetVersions["backend"] != "v1.2.3-abc123-20260429" {
		t.Fatalf("unexpected archive asset versions: %v", record.AssetVersions)
	}
	if !strings.Contains(logBuf.String(), "results: release task=task-apply-1 op=APPLY partition=payments intent=api status=Succeeded release=v1.2.3-abc123-20260429") {
		t.Fatalf("expected release log, got %q", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "deployment_revision="+updatedState.DeploymentRevision) {
		t.Fatalf("expected deployment revision in release log, got %q", logBuf.String())
	}

	// Archive index is no longer written by ArchiveDeployment; reads fall back
	// to the per-deployment scan path (loadDeploymentRecordsFromArchiveScan).
	if _, err := store.ReadFile(ctx, paths.ArchiveIndex("payments", "api")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archive index should not exist, got err=%v", err)
	}

	archivedManifest, err := store.ReadFile(ctx, paths.ArchiveManifest("payments", "api", updatedState.DeploymentRevision))
	if err != nil {
		t.Fatalf("ReadFile(archive manifest) error = %v", err)
	}
	if strings.TrimSpace(string(archivedManifest)) != strings.TrimSpace(string(intentManifest)) {
		t.Fatalf("archive manifest mismatch:\n%s", archivedManifest)
	}

	archiveLogs, err := store.ReadFile(ctx, paths.ArchiveLogs("payments", "api", updatedState.DeploymentRevision))
	if err != nil {
		t.Fatalf("ReadFile(archive logs) error = %v", err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(archiveLogs)))
	if !scanner.Scan() {
		t.Fatalf("expected at least one archived log entry")
	}
	var entry taskdomain.LogEntry
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		t.Fatalf("Unmarshal(log entry) error = %v", err)
	}
	if entry.Message != "apply succeeded" {
		t.Fatalf("unexpected archived log entry: %+v", entry)
	}

	events, err := store.ListDir(ctx, paths.StateEventsDir("payments"))
	if err != nil {
		t.Fatalf("ListDir(events) error = %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected persisted event records")
	}
	if _, err := store.ReadFile(ctx, paths.QueueTask("local", task.TaskID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected queued task to be cleaned up, got %v", err)
	}
	if _, err := store.ReadFile(ctx, paths.QueueClaim("local", task.TaskID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected task claim to be cleaned up, got %v", err)
	}
}

func TestProcessorApplySuccessPersistsObservedState(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := dispatcher.NewDispatcher(store, "guardiand")
	processor := NewProcessor(store, dispatch)

	initialState := baseState("demo", "svc", statedomain.StatusApplying)
	writeJSONFile(t, ctx, store, paths.IntentState("demo", "svc"), initialState)
	seedRawFile(t, ctx, store, paths.IntentManifest("demo", "svc"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: svc
spec:
  intentType: standard
  targetPusher: local
  target:
    cluster: local
  assets:
    - type: Compute
      name: web
      properties:
        image: nginx:latest
`))
	writeJSONFile(t, ctx, store, paths.QueueTask("local", "apply-observed"), taskdomain.Task{
		TaskID:       "apply-observed",
		Partition:    "demo",
		Intent:       "svc",
		Op:           taskdomain.OpApply,
		TargetPusher: "local",
	})

	if err := processor.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:         "apply-observed",
		Op:             taskdomain.OpApply,
		Status:         taskdomain.ResultSucceeded,
		Partition:      "demo",
		Intent:         "svc",
		Pusher:         "local",
		Health:         &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "web: kubernetes deployment has pods in ImagePullBackOff"},
		ApplyReadiness: &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "dependencies resolved"},
		AssetObservations: map[string]*taskdomain.AssetObservation{
			"web": {
				Health:         &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "kubernetes deployment has pods in ImagePullBackOff"},
				ApplyReadiness: &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "dependencies resolved"},
			},
		},
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ProcessResult: %v", err)
	}

	got := loadState(t, ctx, store, "demo", "svc")
	if got.Status != statedomain.StatusHealthy {
		t.Fatalf("status = %q, want Healthy", got.Status)
	}
	if got.Health == nil || got.Health.Status != taskdomain.HealthUnhealthy {
		t.Fatalf("Health = %+v, want unhealthy", got.Health)
	}
	if got.ApplyReadiness == nil || got.ApplyReadiness.Status != taskdomain.ApplyReadinessReady {
		t.Fatalf("ApplyReadiness = %+v, want ready", got.ApplyReadiness)
	}
	if got.AssetObservations == nil || got.AssetObservations["web"] == nil {
		t.Fatalf("AssetObservations = %+v, want web observation", got.AssetObservations)
	}
	if got.AssetObservations["web"].Health == nil || got.AssetObservations["web"].Health.Status != taskdomain.HealthUnhealthy {
		t.Fatalf("web asset health = %+v, want unhealthy", got.AssetObservations["web"])
	}
}

func writeJSONFile(t *testing.T, ctx context.Context, store guardianapi.WriteStore, logicalPath string, value any) {
	t.Helper()

	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal(%s) error = %v", logicalPath, err)
	}
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

func seedRawFile(t *testing.T, ctx context.Context, store guardianapi.WriteStore, logicalPath string, content []byte) {
	t.Helper()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed fixture"},
	}); err != nil {
		t.Fatalf("UpsertFiles(%s) error = %v", logicalPath, err)
	}
}

func baseState(partition, intent string, status statedomain.IntentStatus) statedomain.IntentState {
	return statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         partition,
		Intent:            intent,
		Status:            status,
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "local"},
		IntentVersionID:   "intent-v1",
		IntentSpecHash:    "hash-v1",
		PartitionRevision: "partrev-v1",
		AssetVersionIDs:   map[string]string{},
		AssetVersions:     map[string]string{},
		Outputs:           map[string]string{},
	}
}

// TestProcessorCheckFailed verifies that a failed CHECK result marks
// the intent as CheckFailed and preserves the error message.
func TestProcessorCheckFailed(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "guardiand")
	proc := NewProcessor(store, disp)

	seedIntent(t, ctx, store, "demo", "svc", statedomain.StatusChecking)

	errMsg := "aws quota exceeded"
	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:    "t1",
		Op:        taskdomain.OpCheck,
		Status:    taskdomain.ResultFailed,
		Partition: "demo",
		Intent:    "svc",
		Pusher:    "local",
		Error:     &errMsg,
	}); err != nil {
		t.Fatalf("ProcessResult: %v", err)
	}

	got := loadState(t, ctx, store, "demo", "svc")
	if got.Status != statedomain.StatusCheckFailed {
		t.Fatalf("status = %q, want CheckFailed", got.Status)
	}
	if got.LastError == nil || *got.LastError != errMsg {
		t.Fatalf("LastError = %v, want %q", got.LastError, errMsg)
	}
	if got.ApplyReadiness == nil || got.ApplyReadiness.Status != taskdomain.ApplyReadinessBlocked {
		t.Fatalf("ApplyReadiness = %+v, want blocked", got.ApplyReadiness)
	}
}

// TestProcessorDriftedLocked verifies that drift on a locked intent
// produces DriftedLocked state instead of queuing APPLY.
func TestProcessorDriftedLocked(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "guardiand")
	proc := NewProcessor(store, disp)

	s := baseState("demo", "locked-svc", statedomain.StatusDiffing)
	s.Locked = true
	seedIntentState(t, ctx, store, s)

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:    "t2",
		Op:        taskdomain.OpDiff,
		Status:    taskdomain.ResultSucceeded,
		Partition: "demo",
		Intent:    "locked-svc",
		Pusher:    "local",
		Drift: &taskdomain.DriftReport{
			Status:        "Changed",
			Summary:       "image differs",
			ChangedAssets: []string{"web"},
		},
	}); err != nil {
		t.Fatalf("ProcessResult: %v", err)
	}

	got := loadState(t, ctx, store, "demo", "locked-svc")
	if got.Status != statedomain.StatusDriftedLocked {
		t.Fatalf("status = %q, want DriftedLocked", got.Status)
	}

	// No APPLY task should have been queued.
	entries, _ := store.ListDir(ctx, paths.QueueDir("local"))
	for _, e := range entries {
		if !e.IsDir {
			t.Fatalf("unexpected queue file %q – APPLY must not be queued for locked intent", e.Name)
		}
	}
}

// TestProcessorDriftedReadonly verifies that drift on an intent in a readonly
// partition produces DriftedLocked state without queuing CHECK or APPLY.
func TestProcessorDriftedReadonly(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "guardiand")
	proc := NewProcessor(store, disp)

	s := baseState("infra", "monofs", statedomain.StatusDiffing)
	s.PartitionMode = "readonly"
	seedIntentState(t, ctx, store, s)

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:    "t3",
		Op:        taskdomain.OpDiff,
		Status:    taskdomain.ResultSucceeded,
		Partition: "infra",
		Intent:    "monofs",
		Pusher:    "local",
		Drift: &taskdomain.DriftReport{
			Status:        "Changed",
			Summary:       "config drifted",
			ChangedAssets: []string{"monofs"},
		},
	}); err != nil {
		t.Fatalf("ProcessResult: %v", err)
	}

	got := loadState(t, ctx, store, "infra", "monofs")
	if got.Status != statedomain.StatusDriftedLocked {
		t.Fatalf("status = %q, want DriftedLocked", got.Status)
	}

	// No CHECK or APPLY task should have been queued.
	entries, _ := store.ListDir(ctx, paths.QueueDir("local"))
	for _, e := range entries {
		if !e.IsDir {
			t.Fatalf("unexpected queue file %q – tasks must not be queued for readonly partition", e.Name)
		}
	}
}

// TestProcessorApplyFailed verifies that a failed APPLY result marks
// the intent as ApplyFailed.
func TestProcessorApplyFailed(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "guardiand")
	proc := NewProcessor(store, disp)

	seedIntent(t, ctx, store, "demo", "flaky", statedomain.StatusApplying)
	writeJSONFile(t, ctx, store, paths.QueueTask("local", "t3"), taskdomain.Task{
		TaskID:       "t3",
		Partition:    "demo",
		Intent:       "flaky",
		Op:           taskdomain.OpApply,
		TargetPusher: "local",
	})
	writeJSONFile(t, ctx, store, paths.QueueClaim("local", "t3"), taskdomain.ClaimFile{
		TaskID:       "t3",
		WorkerID:     "worker-1",
		ClaimedAt:    time.Now().UTC(),
		LeaseSeconds: 300,
	})

	errMsg := "terraform apply failed"
	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:    "t3",
		Op:        taskdomain.OpApply,
		Status:    taskdomain.ResultFailed,
		Partition: "demo",
		Intent:    "flaky",
		Pusher:    "local",
		Error:     &errMsg,
	}); err != nil {
		t.Fatalf("ProcessResult: %v", err)
	}

	got := loadState(t, ctx, store, "demo", "flaky")
	if got.Status != statedomain.StatusApplyFailed {
		t.Fatalf("status = %q, want ApplyFailed", got.Status)
	}
	if _, err := store.ReadFile(ctx, paths.QueueTask("local", "t3")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected failed task to be cleaned up, got %v", err)
	}
	if _, err := store.ReadFile(ctx, paths.QueueClaim("local", "t3")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected failed task claim to be cleaned up, got %v", err)
	}
}

// TestProcessorDestroySuccess verifies that a successful DESTROY result
// transitions the intent to Destroyed.
func TestProcessorDestroySuccess(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "guardiand")
	proc := NewProcessor(store, disp)

	seedIntent(t, ctx, store, "demo", "old-svc", statedomain.StatusDestroying)

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:    "t4",
		Op:        taskdomain.OpDestroy,
		Status:    taskdomain.ResultSucceeded,
		Partition: "demo",
		Intent:    "old-svc",
		Pusher:    "local",
	}); err != nil {
		t.Fatalf("ProcessResult: %v", err)
	}

	got := loadState(t, ctx, store, "demo", "old-svc")
	if got.Status != statedomain.StatusDestroyed {
		t.Fatalf("status = %q, want Destroyed", got.Status)
	}
}

// TestProcessorDiffNoChange verifies that DIFF with InSync drift marks Healthy
// without queuing APPLY.
func TestProcessorDiffNoChange(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "guardiand")
	proc := NewProcessor(store, disp)

	seedIntent(t, ctx, store, "demo", "stable", statedomain.StatusDiffing)

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:         "t5",
		Op:             taskdomain.OpDiff,
		Status:         taskdomain.ResultSucceeded,
		Partition:      "demo",
		Intent:         "stable",
		Pusher:         "local",
		Drift:          &taskdomain.DriftReport{Status: "InSync", Summary: "all good"},
		Health:         &taskdomain.HealthObservation{Status: taskdomain.HealthDegraded, Summary: "web: waiting for ready replicas"},
		ApplyReadiness: &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "dependencies resolved"},
		AssetObservations: map[string]*taskdomain.AssetObservation{
			"web": {
				Health:         &taskdomain.HealthObservation{Status: taskdomain.HealthDegraded, Summary: "waiting for ready replicas"},
				ApplyReadiness: &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "dependencies resolved"},
			},
		},
	}); err != nil {
		t.Fatalf("ProcessResult: %v", err)
	}

	got := loadState(t, ctx, store, "demo", "stable")
	if got.Status != statedomain.StatusHealthy {
		t.Fatalf("status = %q, want Healthy", got.Status)
	}
	if got.Health == nil || got.Health.Status != taskdomain.HealthDegraded {
		t.Fatalf("Health = %+v, want degraded", got.Health)
	}
	if got.ApplyReadiness == nil || got.ApplyReadiness.Status != taskdomain.ApplyReadinessReady {
		t.Fatalf("ApplyReadiness = %+v, want ready", got.ApplyReadiness)
	}
	if got.AssetObservations == nil || got.AssetObservations["web"] == nil {
		t.Fatalf("AssetObservations = %+v, want web observation", got.AssetObservations)
	}
}

func TestProcessorReusesQueuedTaskPayloadAcrossPhases(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "guardiand")
	proc := NewProcessor(store, disp)

	state := baseState("demo", "svc", statedomain.StatusDiffing)
	state.LastTaskID = "diff-task"
	seedIntentState(t, ctx, store, state)

	diffTask := taskdomain.Task{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "Task",
		TaskID:            "diff-task",
		CorrelationID:     "corr-demo",
		Partition:         "demo",
		Intent:            "svc",
		Op:                taskdomain.OpDiff,
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "local"},
		PartitionRevision: "partrev-v1",
		IntentVersionID:   "intent-v1",
		IntentSpecHash:    "hash-v1",
		AssetVersionIDs:   map[string]string{"web": "asset-v1"},
		AssetVersions:     map[string]string{"web": "ver-v1"},
		Assets: []taskdomain.AbstractAsset{{
			Type:       "Compute",
			Name:       "web",
			Properties: map[string]any{"image": "nginx:latest"},
		}},
	}
	writeJSONFile(t, ctx, store, paths.QueueTask("local", diffTask.TaskID), diffTask)

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:    diffTask.TaskID,
		Op:        taskdomain.OpDiff,
		Status:    taskdomain.ResultSucceeded,
		Partition: "demo",
		Intent:    "svc",
		Pusher:    "local",
		Drift: &taskdomain.DriftReport{
			Status:        "Changed",
			Summary:       "image drift",
			ChangedAssets: []string{"web"},
		},
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ProcessResult(diff): %v", err)
	}

	stateAfterDiff := loadState(t, ctx, store, "demo", "svc")
	if stateAfterDiff.Status != statedomain.StatusChecking {
		t.Fatalf("status after diff = %q, want Checking", stateAfterDiff.Status)
	}
	if stateAfterDiff.LastTaskID == diffTask.TaskID {
		t.Fatalf("expected new check task id, got %q", stateAfterDiff.LastTaskID)
	}

	checkTaskRaw, err := store.ReadFile(ctx, paths.QueueTask("local", stateAfterDiff.LastTaskID))
	if err != nil {
		t.Fatalf("ReadFile(check task): %v", err)
	}
	var checkTask taskdomain.Task
	if err := json.Unmarshal(checkTaskRaw, &checkTask); err != nil {
		t.Fatalf("Unmarshal(check task): %v", err)
	}
	if checkTask.Op != taskdomain.OpCheck {
		t.Fatalf("checkTask.Op = %q, want CHECK", checkTask.Op)
	}
	if checkTask.CorrelationID != diffTask.CorrelationID {
		t.Fatalf("checkTask.CorrelationID = %q, want %q", checkTask.CorrelationID, diffTask.CorrelationID)
	}
	if !reflect.DeepEqual(checkTask.Assets, diffTask.Assets) {
		t.Fatalf("checkTask.Assets = %#v, want %#v", checkTask.Assets, diffTask.Assets)
	}

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:     checkTask.TaskID,
		Op:         taskdomain.OpCheck,
		Status:     taskdomain.ResultSucceeded,
		Partition:  "demo",
		Intent:     "svc",
		Pusher:     "local",
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ProcessResult(check): %v", err)
	}

	stateAfterCheck := loadState(t, ctx, store, "demo", "svc")
	if stateAfterCheck.Status != statedomain.StatusApplying {
		t.Fatalf("status after check = %q, want Applying", stateAfterCheck.Status)
	}

	applyTaskRaw, err := store.ReadFile(ctx, paths.QueueTask("local", stateAfterCheck.LastTaskID))
	if err != nil {
		t.Fatalf("ReadFile(apply task): %v", err)
	}
	var applyTask taskdomain.Task
	if err := json.Unmarshal(applyTaskRaw, &applyTask); err != nil {
		t.Fatalf("Unmarshal(apply task): %v", err)
	}
	if applyTask.Op != taskdomain.OpApply {
		t.Fatalf("applyTask.Op = %q, want APPLY", applyTask.Op)
	}
	if applyTask.CorrelationID != diffTask.CorrelationID {
		t.Fatalf("applyTask.CorrelationID = %q, want %q", applyTask.CorrelationID, diffTask.CorrelationID)
	}
	if !reflect.DeepEqual(applyTask.Assets, diffTask.Assets) {
		t.Fatalf("applyTask.Assets = %#v, want %#v", applyTask.Assets, diffTask.Assets)
	}
}

// helpers used by the new tests

func seedIntent(t *testing.T, ctx context.Context, store *memory.Store, partition, intent string, status statedomain.IntentStatus) {
	t.Helper()
	seedIntentState(t, ctx, store, baseState(partition, intent, status))
}

func seedIntentState(t *testing.T, ctx context.Context, store *memory.Store, s statedomain.IntentState) {
	t.Helper()
	writeJSONFile(t, ctx, store, paths.IntentState(s.Partition, s.Intent), s)
}

func loadState(t *testing.T, ctx context.Context, store *memory.Store, partition, intent string) statedomain.IntentState {
	t.Helper()
	var s statedomain.IntentState
	data, err := store.ReadFile(ctx, paths.IntentState(partition, intent))
	if err != nil {
		t.Fatalf("ReadFile intent state: %v", err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("Unmarshal intent state: %v", err)
	}
	return s
}
