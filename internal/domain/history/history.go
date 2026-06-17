package history

import (
	"time"

	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
)

type DeploymentRecord struct {
	APIVersion         string                 `json:"apiVersion"`
	Kind               string                 `json:"kind"`
	DeploymentRevision string                 `json:"deploymentRevision"`
	Partition          string                 `json:"partition"`
	Intent             string                 `json:"intent"`
	Target             targetdomain.Placement `json:"target,omitempty"`
	PartitionRevision  string                 `json:"partitionRevision"`
	IntentVersionID    string                 `json:"intentVersionID"`
	AssetVersionIDs    map[string]string      `json:"assetVersionIDs"`
	AssetVersions      map[string]string      `json:"assetVersions,omitempty"`
	TaskIDs            []string               `json:"taskIDs"`
	ChangedAssets      []string               `json:"changedAssets,omitempty"`
	SelfHealing        bool                   `json:"selfHealing,omitempty"`
	Outputs            map[string]string      `json:"outputs"`
	CreatedAt          time.Time              `json:"createdAt"`
}
