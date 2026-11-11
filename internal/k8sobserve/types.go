// Package k8sobserve collects the Kubernetes resources managed by a Guardian
// pusher and publishes a per-intent inventory into the shared store. The
// Guardian observed collector reads that inventory and turns it into
// per-intent resource observations for the topology.
package k8sobserve

import "time"

// Resource is one live Kubernetes object managed by Guardian. Sensitive
// payloads (Secret data, ConfigMap values) are never copied into the
// inventory.
type Resource struct {
	Partition      string            `json:"partition"`
	Intent         string            `json:"intent"`
	Asset          string            `json:"asset,omitempty"`
	AssetType      string            `json:"assetType,omitempty"`
	Cluster        string            `json:"cluster,omitempty"`
	Namespace      string            `json:"namespace,omitempty"`
	Kind           string            `json:"kind"`
	APIVersion     string            `json:"apiVersion,omitempty"`
	Name           string            `json:"name"`
	UID            string            `json:"uid,omitempty"`
	Status         string            `json:"status,omitempty"`
	Ready          string            `json:"ready,omitempty"`
	Replicas       int               `json:"replicas,omitempty"`
	Available      int               `json:"available,omitempty"`
	CreatedAt      time.Time         `json:"createdAt,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	ManagedAsset   string            `json:"managedAsset,omitempty"`
	ManagedBy      string            `json:"managedBy,omitempty"`
	OwnerReference string            `json:"ownerReference,omitempty"`
}

// Inventory is the managed resource set for one partition/intent.
type Inventory struct {
	APIVersion  string     `json:"apiVersion"`
	Kind        string     `json:"kind"`
	Cluster     string     `json:"cluster,omitempty"`
	Partition   string     `json:"partition"`
	Intent      string     `json:"intent"`
	GeneratedAt time.Time  `json:"generatedAt"`
	Resources   []Resource `json:"resources"`
}

// InventoryKind is the serialized schema kind.
const InventoryKind = "KubernetesResourceInventory"
