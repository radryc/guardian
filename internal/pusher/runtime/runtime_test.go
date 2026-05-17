package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/drivers"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRuntimeClaimAndApplyResultFlow(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reg := registry.New()
	reg.Register(drivers.NewComputeDriver())

	writeTask := func(taskID string) {
		t.Helper()
		task := taskdomain.Task{
			APIVersion:      "guardian/v1alpha1",
			Kind:            "Task",
			TaskID:          taskID,
			Partition:       "payments",
			Intent:          "api",
			Op:              taskdomain.OpApply,
			TargetPusher:    "local",
			Target:          targetdomain.Placement{Cluster: "local"},
			AssetVersionIDs: map[string]string{"backend": "asset-v1"},
			Assets: []taskdomain.AbstractAsset{{
				Type: "Compute",
				Name: "backend",
				Properties: map[string]any{
					"image": "ghcr.io/example/api:v1",
				},
			}},
		}
		content, err := json.Marshal(task)
		if err != nil {
			t.Fatalf("Marshal(task) error = %v", err)
		}
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{
				LogicalPath: paths.QueueTask("local", taskID),
				Content:     content,
			}},
			Context: guardianapi.MutationContext{PrincipalID: "guardiand", Reason: "seed task"},
		}); err != nil {
			t.Fatalf("UpsertFiles(task) error = %v", err)
		}
	}

	r1 := &Runtime{QueuePath: paths.QueueDir("local"), WorkerID: "worker-1", Store: store, Registry: reg}
	r2 := &Runtime{QueuePath: paths.QueueDir("local"), WorkerID: "worker-2", Store: store, Registry: reg}

	writeTask("task-claim")
	if _, claimed, err := r1.tryClaimTask(ctx, "task-claim"); err != nil {
		t.Fatalf("tryClaimTask() error = %v", err)
	} else if !claimed {
		t.Fatalf("expected first worker to claim task")
	}
	if _, claimed, err := r2.tryClaimTask(ctx, "task-claim"); err != nil {
		t.Fatalf("second tryClaimTask() error = %v", err)
	} else if claimed {
		t.Fatalf("expected second worker claim to fail")
	}

	writeTask("task-run")
	if err := r1.processPending(ctx); err != nil {
		t.Fatalf("processPending() error = %v", err)
	}

	resultContent, err := store.ReadFile(ctx, paths.QueueResult("local", "task-run"))
	if err != nil {
		t.Fatalf("ReadFile(result) error = %v", err)
	}

	var result taskdomain.TaskResult
	if err := json.Unmarshal(resultContent, &result); err != nil {
		t.Fatalf("Unmarshal(result) error = %v", err)
	}
	if result.Status != taskdomain.ResultSucceeded {
		t.Fatalf("unexpected result status: %s", result.Status)
	}
	if result.Op != taskdomain.OpApply {
		t.Fatalf("unexpected result op: %s", result.Op)
	}
	if result.Outputs["backend.id"] == "" {
		t.Fatalf("expected driver outputs, got %v", result.Outputs)
	}
}

func TestTryClaimTaskMissingTaskFileIsBenign(t *testing.T) {
	ctx := context.Background()
	r := &Runtime{QueuePath: paths.QueueDir("local"), WorkerID: "worker-1", Store: memory.New()}

	task, claimed, err := r.tryClaimTask(ctx, "missing-task")
	if err != nil {
		t.Fatalf("tryClaimTask() error = %v", err)
	}
	if claimed {
		t.Fatal("expected missing task file to remain unclaimed")
	}
	if task != nil {
		t.Fatalf("expected no task payload, got %+v", task)
	}
}

// TestRuntimeCheckOperation verifies that a CHECK task calls driver.Check
// and writes a Succeeded result with no Drift report.
func TestRuntimeCheckOperation(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reg := registry.New()
	reg.Register(drivers.NewComputeDriver())

	task := taskdomain.Task{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "Task",
		TaskID:       "task-check",
		Partition:    "demo",
		Intent:       "svc",
		Op:           taskdomain.OpCheck,
		TargetPusher: "local",
		Target:       targetdomain.Placement{Cluster: "local"},
		Assets: []taskdomain.AbstractAsset{{
			Type:       "Compute",
			Name:       "web",
			Properties: map[string]any{"image": "nginx:latest"},
		}},
	}
	content, _ := json.Marshal(task)
	store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("local", task.TaskID), Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
	})

	r := &Runtime{QueuePath: paths.QueueDir("local"), WorkerID: "w1", Store: store, Registry: reg}
	if err := r.processPending(ctx); err != nil {
		t.Fatalf("processPending: %v", err)
	}

	raw, err := store.ReadFile(ctx, paths.QueueResult("local", task.TaskID))
	if err != nil {
		t.Fatalf("ReadFile result: %v", err)
	}
	var result taskdomain.TaskResult
	json.Unmarshal(raw, &result)
	if result.Status != taskdomain.ResultSucceeded {
		t.Fatalf("status = %q, want Succeeded", result.Status)
	}
	if result.Op != taskdomain.OpCheck {
		t.Fatalf("op = %q, want CHECK", result.Op)
	}
	if result.Drift != nil {
		t.Fatalf("CHECK result must not include Drift; got %+v", result.Drift)
	}
}

// TestRuntimeDiffOperation verifies that a DIFF task on a not-yet-applied
// driver reports Changed drift.
func TestRuntimeDiffOperation(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reg := registry.New()
	reg.Register(drivers.NewComputeDriver())

	task := taskdomain.Task{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "Task",
		TaskID:       "task-diff",
		Partition:    "demo",
		Intent:       "svc",
		Op:           taskdomain.OpDiff,
		TargetPusher: "local",
		Target:       targetdomain.Placement{Cluster: "local"},
		Assets: []taskdomain.AbstractAsset{{
			Type:       "Compute",
			Name:       "web",
			Properties: map[string]any{"image": "nginx:latest"},
		}},
	}
	content, _ := json.Marshal(task)
	store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("local", task.TaskID), Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
	})

	r := &Runtime{QueuePath: paths.QueueDir("local"), WorkerID: "w1", Store: store, Registry: reg}
	if err := r.processPending(ctx); err != nil {
		t.Fatalf("processPending: %v", err)
	}

	raw, err := store.ReadFile(ctx, paths.QueueResult("local", task.TaskID))
	if err != nil {
		t.Fatalf("ReadFile result: %v", err)
	}
	var result taskdomain.TaskResult
	json.Unmarshal(raw, &result)
	if result.Status != taskdomain.ResultSucceeded {
		t.Fatalf("status = %q, want Succeeded", result.Status)
	}
	if result.Drift == nil {
		t.Fatal("DIFF result must include Drift")
	}
	if result.Drift.Status != "Changed" {
		t.Fatalf("Drift.Status = %q, want Changed (asset not yet applied)", result.Drift.Status)
	}
}

// TestRuntimeDiffAfterApply verifies that DIFF on an already-applied asset
// reports InSync.
func TestRuntimeDiffAfterApply(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reg := registry.New()
	compute := drivers.NewComputeDriver()
	reg.Register(compute)

	r := &Runtime{QueuePath: paths.QueueDir("local"), WorkerID: "w1", Store: store, Registry: reg}

	runTask := func(id string, op taskdomain.Operation) taskdomain.TaskResult {
		task := taskdomain.Task{
			APIVersion:   "guardian/v1alpha1",
			Kind:         "Task",
			TaskID:       id,
			Partition:    "demo",
			Intent:       "svc",
			Op:           op,
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Assets: []taskdomain.AbstractAsset{{
				Type:       "Compute",
				Name:       "web",
				Properties: map[string]any{"image": "nginx:stable"},
			}},
		}
		content, _ := json.Marshal(task)
		store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("local", id), Content: content}},
			Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
		})
		r.processPending(ctx)
		raw, _ := store.ReadFile(ctx, paths.QueueResult("local", id))
		var res taskdomain.TaskResult
		json.Unmarshal(raw, &res)
		return res
	}

	// Apply first so the driver has state.
	applyRes := runTask("t-apply", taskdomain.OpApply)
	if applyRes.Status != taskdomain.ResultSucceeded {
		t.Fatalf("apply status = %q", applyRes.Status)
	}

	// Diff should now report InSync.
	diffRes := runTask("t-diff-after", taskdomain.OpDiff)
	if diffRes.Drift == nil {
		t.Fatal("DIFF result missing Drift")
	}
	if diffRes.Drift.Status != "InSync" {
		t.Fatalf("Drift.Status = %q, want InSync after apply", diffRes.Drift.Status)
	}
	if diffRes.AssetObservations == nil || diffRes.AssetObservations["web"] == nil {
		t.Fatalf("AssetObservations = %+v, want web observation", diffRes.AssetObservations)
	}
	if got := diffRes.AssetObservations["web"].Health; got == nil || got.Status != taskdomain.HealthHealthy {
		t.Fatalf("web health = %+v, want healthy", got)
	}
	if got := diffRes.AssetObservations["web"].ApplyReadiness; got == nil || got.Status != taskdomain.ApplyReadinessReady {
		t.Fatalf("web applyReadiness = %+v, want ready", got)
	}
}

type applyObservedDriver struct{}

func (d *applyObservedDriver) Type() string { return "ObservedApply" }

func (d *applyObservedDriver) Validate(map[string]any) error { return nil }

func (d *applyObservedDriver) Check(context.Context, registry.AssetInput) error { return nil }

func (d *applyObservedDriver) Diff(context.Context, registry.AssetInput) (taskdomain.DriftReport, error) {
	return taskdomain.DriftReport{Status: "InSync", Summary: "driver state matches"}, nil
}

func (d *applyObservedDriver) Apply(context.Context, registry.AssetInput) (registry.AssetResult, error) {
	return registry.AssetResult{Outputs: map[string]string{"id": "observed-asset"}}, nil
}

func (d *applyObservedDriver) Destroy(context.Context, registry.AssetInput) error { return nil }

func (d *applyObservedDriver) ObserveState(context.Context, registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "driver observed ImagePullBackOff"}, &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "dependencies resolved"}, nil
}

func TestRuntimeApplyIncludesObservedState(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reg := registry.New()
	reg.Register(&applyObservedDriver{})

	r := &Runtime{QueuePath: paths.QueueDir("local"), WorkerID: "w1", Store: store, Registry: reg}
	task := &taskdomain.Task{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "Task",
		TaskID:       "task-apply-observed",
		Partition:    "demo",
		Intent:       "svc",
		Op:           taskdomain.OpApply,
		TargetPusher: "local",
		Target:       targetdomain.Placement{Cluster: "local"},
		Assets: []taskdomain.AbstractAsset{{
			Type: "ObservedApply",
			Name: "web",
		}},
	}

	result := r.executeTask(ctx, task)
	if result.Status != taskdomain.ResultSucceeded {
		t.Fatalf("executeTask() status = %q, want %q", result.Status, taskdomain.ResultSucceeded)
	}
	if result.Health == nil || result.Health.Status != taskdomain.HealthUnhealthy {
		t.Fatalf("Health = %+v, want unhealthy", result.Health)
	}
	if result.ApplyReadiness == nil || result.ApplyReadiness.Status != taskdomain.ApplyReadinessReady {
		t.Fatalf("ApplyReadiness = %+v, want ready", result.ApplyReadiness)
	}
	if result.AssetObservations == nil || result.AssetObservations["web"] == nil {
		t.Fatalf("AssetObservations = %+v, want web observation", result.AssetObservations)
	}
	if got := result.AssetObservations["web"].Health; got == nil || got.Status != taskdomain.HealthUnhealthy {
		t.Fatalf("web health = %+v, want unhealthy", got)
	}
}

func TestRuntimeExecuteTaskTraceIncludesDebugJSON(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reg := registry.New()
	reg.Register(drivers.NewComputeDriver())

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		otel.SetTracerProvider(previousProvider)
		_ = tp.Shutdown(ctx)
	}()

	r := &Runtime{WorkerID: "worker-trace", Store: store, Registry: reg}
	task := &taskdomain.Task{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "Task",
		TaskID:            "task-trace",
		CorrelationID:     "corr-123",
		Partition:         "payments",
		Intent:            "api",
		Op:                taskdomain.OpApply,
		TargetPusher:      "local",
		Target:            targetdomain.Placement{Cluster: "cluster-a", Namespace: "payments", Region: "eu-west-1", Account: "acct-7"},
		PartitionRevision: "part-rev-9",
		IntentVersionID:   "intent-v7",
		IntentSpecHash:    "hash-abc",
		AssetVersionIDs:   map[string]string{"backend": "asset-v1"},
		CreatedAt:         time.Date(2026, 4, 22, 13, 37, 17, 0, time.UTC),
		Assets: []taskdomain.AbstractAsset{{
			Type:      "Compute",
			Name:      "backend",
			DependsOn: []string{"db"},
			Payload:   map[string]string{"TOKEN": "super-secret", "ENV": "prod"},
			Properties: map[string]any{
				"image":    "ghcr.io/example/api:v1",
				"password": "dont-log-this",
			},
		}},
	}

	result := r.executeTask(ctx, task)
	if result.Status != taskdomain.ResultSucceeded {
		t.Fatalf("executeTask() status = %q, want %q", result.Status, taskdomain.ResultSucceeded)
	}

	span := findEndedSpanByName(t, recorder.Ended(), "guardian.task.execute")
	attrs := spanAttributes(span.Attributes())
	if got := attrs["guardian.task.correlation_id"].AsString(); got != "corr-123" {
		t.Fatalf("guardian.task.correlation_id = %q, want corr-123", got)
	}
	if got := attrs["guardian.worker.id"].AsString(); got != "worker-trace" {
		t.Fatalf("guardian.worker.id = %q, want worker-trace", got)
	}
	if got := attrs["guardian.result.status"].AsString(); got != string(taskdomain.ResultSucceeded) {
		t.Fatalf("guardian.result.status = %q, want %q", got, taskdomain.ResultSucceeded)
	}
	if got := attrs["guardian.asset.count"].AsInt64(); got != 1 {
		t.Fatalf("guardian.asset.count = %d, want 1", got)
	}

	inputJSON := findEventAttr(t, span.Events(), "guardian.task.input", "guardian.task.input_json")
	if strings.Contains(inputJSON, "super-secret") || strings.Contains(inputJSON, "dont-log-this") {
		t.Fatalf("input trace JSON leaked raw secret values: %s", inputJSON)
	}
	var inputSummary struct {
		CorrelationID string `json:"correlationID"`
		WorkerID      string `json:"workerID"`
		Assets        []struct {
			PayloadKeys  []string `json:"payloadKeys"`
			PropertyKeys []string `json:"propertyKeys"`
		} `json:"assets"`
	}
	if err := json.Unmarshal([]byte(inputJSON), &inputSummary); err != nil {
		t.Fatalf("Unmarshal(inputJSON) error = %v", err)
	}
	if inputSummary.CorrelationID != "corr-123" {
		t.Fatalf("input correlationID = %q, want corr-123", inputSummary.CorrelationID)
	}
	if inputSummary.WorkerID != "worker-trace" {
		t.Fatalf("input workerID = %q, want worker-trace", inputSummary.WorkerID)
	}
	if len(inputSummary.Assets) != 1 {
		t.Fatalf("input assets len = %d, want 1", len(inputSummary.Assets))
	}
	if got := strings.Join(inputSummary.Assets[0].PayloadKeys, ","); got != "ENV,TOKEN" {
		t.Fatalf("payload keys = %q, want ENV,TOKEN", got)
	}
	if got := strings.Join(inputSummary.Assets[0].PropertyKeys, ","); got != "image,password" {
		t.Fatalf("property keys = %q, want image,password", got)
	}

	resultJSON := findEventAttr(t, span.Events(), "guardian.task.result", "guardian.task.result_json")
	var resultSummary struct {
		Status     string   `json:"status"`
		OutputKeys []string `json:"outputKeys"`
		Logs       []struct {
			Message string `json:"message"`
		} `json:"logs"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &resultSummary); err != nil {
		t.Fatalf("Unmarshal(resultJSON) error = %v", err)
	}
	if resultSummary.Status != string(taskdomain.ResultSucceeded) {
		t.Fatalf("result status = %q, want %q", resultSummary.Status, taskdomain.ResultSucceeded)
	}
	outputKeys := map[string]struct{}{}
	for _, key := range resultSummary.OutputKeys {
		outputKeys[key] = struct{}{}
	}
	for _, key := range []string{"backend.id", "id", "image", "running"} {
		if _, ok := outputKeys[key]; !ok {
			t.Fatalf("output keys missing %q: %v", key, resultSummary.OutputKeys)
		}
	}
	if len(resultSummary.Logs) == 0 {
		t.Fatal("expected task result logs in trace JSON")
	}
	if !strings.Contains(resultSummary.Logs[len(resultSummary.Logs)-1].Message, "task completed") {
		t.Fatalf("last result log = %q, want task completed", resultSummary.Logs[len(resultSummary.Logs)-1].Message)
	}
}

func findEndedSpanByName(t *testing.T, spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if span.Name() == name {
			return span
		}
	}
	t.Fatalf("span %q not found; got %d spans", name, len(spans))
	return nil
}

func spanAttributes(attrs []attribute.KeyValue) map[string]attribute.Value {
	out := make(map[string]attribute.Value, len(attrs))
	for _, attr := range attrs {
		out[string(attr.Key)] = attr.Value
	}
	return out
}

func findEventAttr(t *testing.T, events []sdktrace.Event, eventName, attrKey string) string {
	t.Helper()
	for _, event := range events {
		if event.Name != eventName {
			continue
		}
		for _, attr := range event.Attributes {
			if string(attr.Key) == attrKey {
				return attr.Value.AsString()
			}
		}
	}
	t.Fatalf("event %q with attr %q not found", eventName, attrKey)
	return ""
}

// TestRuntimeDestroyOperation verifies that DESTROY writes a Succeeded result
// and the driver removes the instance (subsequent Diff shows Changed).
func TestRuntimeDestroyOperation(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reg := registry.New()
	compute := drivers.NewComputeDriver()
	reg.Register(compute)

	r := &Runtime{QueuePath: paths.QueueDir("local"), WorkerID: "w1", Store: store, Registry: reg}

	runTask := func(id string, op taskdomain.Operation) taskdomain.TaskResult {
		task := taskdomain.Task{
			APIVersion:   "guardian/v1alpha1",
			Kind:         "Task",
			TaskID:       id,
			Partition:    "demo",
			Intent:       "svc",
			Op:           op,
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Assets: []taskdomain.AbstractAsset{{
				Type:       "Compute",
				Name:       "web",
				Properties: map[string]any{"image": "img:v1"},
			}},
		}
		content, _ := json.Marshal(task)
		store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("local", id), Content: content}},
			Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
		})
		r.processPending(ctx)
		raw, _ := store.ReadFile(ctx, paths.QueueResult("local", id))
		var res taskdomain.TaskResult
		json.Unmarshal(raw, &res)
		return res
	}

	runTask("apply-before-destroy", taskdomain.OpApply)

	destroyRes := runTask("destroy-1", taskdomain.OpDestroy)
	if destroyRes.Status != taskdomain.ResultSucceeded {
		t.Fatalf("destroy status = %q, want Succeeded", destroyRes.Status)
	}

	diffRes := runTask("diff-after-destroy", taskdomain.OpDiff)
	if diffRes.Drift == nil || diffRes.Drift.Status != "Changed" {
		t.Fatalf("after destroy: drift = %+v, want Changed (instance gone)", diffRes.Drift)
	}
}

// TestRuntimeDatabaseDriver verifies that the Database driver can execute
// Check, Diff, Apply, and Destroy through the runtime.
func TestRuntimeDatabaseDriver(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reg := registry.New()
	dbDriver := drivers.NewDatabaseDriver()
	reg.Register(dbDriver)
	reg.RegisterAs(assetdomain.TypeSQLDatabase, dbDriver)

	r := &Runtime{QueuePath: paths.QueueDir("local"), WorkerID: "w1", Store: store, Registry: reg}

	runDB := func(id string, op taskdomain.Operation, assetType string) taskdomain.TaskResult {
		task := taskdomain.Task{
			APIVersion:   "guardian/v1alpha1",
			Kind:         "Task",
			TaskID:       id,
			Partition:    "demo",
			Intent:       "data",
			Op:           op,
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Assets: []taskdomain.AbstractAsset{{
				Type:       assetType,
				Name:       "primary",
				Properties: map[string]any{"engine": "postgres"},
			}},
		}
		content, _ := json.Marshal(task)
		store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("local", id), Content: content}},
			Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
		})
		r.processPending(ctx)
		raw, _ := store.ReadFile(ctx, paths.QueueResult("local", id))
		var res taskdomain.TaskResult
		json.Unmarshal(raw, &res)
		return res
	}

	if res := runDB("db-check", taskdomain.OpCheck, assetdomain.TypeDatabase); res.Status != taskdomain.ResultSucceeded {
		t.Fatalf("db check status = %q", res.Status)
	}
	if res := runDB("db-diff", taskdomain.OpDiff, assetdomain.TypeDatabase); res.Drift == nil || res.Drift.Status != "Changed" {
		t.Fatalf("db diff before apply: %+v", res.Drift)
	}
	applyRes := runDB("db-apply", taskdomain.OpApply, assetdomain.TypeDatabase)
	if applyRes.Status != taskdomain.ResultSucceeded {
		t.Fatalf("db apply status = %q", applyRes.Status)
	}
	if applyRes.Outputs["primary.url"] == "" {
		t.Fatalf("db apply outputs missing url: %v", applyRes.Outputs)
	}
	if res := runDB("db-diff2", taskdomain.OpDiff, assetdomain.TypeDatabase); res.Drift == nil || res.Drift.Status != "InSync" {
		t.Fatalf("db diff after apply: %+v", res.Drift)
	} else if res.AssetObservations == nil || res.AssetObservations["primary"] == nil {
		t.Fatalf("db asset observations = %+v, want primary observation", res.AssetObservations)
	} else if got := res.AssetObservations["primary"].Health; got == nil || got.Status != taskdomain.HealthHealthy {
		t.Fatalf("db health = %+v, want healthy", got)
	}
	if res := runDB("sql-db-check", taskdomain.OpCheck, assetdomain.TypeSQLDatabase); res.Status != taskdomain.ResultSucceeded {
		t.Fatalf("sql db check status = %q", res.Status)
	}
}

func TestRuntimeCanHandleSkipsMismatchedTask(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reg := registry.New()
	reg.Register(drivers.NewComputeDriver())

	task := taskdomain.Task{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "Task",
		TaskID:       "task-skip",
		Partition:    "demo",
		Intent:       "svc",
		Op:           taskdomain.OpApply,
		TargetPusher: "local",
		Target:       targetdomain.Placement{Cluster: "other"},
		Assets: []taskdomain.AbstractAsset{{
			Type:       "Compute",
			Name:       "web",
			Properties: map[string]any{"image": "nginx:latest"},
		}},
	}
	content, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal(task) error = %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("local", task.TaskID), Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
	}); err != nil {
		t.Fatalf("UpsertFiles(task) error = %v", err)
	}

	r := &Runtime{
		QueuePath: paths.QueueDir("local"),
		WorkerID:  "w1",
		Store:     store,
		Registry:  reg,
		CanHandle: func(task *taskdomain.Task) bool { return task.Target.Cluster == "local" },
	}
	if err := r.processPending(ctx); err != nil {
		t.Fatalf("processPending: %v", err)
	}

	if _, err := store.ReadFile(ctx, paths.QueueClaim("local", task.TaskID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no claim for skipped task, got %v", err)
	}
	if _, err := store.ReadFile(ctx, paths.QueueResult("local", task.TaskID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no result for skipped task, got %v", err)
	}
}

func TestRuntimeBacksOffRepeatedUnclaimedTaskChecks(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reg := registry.New()
	reg.Register(drivers.NewComputeDriver())

	task := taskdomain.Task{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "Task",
		TaskID:       "task-backoff",
		Partition:    "demo",
		Intent:       "svc",
		Op:           taskdomain.OpApply,
		TargetPusher: "local",
		Target:       targetdomain.Placement{Cluster: "other"},
		Assets: []taskdomain.AbstractAsset{{
			Type:       "Compute",
			Name:       "web",
			Properties: map[string]any{"image": "nginx:latest"},
		}},
	}
	content, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal(task) error = %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("local", task.TaskID), Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
	}); err != nil {
		t.Fatalf("UpsertFiles(task) error = %v", err)
	}

	countingStore := &readCountingStore{Store: store, countedPath: paths.QueueTask("local", task.TaskID)}
	r := &Runtime{
		QueuePath:               paths.QueueDir("local"),
		WorkerID:                "w1",
		Store:                   countingStore,
		Registry:                reg,
		UnclaimedTaskRetryDelay: time.Minute,
		CanHandle:               func(task *taskdomain.Task) bool { return task.Target.Cluster == "local" },
	}

	if err := r.processPending(ctx); err != nil {
		t.Fatalf("first processPending: %v", err)
	}
	if got, want := countingStore.readCount, 1; got != want {
		t.Fatalf("task ReadFile count after first scan = %d, want %d", got, want)
	}

	if err := r.processPending(ctx); err != nil {
		t.Fatalf("second processPending: %v", err)
	}
	if got, want := countingStore.readCount, 1; got != want {
		t.Fatalf("task ReadFile count after immediate retry = %d, want %d", got, want)
	}
	if _, err := countingStore.ReadFile(ctx, paths.QueueClaim("local", task.TaskID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no claim for backed off task, got %v", err)
	}
	if _, err := countingStore.ReadFile(ctx, paths.QueueResult("local", task.TaskID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no result for backed off task, got %v", err)
	}
}

func TestRuntimeSkipsTaskWithExistingResult(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reg := registry.New()
	reg.Register(drivers.NewComputeDriver())

	task := taskdomain.Task{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "Task",
		TaskID:       "task-done",
		Partition:    "demo",
		Intent:       "svc",
		Op:           taskdomain.OpApply,
		TargetPusher: "local",
		Target:       targetdomain.Placement{Cluster: "local"},
		Assets: []taskdomain.AbstractAsset{{
			Type:       "Compute",
			Name:       "web",
			Properties: map[string]any{"image": "nginx:latest"},
		}},
	}
	content, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal(task) error = %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("local", task.TaskID), Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
	}); err != nil {
		t.Fatalf("UpsertFiles(task) error = %v", err)
	}
	writeJSON := func(path string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("Marshal(%s) error = %v", path, err)
		}
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes:  []guardianapi.PathWrite{{LogicalPath: path, Content: data}},
			Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
		}); err != nil {
			t.Fatalf("UpsertFiles(%s) error = %v", path, err)
		}
	}
	writeJSON(paths.QueueResult("local", task.TaskID), taskdomain.TaskResult{
		TaskID: task.TaskID, Op: taskdomain.OpApply, Status: taskdomain.ResultSucceeded, Partition: "demo", Intent: "svc", Pusher: "local",
	})

	r := &Runtime{QueuePath: paths.QueueDir("local"), WorkerID: "w1", Store: store, Registry: reg}
	if err := r.processPending(ctx); err != nil {
		t.Fatalf("processPending: %v", err)
	}

	if _, err := store.ReadFile(ctx, paths.QueueClaim("local", task.TaskID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no claim for completed task, got %v", err)
	}
}

func TestRuntimeReclaimsExpiredClaim(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reg := registry.New()
	reg.Register(drivers.NewComputeDriver())

	task := taskdomain.Task{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "Task",
		TaskID:       "task-expired-claim",
		Partition:    "demo",
		Intent:       "svc",
		Op:           taskdomain.OpApply,
		TargetPusher: "local",
		Target:       targetdomain.Placement{Cluster: "local"},
		Assets: []taskdomain.AbstractAsset{{
			Type:       "Compute",
			Name:       "web",
			Properties: map[string]any{"image": "nginx:latest"},
		}},
	}
	taskContent, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal(task) error = %v", err)
	}
	claimContent, err := json.Marshal(taskdomain.ClaimFile{
		TaskID:       task.TaskID,
		WorkerID:     "dead-worker",
		ClaimedAt:    time.Now().Add(-10 * time.Minute).UTC(),
		LeaseSeconds: 30,
	})
	if err != nil {
		t.Fatalf("Marshal(claim) error = %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{
			{LogicalPath: paths.QueueTask("local", task.TaskID), Content: taskContent},
			{LogicalPath: paths.QueueClaim("local", task.TaskID), Content: claimContent},
		},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
	}); err != nil {
		t.Fatalf("UpsertFiles(seed) error = %v", err)
	}

	r := &Runtime{QueuePath: paths.QueueDir("local"), WorkerID: "w2", Store: store, Registry: reg}
	if err := r.processPending(ctx); err != nil {
		t.Fatalf("processPending: %v", err)
	}

	raw, err := store.ReadFile(ctx, paths.QueueResult("local", task.TaskID))
	if err != nil {
		t.Fatalf("ReadFile(result) error = %v", err)
	}
	var result taskdomain.TaskResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("Unmarshal(result) error = %v", err)
	}
	if result.Status != taskdomain.ResultSucceeded {
		t.Fatalf("result.Status = %q, want Succeeded", result.Status)
	}
	claimRaw, err := store.ReadFile(ctx, paths.QueueClaim("local", task.TaskID))
	if err != nil {
		t.Fatalf("ReadFile(claim) error = %v", err)
	}
	var claim taskdomain.ClaimFile
	if err := json.Unmarshal(claimRaw, &claim); err != nil {
		t.Fatalf("Unmarshal(claim) error = %v", err)
	}
	if claim.WorkerID != "w2" {
		t.Fatalf("claim.WorkerID = %q, want w2", claim.WorkerID)
	}
}

type readCountingStore struct {
	guardianapi.Store
	countedPath string
	readCount   int
}

func (s *readCountingStore) ReadFile(ctx context.Context, logicalPath string) ([]byte, error) {
	if logicalPath == s.countedPath {
		s.readCount++
	}
	return s.Store.ReadFile(ctx, logicalPath)
}
