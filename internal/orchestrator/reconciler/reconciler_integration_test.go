package reconciler_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/dispatcher"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/reconciler"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/results"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestEndToEndReconcileFlow(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "test")
	recon := reconciler.NewReconciler(store, disp, time.Minute)
	proc := results.NewProcessor(store, disp)

	seedRaw(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`))
	seedRaw(t, ctx, store, paths.IntentManifest("demo", "core-storage"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: core-storage
spec:
  intentType: standard
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Database
      name: db
      properties:
        engine: postgres
`))
	seedRaw(t, ctx, store, paths.IntentManifest("demo", "worker-nodes"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: worker-nodes
spec:
  intentType: standard
  joins:
    - core-storage
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Compute
      name: worker
      properties:
        image: worker:v1
`))

	if err := recon.ReconcilePartition(ctx, "demo", true); err != nil {
		t.Fatalf("ReconcilePartition() error = %v", err)
	}

	coreState, err := common.LoadIntentState(ctx, store, "demo", "core-storage")
	if err != nil {
		t.Fatalf("LoadIntentState(core-storage) error = %v", err)
	}
	workerState, err := common.LoadIntentState(ctx, store, "demo", "worker-nodes")
	if err != nil {
		t.Fatalf("LoadIntentState(worker-nodes) error = %v", err)
	}
	if coreState.Status != statedomain.StatusDiffing {
		t.Fatalf("coreState.Status = %q, want %q", coreState.Status, statedomain.StatusDiffing)
	}
	if workerState.Status != statedomain.StatusBlocked {
		t.Fatalf("workerState.Status = %q, want %q", workerState.Status, statedomain.StatusBlocked)
	}
	partitionState, err := common.LoadPartitionState(ctx, store, "demo")
	if err != nil {
		t.Fatalf("LoadPartitionState(initial) error = %v", err)
	}
	if got, want := partitionState.Status, "Progressing"; got != want {
		t.Fatalf("partitionState.Status after first reconcile = %q, want %q", got, want)
	}

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:     coreState.LastTaskID,
		Op:         taskdomain.OpDiff,
		Status:     taskdomain.ResultSucceeded,
		Partition:  "demo",
		Intent:     "core-storage",
		Pusher:     "local",
		Drift:      &taskdomain.DriftReport{Status: "InSync", Summary: "no drift"},
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ProcessResult(diff core) error = %v", err)
	}
	coreState, _ = common.LoadIntentState(ctx, store, "demo", "core-storage")
	if coreState.Status != statedomain.StatusHealthy {
		t.Fatalf("coreState.Status after diff = %q, want %q", coreState.Status, statedomain.StatusHealthy)
	}
	workerState, _ = common.LoadIntentState(ctx, store, "demo", "worker-nodes")
	if workerState.Status != statedomain.StatusDiffing {
		t.Fatalf("workerState.Status after unblock = %q, want %q", workerState.Status, statedomain.StatusDiffing)
	}

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:     workerState.LastTaskID,
		Op:         taskdomain.OpDiff,
		Status:     taskdomain.ResultSucceeded,
		Partition:  "demo",
		Intent:     "worker-nodes",
		Pusher:     "local",
		Drift:      &taskdomain.DriftReport{Status: "Changed", Summary: "worker drift", ChangedAssets: []string{"worker"}},
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ProcessResult(diff worker) error = %v", err)
	}
	workerState, _ = common.LoadIntentState(ctx, store, "demo", "worker-nodes")
	if workerState.Status != statedomain.StatusChecking {
		t.Fatalf("workerState.Status after diff = %q, want %q", workerState.Status, statedomain.StatusChecking)
	}

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:     workerState.LastTaskID,
		Op:         taskdomain.OpCheck,
		Status:     taskdomain.ResultSucceeded,
		Partition:  "demo",
		Intent:     "worker-nodes",
		Pusher:     "local",
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ProcessResult(check worker) error = %v", err)
	}
	workerState, _ = common.LoadIntentState(ctx, store, "demo", "worker-nodes")
	if workerState.Status != statedomain.StatusApplying {
		t.Fatalf("workerState.Status after check = %q, want %q", workerState.Status, statedomain.StatusApplying)
	}

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:     workerState.LastTaskID,
		Op:         taskdomain.OpApply,
		Status:     taskdomain.ResultSucceeded,
		Partition:  "demo",
		Intent:     "worker-nodes",
		Pusher:     "local",
		Outputs:    map[string]string{"instance": "demo/worker-nodes/worker", "image": "worker:v1"},
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ProcessResult(apply worker) error = %v", err)
	}

	workerState, err = common.LoadIntentState(ctx, store, "demo", "worker-nodes")
	if err != nil {
		t.Fatalf("LoadIntentState(worker) final error = %v", err)
	}
	if workerState.Status != statedomain.StatusHealthy {
		t.Fatalf("workerState.Status final = %q, want %q", workerState.Status, statedomain.StatusHealthy)
	}
	partitionState, err = common.LoadPartitionState(ctx, store, "demo")
	if err != nil {
		t.Fatalf("LoadPartitionState(final) error = %v", err)
	}
	if got, want := partitionState.Status, "Healthy"; got != want {
		t.Fatalf("partitionState.Status final = %q, want %q", got, want)
	}
	if got, want := partitionState.Metrics.HealthyIntents, 2; got != want {
		t.Fatalf("partitionState healthy intents = %d, want %d", got, want)
	}
	if workerState.Outputs["instance"] != "demo/worker-nodes/worker" {
		t.Fatalf("workerState.Outputs[instance] = %q", workerState.Outputs["instance"])
	}

	var archive map[string]any
	if err := loadJSON(ctx, store, paths.ArchiveState("demo", "worker-nodes", workerState.DeploymentRevision), &archive); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
}

func TestRefreshCyclePreservesHealthyStatus(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "test")
	recon := reconciler.NewReconciler(store, disp, time.Minute)
	proc := results.NewProcessor(store, disp)

	seedRaw(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`))
	seedRaw(t, ctx, store, paths.IntentManifest("demo", "api"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Compute
      name: web
      properties:
        image: api:v1
`))
	existingState, err := json.Marshal(statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         "demo",
		Intent:            "api",
		Status:            statedomain.StatusHealthy,
		IntentVersionID:   "intent-prev",
		IntentSpecHash:    "spec-prev",
		PartitionRevision: "part-prev",
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "local"},
		AssetVersionIDs:   map[string]string{"web": "asset-prev"},
		Outputs:           map[string]string{"web.id": "demo/api/web"},
	})
	if err != nil {
		t.Fatalf("Marshal(existing state) error = %v", err)
	}

	seedRaw(t, ctx, store, paths.IntentState("demo", "api"), existingState)

	if err := recon.ReconcilePartition(ctx, "demo", true); err != nil {
		t.Fatalf("ReconcilePartition() error = %v", err)
	}

	state, err := common.LoadIntentState(ctx, store, "demo", "api")
	if err != nil {
		t.Fatalf("LoadIntentState(api) after reconcile error = %v", err)
	}
	if state.Status != statedomain.StatusHealthy {
		t.Fatalf("state.Status after queue = %q, want %q", state.Status, statedomain.StatusHealthy)
	}
	if state.LastTaskID == "" {
		t.Fatalf("expected LastTaskID after refresh queue")
	}
	diffTaskID := state.LastTaskID

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:    diffTaskID,
		Op:        taskdomain.OpDiff,
		Status:    taskdomain.ResultSucceeded,
		Partition: "demo",
		Intent:    "api",
		Pusher:    "local",
		Drift: &taskdomain.DriftReport{
			Status:        "Changed",
			Summary:       "image drift",
			ChangedAssets: []string{"web"},
		},
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ProcessResult(diff api) error = %v", err)
	}

	state, err = common.LoadIntentState(ctx, store, "demo", "api")
	if err != nil {
		t.Fatalf("LoadIntentState(api) after diff error = %v", err)
	}
	if state.Status != statedomain.StatusHealthy {
		t.Fatalf("state.Status after diff = %q, want %q", state.Status, statedomain.StatusHealthy)
	}
	if state.LastTaskID == "" || state.LastTaskID == diffTaskID {
		t.Fatalf("expected a new CHECK task after DIFF, got %q", state.LastTaskID)
	}
	checkTaskID := state.LastTaskID

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:     checkTaskID,
		Op:         taskdomain.OpCheck,
		Status:     taskdomain.ResultSucceeded,
		Partition:  "demo",
		Intent:     "api",
		Pusher:     "local",
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ProcessResult(check api) error = %v", err)
	}

	state, err = common.LoadIntentState(ctx, store, "demo", "api")
	if err != nil {
		t.Fatalf("LoadIntentState(api) after check error = %v", err)
	}
	if state.Status != statedomain.StatusHealthy {
		t.Fatalf("state.Status after check = %q, want %q", state.Status, statedomain.StatusHealthy)
	}
	if state.LastTaskID == "" || state.LastTaskID == checkTaskID {
		t.Fatalf("expected a new APPLY task after CHECK, got %q", state.LastTaskID)
	}

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:     state.LastTaskID,
		Op:         taskdomain.OpApply,
		Status:     taskdomain.ResultSucceeded,
		Partition:  "demo",
		Intent:     "api",
		Pusher:     "local",
		Outputs:    map[string]string{"web.id": "demo/api/web", "image": "api:v1"},
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ProcessResult(apply api) error = %v", err)
	}

	state, err = common.LoadIntentState(ctx, store, "demo", "api")
	if err != nil {
		t.Fatalf("LoadIntentState(api) final error = %v", err)
	}
	if state.Status != statedomain.StatusHealthy {
		t.Fatalf("final state.Status = %q, want %q", state.Status, statedomain.StatusHealthy)
	}
}

func TestApplyFailedIntentRequeuesCheckAndLeavesInvalidState(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "test")
	recon := reconciler.NewReconciler(store, disp, time.Minute)

	seedRaw(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`))
	seedRaw(t, ctx, store, paths.IntentManifest("demo", "api"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Compute
      name: web
      properties:
        image: api:v1
`))

	seedRaw(t, ctx, store, paths.IntentState("demo", "api"), mustJSON(t, statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         "demo",
		Intent:            "api",
		Status:            statedomain.StatusApplyFailed,
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "local"},
		IntentVersionID:   "intent-v1",
		IntentSpecHash:    "spec-hash-v1",
		PartitionRevision: "part-rev-v1",
		AssetVersionIDs:   map[string]string{"web": "asset-v1"},
		Outputs:           map[string]string{},
		LastError:         pointerTo("previous apply failed"),
	}))

	if err := recon.ReconcilePartition(ctx, "demo", true); err != nil {
		t.Fatalf("ReconcilePartition() error = %v", err)
	}

	state, err := common.LoadIntentState(ctx, store, "demo", "api")
	if err != nil {
		t.Fatalf("LoadIntentState(api) error = %v", err)
	}
	if got, want := state.Status, statedomain.StatusChecking; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if state.LastTaskID == "" {
		t.Fatalf("expected LastTaskID to be set")
	}

	var task taskdomain.Task
	if err := loadJSON(ctx, store, paths.QueueTask("local", state.LastTaskID), &task); err != nil {
		t.Fatalf("load queued task error = %v", err)
	}
	if task.Op != taskdomain.OpCheck {
		t.Fatalf("queued task op = %q, want %q", task.Op, taskdomain.OpCheck)
	}
}

func pointerTo(value string) *string {
	return &value
}

func TestReconcilePartitionDriftedIdleQueuesCheck(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "test")
	recon := reconciler.NewReconciler(store, disp, time.Minute)

	seedRaw(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`))
	seedRaw(t, ctx, store, paths.IntentManifest("demo", "api"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Compute
      name: web
      properties:
        image: api:v1
`))

	seedRaw(t, ctx, store, paths.IntentState("demo", "api"), mustJSON(t, statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         "demo",
		Intent:            "api",
		Status:            statedomain.StatusDrifted,
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "local"},
		IntentVersionID:   "intent-v1",
		IntentSpecHash:    "spec-hash-v1",
		PartitionRevision: "part-rev-v1",
		AssetVersionIDs:   map[string]string{"web": "asset-v1"},
		Outputs:           map[string]string{},
		Drift: &taskdomain.DriftReport{
			Status:        "Changed",
			Summary:       "web drift",
			ChangedAssets: []string{"web"},
		},
	}))

	if err := recon.ReconcilePartition(ctx, "demo", true); err != nil {
		t.Fatalf("ReconcilePartition() error = %v", err)
	}

	state, err := common.LoadIntentState(ctx, store, "demo", "api")
	if err != nil {
		t.Fatalf("LoadIntentState(api) error = %v", err)
	}
	if state.LastTaskID == "" {
		t.Fatalf("expected LastTaskID to be set")
	}

	var task taskdomain.Task
	if err := loadJSON(ctx, store, paths.QueueTask("local", state.LastTaskID), &task); err != nil {
		t.Fatalf("load queued task error = %v", err)
	}
	if task.Op != taskdomain.OpCheck {
		t.Fatalf("queued task op = %q, want %q", task.Op, taskdomain.OpCheck)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json marshal error: %v", err)
	}
	return content
}

func seedRaw(t *testing.T, ctx context.Context, store *memory.Store, logicalPath string, data []byte) {
	t.Helper()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: data}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed"},
	}); err != nil {
		t.Fatalf("UpsertFiles(%s) error = %v", logicalPath, err)
	}
}

func loadJSON(ctx context.Context, store *memory.Store, logicalPath string, out any) error {
	data, err := store.ReadFile(ctx, logicalPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

type blockingConfigReadStore struct {
	guardianapi.Store

	mu                 sync.Mutex
	delay              time.Duration
	concurrentReads    int
	maxConcurrentReads int
}

func (s *blockingConfigReadStore) ReadFile(ctx context.Context, logicalPath string) ([]byte, error) {
	if strings.HasPrefix(logicalPath, "/partitions/") && strings.HasSuffix(logicalPath, "/config.yaml") {
		s.mu.Lock()
		s.concurrentReads++
		if s.concurrentReads > s.maxConcurrentReads {
			s.maxConcurrentReads = s.concurrentReads
		}
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			s.concurrentReads--
			s.mu.Unlock()
		}()

		timer := time.NewTimer(s.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return s.Store.ReadFile(ctx, logicalPath)
}

func (s *blockingConfigReadStore) MaxConcurrentReads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxConcurrentReads
}

func TestReconcileAllProcessesPartitionsInParallel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	baseStore := memory.New()
	for _, partition := range []string{"alpha", "beta", "gamma"} {
		seedRaw(t, ctx, baseStore, paths.PartitionConfig(partition), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: `+partition+`
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`))
	}
	store := &blockingConfigReadStore{Store: baseStore, delay: 75 * time.Millisecond}
	disp := dispatcher.NewDispatcher(store, "test")
	recon := reconciler.NewReconciler(store, disp, time.Minute)

	if err := recon.ReconcileAll(ctx); err != nil {
		t.Fatalf("ReconcileAll() error = %v", err)
	}
	if got := store.MaxConcurrentReads(); got < 2 {
		t.Fatalf("expected full reconcile to overlap partition config reads, got max concurrency %d", got)
	}
}

func TestReconcilePartitionSerializesSamePartition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	baseStore := memory.New()
	seedRaw(t, ctx, baseStore, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`))
	store := &blockingConfigReadStore{Store: baseStore, delay: 75 * time.Millisecond}
	disp := dispatcher.NewDispatcher(store, "test")
	recon := reconciler.NewReconciler(store, disp, time.Minute)

	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errCh <- recon.ReconcilePartition(ctx, "demo", true)
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("ReconcilePartition() error = %v", err)
		}
	}
	if got := store.MaxConcurrentReads(); got != 1 {
		t.Fatalf("expected same-partition reconcile calls to serialize, got max concurrency %d", got)
	}
}

// TestReconcileLockBlocksApply verifies the full E2E path where an intent with
// locked:true reaches DriftedLocked instead of Applying when drift is found.
func TestReconcileLockBlocksApply(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "test")
	recon := reconciler.NewReconciler(store, disp, time.Minute)
	proc := results.NewProcessor(store, disp)

	seedRaw(t, ctx, store, paths.PartitionConfig("lock-demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: lock-demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`))
	seedRaw(t, ctx, store, paths.IntentManifest("lock-demo", "frozen"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: frozen
spec:
  intentType: standard
  targetPusher: local
  target:
    cluster: local
  locked: true
  assets:
    - type: Compute
      name: srv
      properties:
        image: img:v1
`))

	if err := recon.ReconcilePartition(ctx, "lock-demo", true); err != nil {
		t.Fatalf("ReconcilePartition: %v", err)
	}

	state, err := common.LoadIntentState(ctx, store, "lock-demo", "frozen")
	if err != nil {
		t.Fatalf("LoadIntentState: %v", err)
	}
	if state.Status != statedomain.StatusDiffing {
		t.Fatalf("status = %q, want Diffing", state.Status)
	}

	// DIFF reports drift; locked=true must produce DriftedLocked immediately.
	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:    state.LastTaskID,
		Op:        taskdomain.OpDiff,
		Status:    taskdomain.ResultSucceeded,
		Partition: "lock-demo",
		Intent:    "frozen",
		Pusher:    "local",
		Drift: &taskdomain.DriftReport{
			Status:        "Changed",
			Summary:       "image was patched in prod",
			ChangedAssets: []string{"srv"},
		},
	}); err != nil {
		t.Fatalf("ProcessResult(diff): %v", err)
	}

	state, _ = common.LoadIntentState(ctx, store, "lock-demo", "frozen")
	if state.Status != statedomain.StatusDriftedLocked {
		t.Fatalf("final status = %q, want DriftedLocked", state.Status)
	}
}

// TestReconcileReadonlyPartitionNeverQueuesApply verifies that a partition
// with mode: readonly detects drift and surfaces DriftedLocked status but
// never queues CHECK or APPLY tasks.
func TestReconcileReadonlyPartitionNeverQueuesApply(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "test")
	recon := reconciler.NewReconciler(store, disp, time.Minute)
	proc := results.NewProcessor(store, disp)

	seedRaw(t, ctx, store, paths.PartitionConfig("bootstrap"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: bootstrap
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: readonly
    interval: 30s
`))
	seedRaw(t, ctx, store, paths.IntentManifest("bootstrap", "monofs"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: monofs
spec:
  intentType: standard
  targetPusher: local
  target:
    cluster: local
  assets:
    - type: Compute
      name: svc
      properties:
        image: monofs:v1
`))

	if err := recon.ReconcilePartition(ctx, "bootstrap", true); err != nil {
		t.Fatalf("ReconcilePartition: %v", err)
	}

	state, err := common.LoadIntentState(ctx, store, "bootstrap", "monofs")
	if err != nil {
		t.Fatalf("LoadIntentState: %v", err)
	}
	if state.Status != statedomain.StatusDiffing {
		t.Fatalf("status after reconcile = %q, want Diffing", state.Status)
	}
	if state.PartitionMode != "readonly" {
		t.Fatalf("PartitionMode = %q, want readonly", state.PartitionMode)
	}

	// DIFF reports drift; readonly mode must produce DriftedLocked, not queue CHECK.
	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:    state.LastTaskID,
		Op:        taskdomain.OpDiff,
		Status:    taskdomain.ResultSucceeded,
		Partition: "bootstrap",
		Intent:    "monofs",
		Pusher:    "local",
		Drift: &taskdomain.DriftReport{
			Status:        "Changed",
			Summary:       "image was updated outside guardian",
			ChangedAssets: []string{"svc"},
		},
	}); err != nil {
		t.Fatalf("ProcessResult(diff): %v", err)
	}

	state, _ = common.LoadIntentState(ctx, store, "bootstrap", "monofs")
	if state.Status != statedomain.StatusDriftedLocked {
		t.Fatalf("final status = %q, want DriftedLocked", state.Status)
	}

	// Queue must be empty — no CHECK or APPLY task created.
	entries, _ := store.ListDir(ctx, paths.QueueDir("local"))
	for _, e := range entries {
		if !e.IsDir {
			t.Fatalf("unexpected queue file %q – readonly partition must not queue tasks", e.Name)
		}
	}
}

// TestReconcileOrphanDeleteKeepsNoState verifies that after a partition is
// reconciled and an intent is subsequently removed from the store, the
// reconcile cycle reflects the missing intent as gone without queuing DESTROY
// (orphan policy).
func TestReconcileOrphanDeleteKeepsNoState(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "test")
	recon := reconciler.NewReconciler(store, disp, time.Minute)

	seedRaw(t, ctx, store, paths.PartitionConfig("orphan-demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: orphan-demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`))
	seedRaw(t, ctx, store, paths.IntentManifest("orphan-demo", "transient"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: transient
spec:
  intentType: standard
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Compute
      name: w
      properties:
        image: img:v1
`))

	// First reconcile – intent should be picked up.
	if err := recon.ReconcilePartition(ctx, "orphan-demo", true); err != nil {
		t.Fatalf("first ReconcilePartition: %v", err)
	}
	if _, err := common.LoadIntentState(ctx, store, "orphan-demo", "transient"); err != nil {
		t.Fatalf("intent state missing after first reconcile: %v", err)
	}

	// Delete the intent manifest file (simulate operator removing the blueprint).
	if _, err := store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: paths.IntentManifest("orphan-demo", "transient")}},
		Context: guardianapi.MutationContext{PrincipalID: "ctl", Reason: "remove intent"},
	}); err != nil {
		t.Fatalf("DeletePaths: %v", err)
	}

	// Second reconcile – orphan policy: the reconciler simply won't include the
	// deleted intent in the next compile cycle and drops its state. No DESTROY
	// task should appear.
	if err := recon.ReconcilePartition(ctx, "orphan-demo", true); err != nil {
		t.Fatalf("second ReconcilePartition: %v", err)
	}

	if _, err := store.Stat(ctx, paths.IntentState("orphan-demo", "transient")); !os.IsNotExist(err) {
		t.Fatalf("expected orphaned intent state to be deleted, got err=%v", err)
	}

	// Confirm no DESTROY task is queued by reading every task file in the queue
	// and checking that none has Op=DESTROY for the deleted intent.
	entries, _ := store.ListDir(ctx, paths.QueueDir("local"))
	for _, e := range entries {
		if e.IsDir || !strings.HasSuffix(e.Name, ".json") || strings.HasPrefix(e.Name, ".") {
			continue
		}
		raw, err := store.ReadFile(ctx, paths.QueueDir("local")+"/"+e.Name)
		if err != nil {
			t.Fatalf("ReadFile queue task %s: %v", e.Name, err)
		}
		var qt taskdomain.Task
		if err := json.Unmarshal(raw, &qt); err != nil {
			continue // not a Task JSON – ignore
		}
		if qt.Op == taskdomain.OpDestroy && qt.Intent == "transient" {
			t.Fatalf("unexpected DESTROY task for orphan-deleted intent %q", qt.Intent)
		}
	}
}

func TestReconcileDestroyDeleteQueuesDestroy(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "test")
	recon := reconciler.NewReconciler(store, disp, time.Minute)
	proc := results.NewProcessor(store, disp)

	seedRaw(t, ctx, store, paths.PartitionConfig("destroy-demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: destroy-demo
spec:
  deletionPolicy: destroy
  reconciliation:
    mode: auto
    interval: 30s
`))
	seedRaw(t, ctx, store, paths.IntentManifest("destroy-demo", "legacy"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: legacy
spec:
  intentType: standard
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Compute
      name: old
      properties:
        image: img:v1
`))

	if err := recon.ReconcilePartition(ctx, "destroy-demo", true); err != nil {
		t.Fatalf("first ReconcilePartition: %v", err)
	}
	initialState, err := common.LoadIntentState(ctx, store, "destroy-demo", "legacy")
	if err != nil {
		t.Fatalf("LoadIntentState: %v", err)
	}
	if initialState.IntentVersionID == "" {
		t.Fatalf("expected intent version to be recorded")
	}

	if _, err := store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: paths.IntentManifest("destroy-demo", "legacy")}},
		Context: guardianapi.MutationContext{PrincipalID: "ctl", Reason: "remove intent"},
	}); err != nil {
		t.Fatalf("DeletePaths: %v", err)
	}

	if err := recon.ReconcilePartition(ctx, "destroy-demo", true); err != nil {
		t.Fatalf("second ReconcilePartition: %v", err)
	}

	destroyState, err := common.LoadIntentState(ctx, store, "destroy-demo", "legacy")
	if err != nil {
		t.Fatalf("LoadIntentState after delete: %v", err)
	}
	if destroyState.Status != statedomain.StatusDestroying {
		t.Fatalf("status = %q, want %q", destroyState.Status, statedomain.StatusDestroying)
	}
	taskData, err := store.ReadFile(ctx, paths.QueueTask("local", destroyState.LastTaskID))
	if err != nil {
		t.Fatalf("ReadFile destroy task: %v", err)
	}
	var destroyTask taskdomain.Task
	if err := json.Unmarshal(taskData, &destroyTask); err != nil {
		t.Fatalf("Unmarshal destroy task: %v", err)
	}
	if destroyTask.Op != taskdomain.OpDestroy {
		t.Fatalf("destroy task op = %q, want %q", destroyTask.Op, taskdomain.OpDestroy)
	}

	if err := proc.ProcessResult(ctx, &taskdomain.TaskResult{
		TaskID:     destroyTask.TaskID,
		Op:         taskdomain.OpDestroy,
		Status:     taskdomain.ResultSucceeded,
		Partition:  "destroy-demo",
		Intent:     "legacy",
		Pusher:     "local",
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ProcessResult(destroy): %v", err)
	}

	destroyState, err = common.LoadIntentState(ctx, store, "destroy-demo", "legacy")
	if err != nil {
		t.Fatalf("LoadIntentState final: %v", err)
	}
	if destroyState.Status != statedomain.StatusDestroyed {
		t.Fatalf("final status = %q, want %q", destroyState.Status, statedomain.StatusDestroyed)
	}
	if len(destroyState.Outputs) != 0 {
		t.Fatalf("expected destroy to clear outputs, got %v", destroyState.Outputs)
	}
}
