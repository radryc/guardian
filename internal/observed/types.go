// Package observed implements the Guardian-side observed collector for
// report-only AWS intents. It polls the AWS pusher's read API (Guardian
// initiates the connection; the pusher never dials MonoFS or Guardian),
// compares the observed CloudFormation state with the desired CDK app stored in
// MonoFS and records the result under /partitions/<p>/.observed/.
package observed

import (
	"time"

	statedomain "github.com/radryc/guardian/internal/domain/state"
)

// StackPayload mirrors the AWS CDKStack asset payload manifest fields the
// collector needs. It is a read-only subset of the pusher's stackPayload.
type StackPayload struct {
	SourceType          string            `yaml:"sourceType"`
	SourceDir           string            `yaml:"sourceDir"`
	PrebuiltAssemblyDir string            `yaml:"prebuiltAssemblyDir"`
	AppCommand          string            `yaml:"appCommand,omitempty"`
	Entrypoint          string            `yaml:"entrypoint"`
	StackName           string            `yaml:"stackName"`
	StackID             string            `yaml:"stackID"`
	PackageManager      string            `yaml:"packageManager"`
	Context             map[string]string `yaml:"context,omitempty"`
}

// Snapshot is the captured observed state for one asset, persisted under
// /partitions/<p>/.observed/<intent>/<asset>.json.
type Snapshot struct {
	APIVersion           string    `json:"apiVersion"`
	Kind                 string    `json:"kind"`
	Partition            string    `json:"partition"`
	Intent               string    `json:"intent"`
	Asset                string    `json:"asset"`
	Account              string    `json:"account"`
	Region               string    `json:"region"`
	StackName            string    `json:"stackName"`
	StackID              string    `json:"stackId,omitempty"`
	StackStatus          string    `json:"stackStatus,omitempty"`
	StatusReason         string    `json:"statusReason,omitempty"`
	DriftStatus          string    `json:"driftStatus,omitempty"`
	DriftedResourceCount int       `json:"driftedResourceCount,omitempty"`
	LastEventType        string    `json:"lastEventType,omitempty"`
	LastEventID          string    `json:"lastEventId,omitempty"`
	LastEventTime        time.Time `json:"lastEventTime,omitempty"`
	TemplateHash         string    `json:"templateHash,omitempty"`
	ChangedResources     []string  `json:"changedResources,omitempty"`
	// Resources is the desired resource inventory with per-resource status.
	// It is computed from the desired stack template on every collection so
	// it can never drift from the authored CDK app.
	Resources  []statedomain.ResourceObservation `json:"resources,omitempty"`
	Error      string                            `json:"error,omitempty"`
	CapturedAt time.Time                         `json:"capturedAt"`
}
