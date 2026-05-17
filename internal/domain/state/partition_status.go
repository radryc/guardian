package state

import (
	"fmt"
	"sort"
	"strings"

	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
)

type PartitionStatusMetrics struct {
	TotalIntents       int            `json:"totalIntents"`
	HealthyIntents     int            `json:"healthyIntents"`
	PendingIntents     int            `json:"pendingIntents"`
	AttentionIntents   int            `json:"attentionIntents"`
	FailingIntents     int            `json:"failingIntents"`
	IntentStatusCounts map[string]int `json:"intentStatusCounts,omitempty"`
}

var knownPartitionStatuses = []string{"Compiled", "Healthy", "Progressing", "Attention", "Failing", "Invalid"}

var knownIntentStatuses = []string{
	string(StatusInvalid),
	string(StatusBlocked),
	string(StatusReady),
	string(StatusChecking),
	string(StatusCheckFailed),
	string(StatusDiffing),
	string(StatusDiffFailed),
	string(StatusDrifted),
	string(StatusDriftedLocked),
	string(StatusApplying),
	string(StatusHealthy),
	string(StatusApplyFailed),
	string(StatusDestroying),
	string(StatusDestroyed),
	string(StatusOrphaned),
}

func KnownPartitionStatuses() []string {
	return append([]string(nil), knownPartitionStatuses...)
}

func KnownIntentStatuses() []string {
	return append([]string(nil), knownIntentStatuses...)
}

func ClonePartitionStatusMetrics(in PartitionStatusMetrics) PartitionStatusMetrics {
	out := in
	out.IntentStatusCounts = cloneIntMap(in.IntentStatusCounts)
	return out
}

func NormalizePartitionRuntime(runtime *PartitionRuntime) *PartitionRuntime {
	if runtime == nil {
		return nil
	}
	if runtime.APIVersion == "" {
		runtime.APIVersion = "guardian/v1alpha1"
	}
	if runtime.Kind == "" {
		runtime.Kind = "PartitionRuntime"
	}
	if runtime.Intents == nil {
		runtime.Intents = map[string]*IntentState{}
	}
	for name, state := range runtime.Intents {
		if state == nil {
			continue
		}
		if state.APIVersion == "" {
			state.APIVersion = "guardian/v1alpha1"
		}
		if state.Kind == "" {
			state.Kind = "IntentState"
		}
		if state.Partition == "" {
			state.Partition = runtime.Partition
		}
		if state.Intent == "" {
			state.Intent = name
		}
	}
	runtime.PartitionState = DerivePartitionState(runtime.PartitionState, runtime.Partition, runtime.Intents)
	return runtime
}

func DerivePartitionState(base *PartitionState, partition string, intents map[string]*IntentState) *PartitionState {
	if base == nil && len(intents) == 0 {
		return nil
	}
	state := ClonePartitionState(base)
	if state == nil {
		state = &PartitionState{}
	}
	if state.APIVersion == "" {
		state.APIVersion = "guardian/v1alpha1"
	}
	if state.Kind == "" {
		state.Kind = "PartitionState"
	}
	if state.Partition == "" {
		state.Partition = partition
	}
	metrics := derivePartitionStatusMetrics(state, intents)
	state.Metrics = metrics
	state.Status, state.DisplayStatus, state.Summary = derivePartitionPresentation(state, metrics)
	return state
}

func derivePartitionStatusMetrics(base *PartitionState, intents map[string]*IntentState) PartitionStatusMetrics {
	metrics := PartitionStatusMetrics{IntentStatusCounts: map[string]int{}}
	for _, name := range selectedPartitionIntentNames(base, intents) {
		state := intents[name]
		if state == nil {
			continue
		}
		metrics.TotalIntents++
		status := state.Status
		if status == "" {
			status = StatusReady
		}
		metrics.IntentStatusCounts[string(status)]++
		switch intentHealthBucket(state) {
		case "healthy":
			metrics.HealthyIntents++
		case "pending":
			metrics.PendingIntents++
		case "attention":
			metrics.AttentionIntents++
		case "failing":
			metrics.FailingIntents++
		}
	}
	if len(metrics.IntentStatusCounts) == 0 {
		metrics.IntentStatusCounts = nil
	}
	return metrics
}

func derivePartitionPresentation(base *PartitionState, metrics PartitionStatusMetrics) (string, string, string) {
	if base != nil && len(base.Errors) > 0 {
		return "Invalid", "Invalid", fmt.Sprintf("%d partition error(s) require attention", len(base.Errors))
	}
	if metrics.TotalIntents == 0 {
		status := strings.TrimSpace(baseStatus(base))
		if status == "" {
			status = "Compiled"
		}
		return status, partitionDisplayStatus(status), emptyPartitionSummary(status)
	}
	status := "Healthy"
	switch {
	case metrics.FailingIntents > 0:
		status = "Failing"
	case metrics.AttentionIntents > 0:
		status = "Attention"
	case metrics.PendingIntents > 0:
		status = "Progressing"
	}
	return status, partitionDisplayStatus(status), formatPartitionMetricsSummary(metrics)
}

func selectedPartitionIntentNames(base *PartitionState, intents map[string]*IntentState) []string {
	names := make([]string, 0, len(intents))
	if base != nil && len(base.IntentVersions) > 0 {
		for name := range base.IntentVersions {
			if intents[name] != nil {
				names = append(names, name)
			}
		}
	} else {
		for name, state := range intents {
			if state != nil {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func intentHealthBucket(state *IntentState) string {
	if state == nil {
		return "pending"
	}
	if state.Status == StatusHealthy && state.Health != nil {
		switch state.Health.Status {
		case taskdomain.HealthUnhealthy:
			return "failing"
		case taskdomain.HealthDegraded:
			return "attention"
		}
	}
	switch state.Status {
	case StatusHealthy, StatusDestroyed:
		return "healthy"
	case StatusChecking, StatusDiffing, StatusApplying, StatusDestroying, StatusReady:
		return "pending"
	case StatusBlocked:
		if state.LastError != nil && strings.TrimSpace(*state.LastError) != "" {
			return "failing"
		}
		return "pending"
	case StatusInvalid, StatusCheckFailed, StatusDiffFailed, StatusApplyFailed:
		return "failing"
	case StatusDrifted, StatusDriftedLocked, StatusOrphaned:
		return "attention"
	default:
		return "pending"
	}
}

func partitionDisplayStatus(status string) string {
	switch status {
	case "Healthy":
		return "Stable"
	case "Progressing":
		return "Progressing"
	case "Attention":
		return "Attention"
	case "Failing":
		return "Needs action"
	case "Invalid":
		return "Invalid"
	default:
		return status
	}
}

func emptyPartitionSummary(status string) string {
	if status == "Compiled" {
		return "Configuration compiled; waiting for runtime state"
	}
	return partitionDisplayStatus(status)
}

func formatPartitionMetricsSummary(metrics PartitionStatusMetrics) string {
	parts := make([]string, 0, 4)
	if metrics.FailingIntents > 0 {
		parts = append(parts, fmt.Sprintf("%d failing", metrics.FailingIntents))
	}
	if metrics.AttentionIntents > 0 {
		parts = append(parts, fmt.Sprintf("%d attention", metrics.AttentionIntents))
	}
	if metrics.PendingIntents > 0 {
		parts = append(parts, fmt.Sprintf("%d progressing", metrics.PendingIntents))
	}
	if metrics.HealthyIntents > 0 {
		parts = append(parts, fmt.Sprintf("%d healthy", metrics.HealthyIntents))
	}
	if len(parts) == 0 {
		parts = append(parts, "0 healthy")
	}
	return fmt.Sprintf("%d intent(s): %s", metrics.TotalIntents, strings.Join(parts, ", "))
}

func baseStatus(state *PartitionState) string {
	if state == nil {
		return ""
	}
	return state.Status
}

func cloneIntMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
