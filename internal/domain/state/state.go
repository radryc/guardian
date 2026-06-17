package state

import (
	"time"

	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
)

type IntentStatus string

const (
	StatusInvalid       IntentStatus = "Invalid"
	StatusBlocked       IntentStatus = "Blocked"
	StatusReady         IntentStatus = "Ready"
	StatusChecking      IntentStatus = "Checking"
	StatusCheckFailed   IntentStatus = "CheckFailed"
	StatusDiffing       IntentStatus = "Diffing"
	StatusDiffFailed    IntentStatus = "DiffFailed"
	StatusDrifted       IntentStatus = "Drifted"
	StatusDriftedLocked IntentStatus = "DriftedLocked"
	StatusApplying      IntentStatus = "Applying"
	StatusHealthy       IntentStatus = "Healthy"
	StatusApplyFailed   IntentStatus = "ApplyFailed"
	StatusDestroying    IntentStatus = "Destroying"
	StatusDestroyed     IntentStatus = "Destroyed"
	StatusOrphaned      IntentStatus = "Orphaned"
)

type PartitionState struct {
	APIVersion        string                 `json:"apiVersion"`
	Kind              string                 `json:"kind"`
	Partition         string                 `json:"partition"`
	Status            string                 `json:"status"`
	DisplayStatus     string                 `json:"displayStatus,omitempty"`
	Summary           string                 `json:"summary,omitempty"`
	ConfigVersionID   string                 `json:"configVersionID"`
	PartitionRevision string                 `json:"partitionRevision"`
	IntentVersions    map[string]string      `json:"intentVersions"`
	LastCompiledAt    time.Time              `json:"lastCompiledAt"`
	LastReconciledAt  time.Time              `json:"lastReconciledAt"`
	Errors            []string               `json:"errors"`
	Metrics           PartitionStatusMetrics `json:"metrics"`
}

type PartitionRuntime struct {
	APIVersion     string                  `json:"apiVersion"`
	Kind           string                  `json:"kind"`
	Partition      string                  `json:"partition"`
	UpdatedAt      time.Time               `json:"updatedAt"`
	PartitionState *PartitionState         `json:"partitionState,omitempty"`
	Intents        map[string]*IntentState `json:"intents"`
}

type IntentState struct {
	APIVersion           string                                  `json:"apiVersion"`
	Kind                 string                                  `json:"kind"`
	Partition            string                                  `json:"partition"`
	Intent               string                                  `json:"intent"`
	Status               IntentStatus                            `json:"status"`
	Locked               bool                                    `json:"locked"`
	IntentVersionID      string                                  `json:"intentVersionID"`
	IntentSpecHash       string                                  `json:"intentSpecHash"`
	LastAppliedSpecHash  string                                  `json:"lastAppliedSpecHash,omitempty"`
	PartitionRevision    string                                  `json:"partitionRevision"`
	DeploymentRevision   string                                  `json:"deploymentRevision"`
	TargetPusher       string                                  `json:"targetPusher"`
	Target             targetdomain.Placement                  `json:"target,omitempty"`
	Joins              []string                                `json:"joins"`
	AssetVersionIDs    map[string]string                       `json:"assetVersionIDs"`
	AssetVersions      map[string]string                       `json:"assetVersions,omitempty"`
	Outputs            map[string]string                       `json:"outputs"`
	Drift              *taskdomain.DriftReport                 `json:"drift,omitempty"`
	Health             *taskdomain.HealthObservation           `json:"health,omitempty"`
	ApplyReadiness     *taskdomain.ApplyReadiness              `json:"applyReadiness,omitempty"`
	AssetObservations  map[string]*taskdomain.AssetObservation `json:"assetObservations,omitempty"`
	LastTaskID         string                                  `json:"lastTaskID"`
	LastError          *string                                 `json:"lastError,omitempty"`
	PartitionMode      string                                  `json:"partitionMode,omitempty"`
	Timestamps         StateTimestamps                         `json:"timestamps"`
}

type StateTimestamps struct {
	LastQueuedAt time.Time `json:"lastQueuedAt"`
	LastCheckAt  time.Time `json:"lastCheckAt"`
	LastDiffAt   time.Time `json:"lastDiffAt"`
	LastApplyAt  time.Time `json:"lastApplyAt"`
}

func NewPartitionRuntime(partition string) *PartitionRuntime {
	return &PartitionRuntime{
		APIVersion: "guardian/v1alpha1",
		Kind:       "PartitionRuntime",
		Partition:  partition,
		Intents:    map[string]*IntentState{},
	}
}

func ClonePartitionState(in *PartitionState) *PartitionState {
	if in == nil {
		return nil
	}
	out := *in
	out.IntentVersions = cloneStringMap(in.IntentVersions)
	out.Errors = append([]string(nil), in.Errors...)
	out.Metrics = ClonePartitionStatusMetrics(in.Metrics)
	return &out
}

func CloneIntentState(in *IntentState) *IntentState {
	if in == nil {
		return nil
	}
	out := *in
	out.Joins = append([]string(nil), in.Joins...)
	out.AssetVersionIDs = cloneStringMap(in.AssetVersionIDs)
	out.AssetVersions = cloneStringMap(in.AssetVersions)
	out.Outputs = cloneStringMap(in.Outputs)
	out.Drift = cloneDriftReport(in.Drift)
	out.Health = cloneHealthObservation(in.Health)
	out.ApplyReadiness = cloneApplyReadiness(in.ApplyReadiness)
	out.AssetObservations = cloneAssetObservationMap(in.AssetObservations)
	out.LastError = cloneStringPtr(in.LastError)
	return &out
}

func ClonePartitionRuntime(in *PartitionRuntime) *PartitionRuntime {
	if in == nil {
		return nil
	}
	out := *in
	out.PartitionState = ClonePartitionState(in.PartitionState)
	out.Intents = make(map[string]*IntentState, len(in.Intents))
	for name, state := range in.Intents {
		out.Intents[name] = CloneIntentState(state)
	}
	return &out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringPtr(in *string) *string {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func cloneDriftReport(in *taskdomain.DriftReport) *taskdomain.DriftReport {
	if in == nil {
		return nil
	}
	out := *in
	out.ChangedAssets = append([]string(nil), in.ChangedAssets...)
	return &out
}

func cloneHealthObservation(in *taskdomain.HealthObservation) *taskdomain.HealthObservation {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneApplyReadiness(in *taskdomain.ApplyReadiness) *taskdomain.ApplyReadiness {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneAssetObservationMap(in map[string]*taskdomain.AssetObservation) map[string]*taskdomain.AssetObservation {
	if in == nil {
		return nil
	}
	out := make(map[string]*taskdomain.AssetObservation, len(in))
	for key, value := range in {
		out[key] = cloneAssetObservation(value)
	}
	return out
}

func cloneAssetObservation(in *taskdomain.AssetObservation) *taskdomain.AssetObservation {
	if in == nil {
		return nil
	}
	out := *in
	out.Health = cloneHealthObservation(in.Health)
	out.ApplyReadiness = cloneApplyReadiness(in.ApplyReadiness)
	return &out
}
