package runtime

import (
	"context"
	"testing"

	targetdomain "github.com/radryc/guardian/internal/domain/target"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/drivers"
	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/store/memory"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestExecuteTaskStartsNewTracePerTask guards against otelc-style goroutine
// local span leaks: if the run loop context inherits a long-lived streaming
// span, every task must still get its own trace instead of joining that
// ever-growing process-lifetime trace.
func TestExecuteTaskStartsNewTracePerTask(t *testing.T) {
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

	// Simulate a long-lived span (streaming watch) that never ends and leaks
	// into the context handed to the runtime loop.
	leakedCtx, leaked := tp.Tracer("test").Start(ctx, "long-lived.stream")
	defer leaked.End()
	leakedTraceID := trace.SpanContextFromContext(leakedCtx).TraceID()
	if !leakedTraceID.IsValid() {
		t.Fatal("leaked parent span has an invalid trace ID")
	}

	r := &Runtime{WorkerID: "worker-trace-root", Store: store, Registry: reg}
	task := &taskdomain.Task{
		APIVersion:      "guardian/v1alpha1",
		Kind:            "Task",
		TaskID:          "task-trace-root",
		Partition:       "payments",
		Intent:          "api",
		Op:              taskdomain.OpApply,
		TargetPusher:    "local",
		Target:          targetdomain.Placement{Cluster: "cluster-a", Namespace: "payments"},
		AssetVersionIDs: map[string]string{"backend": "asset-v1"},
		Assets: []taskdomain.AbstractAsset{{
			Type:    "Compute",
			Name:    "backend",
			Payload: map[string]string{"ENV": "prod"},
		}},
	}

	r.executeTask(leakedCtx, task)

	span := findEndedSpanByName(t, recorder.Ended(), "guardian.pusher.task.execute")
	if got := span.SpanContext().TraceID(); got == leakedTraceID {
		t.Fatalf("task span trace ID = %s, want a new root trace instead of the leaked parent", got)
	}
}
