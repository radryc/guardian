package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type blockingUpsertStore struct {
	guardianapi.Store

	mu        sync.Mutex
	blockPath string
	probePath string
	entered   chan struct{}
	release   chan struct{}
	probed    chan struct{}
}

func (s *blockingUpsertStore) UpsertFiles(ctx context.Context, batch guardianapi.MutationBatch) (guardianapi.BatchRevision, error) {
	shouldBlock := false
	shouldProbe := false

	s.mu.Lock()
	for _, write := range batch.Writes {
		if s.blockPath != "" && write.LogicalPath == s.blockPath {
			shouldBlock = true
			s.blockPath = ""
		}
		if s.probePath != "" && write.LogicalPath == s.probePath {
			shouldProbe = true
			s.probePath = ""
		}
	}
	s.mu.Unlock()

	if shouldProbe {
		close(s.probed)
	}
	if shouldBlock {
		close(s.entered)
		select {
		case <-ctx.Done():
			return guardianapi.BatchRevision{}, ctx.Err()
		case <-s.release:
		}
	}
	return s.Store.UpsertFiles(ctx, batch)
}

func TestWriteIntentStateWritesPartitionRuntime(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := NewDispatcher(store, "test")
	guardianPartitionStatusCurrent.Reset()
	guardianIntentStatusCurrent.Reset()
	t.Cleanup(func() {
		guardianPartitionStatusCurrent.Reset()
		guardianIntentStatusCurrent.Reset()
	})

	if err := dispatch.WritePartitionState(ctx, &statedomain.PartitionState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "PartitionState",
		Partition:         "demo",
		Status:            "Compiled",
		IntentVersions:    map[string]string{"api": "intent-v1"},
		LastCompiledAt:    time.Date(2026, time.May, 6, 11, 59, 0, 0, time.UTC),
		LastReconciledAt:  time.Date(2026, time.May, 6, 11, 59, 0, 0, time.UTC),
		ConfigVersionID:   "config-v1",
		PartitionRevision: "partition-rev-v1",
	}); err != nil {
		t.Fatalf("WritePartitionState() error = %v", err)
	}

	state := &statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         "demo",
		Intent:            "api",
		Status:            statedomain.StatusHealthy,
		IntentVersionID:   "intent-v1",
		IntentSpecHash:    "hash-v1",
		PartitionRevision: "partition-rev-v1",
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "local"},
		Outputs:           map[string]string{"url": "https://demo.example"},
		LastTaskID:        "task-1",
		Timestamps: statedomain.StateTimestamps{
			LastQueuedAt: time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC),
		},
	}

	if err := dispatch.WriteIntentState(ctx, state); err != nil {
		t.Fatalf("WriteIntentState() error = %v", err)
	}

	raw, err := store.ReadFile(ctx, paths.PartitionRuntime("demo"))
	if err != nil {
		t.Fatalf("ReadFile(partition runtime) error = %v", err)
	}
	var runtime statedomain.PartitionRuntime
	if err := json.Unmarshal(raw, &runtime); err != nil {
		t.Fatalf("Unmarshal(partition runtime) error = %v", err)
	}
	if got := runtime.Intents["api"]; got == nil {
		t.Fatal("expected runtime snapshot to include api intent")
	} else if got.Outputs["url"] != "https://demo.example" {
		t.Fatalf("runtime outputs = %v", got.Outputs)
	}
	if runtime.PartitionState == nil {
		t.Fatal("expected runtime snapshot to include partition state")
	}
	if got, want := runtime.PartitionState.Status, "Healthy"; got != want {
		t.Fatalf("runtime partition status = %q, want %q", got, want)
	}
	partitionState, err := common.LoadPartitionState(ctx, store, "demo")
	if err != nil {
		t.Fatalf("LoadPartitionState() error = %v", err)
	}
	if got, want := partitionState.DisplayStatus, "Stable"; got != want {
		t.Fatalf("partition display status = %q, want %q", got, want)
	}
	if got, want := partitionState.Metrics.TotalIntents, 1; got != want {
		t.Fatalf("partition total intents = %d, want %d", got, want)
	}
}

func TestWriteIntentStateSerializesPartitionRuntimeUpdates(t *testing.T) {
	ctx := context.Background()
	baseStore := memory.New()
	store := &blockingUpsertStore{
		Store:   baseStore,
		entered: make(chan struct{}),
		release: make(chan struct{}),
		probed:  make(chan struct{}),
	}
	dispatch := NewDispatcher(store, "test")

	if err := dispatch.WritePartitionState(ctx, &statedomain.PartitionState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "PartitionState",
		Partition:         "demo",
		Status:            "Compiled",
		IntentVersions:    map[string]string{"api": "api-v1", "worker": "worker-v1"},
		ConfigVersionID:   "config-v1",
		PartitionRevision: "partition-rev-v1",
	}); err != nil {
		t.Fatalf("WritePartitionState() error = %v", err)
	}

	apiState := &statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         "demo",
		Intent:            "api",
		Status:            statedomain.StatusDiffing,
		IntentVersionID:   "api-v1",
		IntentSpecHash:    "hash-api-v1",
		PartitionRevision: "partition-rev-v1",
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "local"},
		Outputs:           map[string]string{"url": "https://demo.example"},
		LastTaskID:        "task-a",
		Timestamps: statedomain.StateTimestamps{
			LastQueuedAt: time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC),
		},
	}
	workerState := &statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         "demo",
		Intent:            "worker",
		Status:            statedomain.StatusHealthy,
		IntentVersionID:   "worker-v1",
		IntentSpecHash:    "hash-worker-v1",
		PartitionRevision: "partition-rev-v1",
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "local"},
		Outputs:           map[string]string{"cluster": "demo"},
		LastTaskID:        "worker-old",
		Timestamps: statedomain.StateTimestamps{
			LastQueuedAt: time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC),
		},
	}

	if err := dispatch.WriteIntentState(ctx, apiState); err != nil {
		t.Fatalf("seed api state: %v", err)
	}
	if err := dispatch.WriteIntentState(ctx, workerState); err != nil {
		t.Fatalf("seed worker state: %v", err)
	}

	store.mu.Lock()
	store.blockPath = paths.IntentState("demo", "worker")
	store.probePath = paths.IntentState("demo", "api")
	store.mu.Unlock()

	workerUpdate := statedomain.CloneIntentState(workerState)
	workerUpdate.LastTaskID = "worker-new"
	workerUpdate.Status = statedomain.StatusApplying
	workerUpdate.Timestamps.LastQueuedAt = time.Date(2026, time.May, 6, 12, 1, 0, 0, time.UTC)

	apiUpdate := statedomain.CloneIntentState(apiState)
	apiUpdate.LastTaskID = "task-b"
	apiUpdate.Status = statedomain.StatusChecking
	apiUpdate.Timestamps.LastQueuedAt = time.Date(2026, time.May, 6, 12, 1, 5, 0, time.UTC)

	workerErr := make(chan error, 1)
	go func() {
		workerErr <- dispatch.WriteIntentState(ctx, workerUpdate)
	}()

	select {
	case <-store.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocked worker write")
	}

	apiErr := make(chan error, 1)
	go func() {
		apiErr <- dispatch.WriteIntentState(ctx, apiUpdate)
	}()

	select {
	case <-store.probed:
		t.Fatal("concurrent partition write reached the store before the first write completed")
	case <-time.After(150 * time.Millisecond):
	}

	close(store.release)

	if err := <-workerErr; err != nil {
		t.Fatalf("worker WriteIntentState() error = %v", err)
	}
	if err := <-apiErr; err != nil {
		t.Fatalf("api WriteIntentState() error = %v", err)
	}

	directAPI, err := common.LoadIntentState(ctx, store, "demo", "api")
	if err != nil {
		t.Fatalf("LoadIntentState(api) error = %v", err)
	}
	if got, want := directAPI.LastTaskID, "task-b"; got != want {
		t.Fatalf("direct api LastTaskID = %q, want %q", got, want)
	}

	runtimeStates, err := common.LoadAllIntentStates(ctx, store, "demo")
	if err != nil {
		t.Fatalf("LoadAllIntentStates() error = %v", err)
	}
	if got, want := runtimeStates["api"].LastTaskID, "task-b"; got != want {
		t.Fatalf("runtime api LastTaskID = %q, want %q", got, want)
	}
	if got, want := runtimeStates["worker"].LastTaskID, "worker-new"; got != want {
		t.Fatalf("runtime worker LastTaskID = %q, want %q", got, want)
	}
}

func TestDeleteIntentStateDeletesRuntimeSnapshotAndFallsBackToScan(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dispatch := NewDispatcher(store, "test")
	guardianPartitionStatusCurrent.Reset()
	guardianIntentStatusCurrent.Reset()
	t.Cleanup(func() {
		guardianPartitionStatusCurrent.Reset()
		guardianIntentStatusCurrent.Reset()
	})

	if err := dispatch.WritePartitionState(ctx, &statedomain.PartitionState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "PartitionState",
		Partition:         "demo",
		Status:            "Compiled",
		IntentVersions:    map[string]string{"api": "api-v1", "worker": "worker-v1"},
		ConfigVersionID:   "config-v1",
		PartitionRevision: "partition-rev-v1",
	}); err != nil {
		t.Fatalf("WritePartitionState() error = %v", err)
	}

	writeState := func(intent string) {
		t.Helper()
		if err := dispatch.WriteIntentState(ctx, &statedomain.IntentState{
			APIVersion:        "guardian/v1alpha1",
			Kind:              "IntentState",
			Partition:         "demo",
			Intent:            intent,
			Status:            statedomain.StatusHealthy,
			IntentVersionID:   intent + "-v1",
			IntentSpecHash:    intent + "-hash",
			PartitionRevision: "partition-rev-v1",
			TargetPusher:      "local",
			Target:            targetdomain.Placement{Cluster: "local"},
			Outputs:           map[string]string{"intent": intent},
		}); err != nil {
			t.Fatalf("WriteIntentState(%s) error = %v", intent, err)
		}
	}

	writeState("api")
	writeState("worker")

	if err := dispatch.DeleteIntentState(ctx, "demo", "api", "", "test delete"); err != nil {
		t.Fatalf("DeleteIntentState() error = %v", err)
	}
	if _, err := store.ReadFile(ctx, paths.PartitionRuntime("demo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected runtime snapshot tombstone after delete, got err=%v", err)
	}
	if _, err := store.ReadFile(ctx, paths.PartitionState("demo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected partition state tombstone after delete, got err=%v", err)
	}

	states, err := common.LoadAllIntentStates(ctx, store, "demo")
	if err != nil {
		t.Fatalf("LoadAllIntentStates() error = %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("state count = %d, want 1", len(states))
	}
	if _, ok := states["api"]; ok {
		t.Fatalf("deleted intent still present in fallback-loaded states")
	}
	if got := states["worker"]; got == nil || got.Outputs["intent"] != "worker" {
		t.Fatalf("remaining state = %+v", got)
	}
	partitionState, err := common.LoadPartitionState(ctx, store, "demo")
	if err != nil {
		t.Fatalf("LoadPartitionState() fallback error = %v", err)
	}
	if got, want := partitionState.Status, "Healthy"; got != want {
		t.Fatalf("fallback partition status = %q, want %q", got, want)
	}
	if got, want := partitionState.Metrics.TotalIntents, 1; got != want {
		t.Fatalf("fallback partition total intents = %d, want %d", got, want)
	}
}

func TestSeedRuntimeMetricsHydratesCacheAndGauges(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	seedPartitionConfig(t, ctx, store, "alpha")
	seedPartitionConfig(t, ctx, store, "beta")

	seedJSON(t, ctx, store, paths.PartitionRuntime("alpha"), &statedomain.PartitionRuntime{
		APIVersion: "guardian/v1alpha1",
		Kind:       "PartitionRuntime",
		Partition:  "alpha",
		PartitionState: &statedomain.PartitionState{
			APIVersion:        "guardian/v1alpha1",
			Kind:              "PartitionState",
			Partition:         "alpha",
			Status:            "Compiled",
			IntentVersions:    map[string]string{"api": "api-v1"},
			ConfigVersionID:   "config-a",
			PartitionRevision: "part-a",
		},
		Intents: map[string]*statedomain.IntentState{
			"api": {
				APIVersion:        "guardian/v1alpha1",
				Kind:              "IntentState",
				Partition:         "alpha",
				Intent:            "api",
				Status:            statedomain.StatusHealthy,
				IntentVersionID:   "api-v1",
				IntentSpecHash:    "hash-a",
				PartitionRevision: "part-a",
				TargetPusher:      "local",
			},
		},
	})

	seedJSON(t, ctx, store, paths.PartitionState("beta"), &statedomain.PartitionState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "PartitionState",
		Partition:         "beta",
		Status:            "Compiled",
		IntentVersions:    map[string]string{"worker": "worker-v1"},
		ConfigVersionID:   "config-b",
		PartitionRevision: "part-b",
	})
	seedJSON(t, ctx, store, paths.IntentState("beta", "worker"), &statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         "beta",
		Intent:            "worker",
		Status:            statedomain.StatusDrifted,
		IntentVersionID:   "worker-v1",
		IntentSpecHash:    "hash-b",
		PartitionRevision: "part-b",
		TargetPusher:      "local",
	})

	guardianPartitionStatusCurrent.Reset()
	guardianIntentStatusCurrent.Reset()
	t.Cleanup(func() {
		guardianPartitionStatusCurrent.Reset()
		guardianIntentStatusCurrent.Reset()
	})

	dispatch := NewDispatcher(store, "test")
	if err := dispatch.SeedRuntimeMetrics(ctx); err != nil {
		t.Fatalf("SeedRuntimeMetrics() error = %v", err)
	}

	if got, want := len(dispatch.runtimeCache), 2; got != want {
		t.Fatalf("runtime cache size = %d, want %d", got, want)
	}
	if got, want := dispatch.runtimeCache["alpha"].PartitionState.Status, "Healthy"; got != want {
		t.Fatalf("alpha partition status = %q, want %q", got, want)
	}
	if got, want := dispatch.runtimeCache["beta"].PartitionState.Status, "Attention"; got != want {
		t.Fatalf("beta partition status = %q, want %q", got, want)
	}
	if got, want := gatherGaugeValue(t, "guardian_dispatcher_partition_status_current", map[string]string{"status": "Healthy"}), 1.0; got != want {
		t.Fatalf("healthy partition gauge = %v, want %v", got, want)
	}
	if got, want := gatherGaugeValue(t, "guardian_dispatcher_partition_status_current", map[string]string{"status": "Attention"}), 1.0; got != want {
		t.Fatalf("attention partition gauge = %v, want %v", got, want)
	}
	if got, want := gatherGaugeValue(t, "guardian_dispatcher_intent_status_current", map[string]string{"status": string(statedomain.StatusHealthy)}), 1.0; got != want {
		t.Fatalf("healthy intent gauge = %v, want %v", got, want)
	}
	if got, want := gatherGaugeValue(t, "guardian_dispatcher_intent_status_current", map[string]string{"status": string(statedomain.StatusDrifted)}), 1.0; got != want {
		t.Fatalf("drifted intent gauge = %v, want %v", got, want)
	}
	if got := gatherGaugeValue(t, "guardian_dispatcher_partition_status_current", map[string]string{"status": "Progressing"}); got != 0 {
		t.Fatalf("progressing partition gauge = %v, want 0", got)
	}
	if got := gatherGaugeValue(t, "guardian_dispatcher_intent_status_current", map[string]string{"status": string(statedomain.StatusChecking)}); got != 0 {
		t.Fatalf("checking intent gauge = %v, want 0", got)
	}
}

func seedPartitionConfig(t *testing.T, ctx context.Context, store guardianapi.WriteStore, partition string) {
	t.Helper()
	seedRaw(t, ctx, store, paths.PartitionConfig(partition), []byte("apiVersion: guardian/v1alpha1\nkind: Partition\nmetadata:\n  name: "+partition+"\nspec:\n  reconciliation:\n    mode: auto\n"))
}

func seedJSON(t *testing.T, ctx context.Context, store guardianapi.WriteStore, logicalPath string, value any) {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal(%s) error = %v", logicalPath, err)
	}
	seedRaw(t, ctx, store, logicalPath, content)
}

func seedRaw(t *testing.T, ctx context.Context, store guardianapi.WriteStore, logicalPath string, content []byte) {
	t.Helper()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed fixture"},
	}); err != nil {
		t.Fatalf("UpsertFiles(%s) error = %v", logicalPath, err)
	}
}

func gatherGaugeValue(t *testing.T, metricName string, labels map[string]string) float64 {
	t.Helper()
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, family := range metricFamilies {
		if family.GetName() != metricName {
			continue
		}
		for _, metric := range family.GetMetric() {
			if !metricHasLabels(metric.GetLabel(), labels) {
				continue
			}
			if metric.Gauge == nil {
				return 0
			}
			return metric.GetGauge().GetValue()
		}
	}
	return 0
}

func metricHasLabels(metricLabels []*dto.LabelPair, labels map[string]string) bool {
	if len(labels) == 0 {
		return len(metricLabels) == 0
	}
	for key, want := range labels {
		matched := false
		for _, pair := range metricLabels {
			if pair.GetName() == key && pair.GetValue() == want {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
