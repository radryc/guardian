package intent

import (
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	partitiondomain "github.com/rydzu/ainfra/guardian/internal/domain/partition"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
)

type Metadata = partitiondomain.Metadata
type AssetSpec = assetdomain.Spec

type Intent struct {
	APIVersion string     `yaml:"apiVersion" json:"apiVersion"`
	Kind       string     `yaml:"kind" json:"kind"`
	Metadata   Metadata   `yaml:"metadata" json:"metadata"`
	Spec       IntentSpec `yaml:"spec" json:"spec"`
}

type IntentSpec struct {
	IntentType   string                 `yaml:"intentType" json:"intentType"`
	Joins        []string               `yaml:"joins,omitempty" json:"joins,omitempty"`
	TargetPusher string                 `yaml:"targetPusher" json:"targetPusher"`
	Target       targetdomain.Placement `yaml:"target,omitempty" json:"target,omitempty"`
	Locked       bool                   `yaml:"locked" json:"locked"`
	Hints        []assetdomain.Hint     `yaml:"hints,omitempty" json:"hints,omitempty"`
	Assets       []AssetSpec            `yaml:"assets" json:"assets"`
}
