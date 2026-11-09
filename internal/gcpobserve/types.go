// Package gcpobserve maintains a read-only view of GCP resources managed by the
// Guardian GCP pusher. It combines OpenTofu state objects in GCS with Cloud
// Asset Inventory, and never writes to MonoFS or Guardian.
package gcpobserve

import "time"

// StackResource is one Cloud Asset Inventory resource.
type StackResource struct {
	Name                   string    `json:"name"`
	AssetType              string    `json:"assetType"`
	Location               string    `json:"location,omitempty"`
	DisplayName            string    `json:"displayName,omitempty"`
	State                  string    `json:"state,omitempty"`
	ParentFullResourceName string    `json:"parentFullResourceName,omitempty"`
	CreateTime             time.Time `json:"createTime,omitempty"`
	UpdateTime             time.Time `json:"updateTime,omitempty"`
}

// StackState is the observed state of one partition/intent.
type StackState struct {
	Project   string          `json:"project"`
	Region    string          `json:"region,omitempty"`
	StackName string          `json:"stackName"`
	Partition string          `json:"partition"`
	Intent    string          `json:"intent"`
	Status    string          `json:"status,omitempty"`
	StatePath string          `json:"statePath,omitempty"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Resources []StackResource `json:"resources,omitempty"`
	Source    string          `json:"source,omitempty"`
	// ResourcesComplete is false when Cloud Asset Inventory could not be
	// enumerated; the previous inventory is retained instead of publishing an
	// empty one. Never serialized.
	ResourcesComplete bool `json:"-"`
	// DriftStatus is left Unknown: authoritative drift is the task engine's
	// `tofu plan -detailed-exitcode`, not an observation cache.
	DriftStatus string `json:"driftStatus,omitempty"`
}

// Event is a normalized change event. v1 derives events from Cloud Logging
// audit entries in a future iteration; the field exists for API stability.
type Event struct {
	ID        string    `json:"id"`
	Project   string    `json:"project"`
	StackName string    `json:"stackName"`
	Status    string    `json:"status,omitempty"`
	Message   string    `json:"message,omitempty"`
	Time      time.Time `json:"time"`
}
