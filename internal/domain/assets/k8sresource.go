package assets

import (
	"fmt"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

// K8sResourceSpec describes a raw Kubernetes manifest applied verbatim. The
// manifest is supplied through the asset's `k8s` payload and may contain
// multiple YAML documents (e.g. ServiceAccount + ClusterRole + RoleBinding).
type K8sResourceSpec struct {
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

type k8sResourceDefinition struct{}

func init() {
	Register(k8sResourceDefinition{})
}

func (k8sResourceDefinition) Type() string { return assetdomain.TypeK8sResource }

func (k8sResourceDefinition) NewSpec() any { return &K8sResourceSpec{} }

func (k8sResourceDefinition) Validate(spec any, _ ValidationContext) error {
	_, ok := spec.(*K8sResourceSpec)
	if !ok {
		return fmt.Errorf("internal k8s resource spec type mismatch")
	}
	return nil
}
