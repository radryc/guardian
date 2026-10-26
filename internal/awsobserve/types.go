// Package awsobserve consumes CloudFormation observation events from an SQS
// queue (fed by EventBridge) and maintains a read-only cache of the latest
// observed stack state. The pusher serves that cache over its read API; it
// never writes to MonoFS and never dials Guardian.
package awsobserve

import "time"

// DriftedResource is one CloudFormation resource reported as drifted.
type DriftedResource struct {
	LogicalID    string `json:"logicalId"`
	ResourceType string `json:"resourceType,omitempty"`
	Status       string `json:"status,omitempty"`
}

// StackState is the latest observed state for one CloudFormation stack.
type StackState struct {
	Account              string            `json:"account"`
	Region               string            `json:"region"`
	StackID              string            `json:"stackId"`
	StackName            string            `json:"stackName"`
	StackStatus          string            `json:"stackStatus,omitempty"`
	StatusReason         string            `json:"statusReason,omitempty"`
	DriftStatus          string            `json:"driftStatus,omitempty"`
	DriftedResourceCount int               `json:"driftedResourceCount,omitempty"`
	DriftedResources     []DriftedResource `json:"driftedResources,omitempty"`
	LastEventType        string            `json:"lastEventType,omitempty"`
	LastEventID          string            `json:"lastEventId,omitempty"`
	LastEventTime        time.Time         `json:"lastEventTime,omitempty"`
	UpdatedAt            time.Time         `json:"updatedAt"`
	Source               string            `json:"source,omitempty"`
	// Tags and Outputs are the CloudFormation stack metadata used for
	// onboarding discovery and probe target resolution.
	Tags    map[string]string `json:"tags,omitempty"`
	Outputs map[string]string `json:"outputs,omitempty"`
}

// Event is a normalized CloudFormation observation event.
type Event struct {
	ID                   string    `json:"id"`
	Account              string    `json:"account"`
	Region               string    `json:"region"`
	StackID              string    `json:"stackId"`
	StackName            string    `json:"stackName"`
	DetailType           string    `json:"detailType"`
	Status               string    `json:"status,omitempty"`
	StatusReason         string    `json:"statusReason,omitempty"`
	DriftStatus          string    `json:"driftStatus,omitempty"`
	DetectionStatus      string    `json:"detectionStatus,omitempty"`
	DriftedResourceCount int       `json:"driftedResourceCount,omitempty"`
	Time                 time.Time `json:"time"`
}

const (
	sourceEventBridge = "eventbridge"
	sourceRefresh     = "refresh"
)
