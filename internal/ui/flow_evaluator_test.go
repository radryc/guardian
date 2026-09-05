package ui

import (
	"context"
	"testing"
	"time"

	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
)

func TestGuardianObservationFlowEvaluator(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	evaluator := defaultFlowStateEvaluator()

	tests := []struct {
		name    string
		input   FlowAssetContext
		want    string
		summary string
	}{
		{
			name: "active runtime is busy",
			input: FlowAssetContext{
				Runtime:      intentTaskRuntime{Active: true},
				MetricGroups: []string{"throughput"},
				Now:          now,
			},
			want:    "busy",
			summary: "Reconcile task is in progress",
		},
		{
			name: "unhealthy observation is failing",
			input: FlowAssetContext{
				MetricGroups: []string{"throughput"},
				ObservedHealth: &taskdomain.HealthObservation{
					Status:  taskdomain.HealthUnhealthy,
					Summary: "container crashloop",
				},
				Now: now,
			},
			want:    "failing",
			summary: "container crashloop",
		},
		{
			name: "healthy observation is healthy",
			input: FlowAssetContext{
				MetricGroups: []string{"throughput"},
				ObservedHealth: &taskdomain.HealthObservation{
					Status: taskdomain.HealthHealthy,
				},
				Now: now,
			},
			want:    "healthy",
			summary: "Asset flow is within expected bounds",
		},
		{
			name: "checking status falls back to busy",
			input: FlowAssetContext{
				MetricGroups: []string{"throughput"},
				IntentState: &statedomain.IntentState{
					Status: statedomain.StatusChecking,
				},
				Now: now,
			},
			want:    "busy",
			summary: "Intent reconcile is still running",
		},
		{
			name: "missing signals remains unknown",
			input: FlowAssetContext{
				MetricGroups: []string{"throughput"},
				Now:          now,
			},
			want:    "unknown",
			summary: "Awaiting flow telemetry",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := evaluator.Evaluate(context.Background(), tc.input)
			if got.State != tc.want {
				t.Fatalf("state = %q, want %q", got.State, tc.want)
			}
			if got.Summary != tc.summary {
				t.Fatalf("summary = %q, want %q", got.Summary, tc.summary)
			}
			if got.Source != "guardian-observation" {
				t.Fatalf("source = %q, want guardian-observation", got.Source)
			}
			if got.UpdatedAt.IsZero() {
				t.Fatalf("expected non-zero updatedAt")
			}
		})
	}
}

func TestGuardianObservationFlowEvaluator_NoMetricGroups(t *testing.T) {
	t.Parallel()

	got := defaultFlowStateEvaluator().Evaluate(context.Background(), FlowAssetContext{})
	if got != (FlowObservation{}) {
		t.Fatalf("expected empty observation when metric groups are missing, got %+v", got)
	}
}
