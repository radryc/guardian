package ui

import (
	"context"
	"strings"
	"time"

	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
)

type FlowStateEvaluator interface {
	Evaluate(ctx context.Context, input FlowAssetContext) FlowObservation
}

type FlowAssetContext struct {
	Partition      string
	Intent         string
	Asset          string
	IntentState    *statedomain.IntentState
	Runtime        intentTaskRuntime
	MetricGroups   []string
	ObservedHealth *taskdomain.HealthObservation
	ApplyReadiness *taskdomain.ApplyReadiness
	Now            time.Time
}

type FlowObservation struct {
	State     string
	Summary   string
	Source    string
	UpdatedAt time.Time
}

func defaultFlowStateEvaluator() FlowStateEvaluator {
	return guardianObservationFlowEvaluator{}
}

type guardianObservationFlowEvaluator struct{}

func (guardianObservationFlowEvaluator) Evaluate(_ context.Context, input FlowAssetContext) FlowObservation {
	if len(input.MetricGroups) == 0 {
		return FlowObservation{}
	}
	updatedAt := flowUpdatedAt(input)
	if input.Runtime.TimedOut {
		return FlowObservation{
			State:     "failing",
			Summary:   "Reconcile task timed out before flow telemetry was refreshed",
			Source:    "guardian-observation",
			UpdatedAt: updatedAt,
		}
	}
	if input.Runtime.Active {
		return FlowObservation{
			State:     "busy",
			Summary:   "Reconcile task is in progress",
			Source:    "guardian-observation",
			UpdatedAt: updatedAt,
		}
	}
	if input.ApplyReadiness != nil && input.ApplyReadiness.Status == taskdomain.ApplyReadinessBlocked {
		summary := strings.TrimSpace(input.ApplyReadiness.Summary)
		if summary == "" {
			summary = "Asset dependencies are not ready"
		}
		return FlowObservation{
			State:     "busy",
			Summary:   summary,
			Source:    "guardian-observation",
			UpdatedAt: updatedAt,
		}
	}
	if input.ObservedHealth != nil {
		summary := strings.TrimSpace(input.ObservedHealth.Summary)
		switch input.ObservedHealth.Status {
		case taskdomain.HealthHealthy:
			if summary == "" {
				summary = "Asset flow is within expected bounds"
			}
			return FlowObservation{
				State:     "healthy",
				Summary:   summary,
				Source:    "guardian-observation",
				UpdatedAt: updatedAt,
			}
		case taskdomain.HealthDegraded:
			if summary == "" {
				summary = "Asset is progressing with degraded health signals"
			}
			return FlowObservation{
				State:     "busy",
				Summary:   summary,
				Source:    "guardian-observation",
				UpdatedAt: updatedAt,
			}
		case taskdomain.HealthUnhealthy:
			if summary == "" {
				summary = "Asset health is unhealthy"
			}
			return FlowObservation{
				State:     "failing",
				Summary:   summary,
				Source:    "guardian-observation",
				UpdatedAt: updatedAt,
			}
		}
	}
	if input.IntentState != nil {
		switch input.IntentState.Status {
		case statedomain.StatusChecking, statedomain.StatusDiffing, statedomain.StatusApplying:
			return FlowObservation{
				State:     "busy",
				Summary:   "Intent reconcile is still running",
				Source:    "guardian-observation",
				UpdatedAt: updatedAt,
			}
		}
	}
	return FlowObservation{
		State:     "unknown",
		Summary:   "Awaiting flow telemetry",
		Source:    "guardian-observation",
		UpdatedAt: updatedAt,
	}
}

func flowUpdatedAt(input FlowAssetContext) time.Time {
	if input.IntentState == nil {
		if input.Now.IsZero() {
			return time.Now().UTC()
		}
		return input.Now.UTC()
	}
	for _, candidate := range []time.Time{
		input.IntentState.Timestamps.LastApplyAt,
		input.IntentState.Timestamps.LastDiffAt,
		input.IntentState.Timestamps.LastCheckAt,
		input.IntentState.Timestamps.LastQueuedAt,
	} {
		if !candidate.IsZero() {
			return candidate.UTC()
		}
	}
	if input.Now.IsZero() {
		return time.Now().UTC()
	}
	return input.Now.UTC()
}
