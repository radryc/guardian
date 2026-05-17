package task

import (
	"time"

	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
)

type Operation string

const (
	OpCheck   Operation = "CHECK"
	OpDiff    Operation = "DIFF"
	OpApply   Operation = "APPLY"
	OpDestroy Operation = "DESTROY"
)

type Task struct {
	APIVersion        string                 `json:"apiVersion"`
	Kind              string                 `json:"kind"`
	TaskID            string                 `json:"taskID"`
	CorrelationID     string                 `json:"correlationID"`
	Partition         string                 `json:"partition"`
	Intent            string                 `json:"intent"`
	Op                Operation              `json:"op"`
	TargetPusher      string                 `json:"targetPusher"`
	Target            targetdomain.Placement `json:"target,omitempty"`
	PartitionRevision string                 `json:"partitionRevision"`
	IntentVersionID   string                 `json:"intentVersionID"`
	IntentSpecHash    string                 `json:"intentSpecHash"`
	AssetVersionIDs   map[string]string      `json:"assetVersionIDs"`
	AssetVersions     map[string]string      `json:"assetVersions,omitempty"`
	Assets            []AbstractAsset        `json:"assets"`
	CreatedAt         time.Time              `json:"createdAt"`
}

type AbstractAsset struct {
	Type       string            `json:"type"`
	Name       string            `json:"name"`
	DependsOn  []string          `json:"dependsOn,omitempty"`
	Payload    map[string]string `json:"payload,omitempty"`
	Properties map[string]any    `json:"properties,omitempty"`
}

type ClaimFile struct {
	TaskID       string    `json:"taskID"`
	WorkerID     string    `json:"workerID"`
	ClaimedAt    time.Time `json:"claimedAt"`
	LeaseSeconds int       `json:"leaseSeconds"`
}

type TaskResult struct {
	APIVersion        string                       `json:"apiVersion"`
	Kind              string                       `json:"kind"`
	TaskID            string                       `json:"taskID"`
	Op                Operation                    `json:"op"`
	Status            ResultStatus                 `json:"status"`
	Partition         string                       `json:"partition"`
	Intent            string                       `json:"intent"`
	Pusher            string                       `json:"pusher"`
	Outputs           map[string]string            `json:"outputs,omitempty"`
	Drift             *DriftReport                 `json:"drift,omitempty"`
	Health            *HealthObservation           `json:"health,omitempty"`
	ApplyReadiness    *ApplyReadiness              `json:"applyReadiness,omitempty"`
	AssetObservations map[string]*AssetObservation `json:"assetObservations,omitempty"`
	Error             *string                      `json:"error,omitempty"`
	Logs              []LogEntry                   `json:"logs,omitempty"`
	FinishedAt        time.Time                    `json:"finishedAt"`
}

type ResultStatus string

const (
	ResultSucceeded ResultStatus = "Succeeded"
	ResultFailed    ResultStatus = "Failed"
)

type DriftReport struct {
	Status        string   `json:"status"`
	Summary       string   `json:"summary"`
	ChangedAssets []string `json:"changedAssets"`
}

type HealthStatus string

const (
	HealthUnknown   HealthStatus = "Unknown"
	HealthHealthy   HealthStatus = "Healthy"
	HealthDegraded  HealthStatus = "Degraded"
	HealthUnhealthy HealthStatus = "Unhealthy"
)

type HealthObservation struct {
	Status  HealthStatus `json:"status"`
	Summary string       `json:"summary,omitempty"`
}

type ApplyReadinessStatus string

const (
	ApplyReadinessUnknown ApplyReadinessStatus = "Unknown"
	ApplyReadinessReady   ApplyReadinessStatus = "Ready"
	ApplyReadinessBlocked ApplyReadinessStatus = "Blocked"
)

type ApplyReadiness struct {
	Status  ApplyReadinessStatus `json:"status"`
	Summary string               `json:"summary,omitempty"`
}

type AssetObservation struct {
	Health         *HealthObservation `json:"health,omitempty"`
	ApplyReadiness *ApplyReadiness    `json:"applyReadiness,omitempty"`
}

type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Asset     string    `json:"asset,omitempty"`
	Message   string    `json:"message"`
}
