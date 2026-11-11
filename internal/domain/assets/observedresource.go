package assets

import (
	"fmt"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
)

// ObservedResourceSpec describes a read-only resource discovered from a
// provider inventory (GCP Cloud Asset Inventory or Kubernetes). It is not
// authored by users and is never applied by a pusher.
type ObservedResourceSpec struct {
	// Provider is "gcp" or "kubernetes".
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
	// ResourceType is the provider-native type, for example
	// run.googleapis.com/Service or Deployment.
	ResourceType string `json:"resourceType,omitempty" yaml:"resourceType,omitempty"`
	// ResourceName is the provider-native identifier (full resource name,
	// namespace/name, or UID).
	ResourceName string `json:"resourceName,omitempty" yaml:"resourceName,omitempty"`
	// Namespace is set for Kubernetes resources.
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	// Location is the cloud region/zone for GCP resources.
	Location string `json:"location,omitempty" yaml:"location,omitempty"`
	// State is the live provider state (ACTIVE, Available, Bound, ...).
	State string `json:"state,omitempty" yaml:"state,omitempty"`
	// ManagedAsset is the authored Guardian asset that owns this resource.
	ManagedAsset string `json:"managedAsset,omitempty" yaml:"managedAsset,omitempty"`
	// LatestRevision is set on a service node to show the current deployment
	// artifact without rendering every revision.
	LatestRevision string `json:"latestRevision,omitempty" yaml:"latestRevision,omitempty"`
}

type observedResourceDefinition struct{}

func init() {
	Register(observedResourceDefinition{})
}

func (observedResourceDefinition) Type() string { return assetdomain.TypeObservedResource }

func (observedResourceDefinition) NewSpec() any { return &ObservedResourceSpec{} }

func (observedResourceDefinition) Validate(spec any, _ ValidationContext) error {
	typed, ok := spec.(*ObservedResourceSpec)
	if !ok {
		return fmt.Errorf("internal observed resource spec type mismatch")
	}
	if typed.Provider == "" {
		return fmt.Errorf("property provider is required")
	}
	return nil
}
