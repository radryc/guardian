package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
)

func TestMonofsMetricsFlowEvaluatorBusyOnDelta(t *testing.T) {
	t.Parallel()

	var writes int64 = 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "# TYPE monofs_router_guardian_flow_writes_total counter\n")
		fmt.Fprintf(w, "monofs_router_guardian_flow_writes_total{partition=\"genomics\",intent=\"workers\",path_kind=\"intent_manifest\"} %d\n", atomic.LoadInt64(&writes))
		fmt.Fprintf(w, "monofs_router_guardian_flow_write_bytes_total{partition=\"genomics\",intent=\"workers\",path_kind=\"intent_manifest\"} 100\n")
		fmt.Fprintf(w, "monofs_router_guardian_flow_deletes_total{partition=\"genomics\",intent=\"workers\",path_kind=\"intent_manifest\"} 0\n")
		fmt.Fprintf(w, "monofs_router_guardian_flow_delete_bytes_total{partition=\"genomics\",intent=\"workers\",path_kind=\"intent_manifest\"} 0\n")
	}))
	defer server.Close()

	evaluator := newMonofsMetricsFlowEvaluator(server.URL, 500*time.Millisecond, 0, defaultFlowStateEvaluator())
	ctx := context.Background()
	input := FlowAssetContext{
		Partition:      "genomics",
		Intent:         "workers",
		Asset:          "api",
		MetricGroups:   []string{"ingest"},
		ObservedHealth: &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "running"},
		Now:            time.Now().UTC(),
	}

	first := evaluator.Evaluate(ctx, input)
	if first.State != "healthy" {
		t.Fatalf("first state = %q, want healthy", first.State)
	}
	atomic.StoreInt64(&writes, 2)
	second := evaluator.Evaluate(ctx, input)
	if second.State != "busy" {
		t.Fatalf("second state = %q, want busy", second.State)
	}
	if second.Source != "monofs-metrics" {
		t.Fatalf("second source = %q, want monofs-metrics", second.Source)
	}
}

func TestMonofsMetricsFlowEvaluatorFallbackOnEndpointFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	evaluator := newMonofsMetricsFlowEvaluator(server.URL, 500*time.Millisecond, 0, defaultFlowStateEvaluator())
	input := FlowAssetContext{
		Partition:      "genomics",
		Intent:         "workers",
		Asset:          "api",
		MetricGroups:   []string{"ingest"},
		ObservedHealth: &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "running"},
		Now:            time.Now().UTC(),
	}

	got := evaluator.Evaluate(context.Background(), input)
	if got.State != "healthy" {
		t.Fatalf("state = %q, want healthy", got.State)
	}
	if !strings.Contains(got.Summary, "Flow metrics unavailable") {
		t.Fatalf("summary = %q, want metrics unavailable note", got.Summary)
	}
}

func TestParseMonofsFlowMetricLine(t *testing.T) {
	t.Parallel()

	line := "monofs_router_guardian_flow_deletes_total{partition=\"doctor\",intent=\"none\",path_kind=\"catalog\"} 4"
	series, value, ok := parseMonofsFlowMetricLine(line)
	if !ok {
		t.Fatalf("expected parse success")
	}
	if series.partition != "doctor" || series.intent != "none" || series.pathKind != "catalog" {
		t.Fatalf("series = %+v", series)
	}
	if value != 4 {
		t.Fatalf("value = %v, want 4", value)
	}
}
