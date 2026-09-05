package partition

import targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"

type Partition struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
}

type Metadata struct {
	Name   string            `yaml:"name" json:"name"`
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

type Spec struct {
	DeletionPolicy string             `yaml:"deletionPolicy" json:"deletionPolicy"`
	Reconciliation ReconciliationSpec `yaml:"reconciliation" json:"reconciliation"`
	Defaults       PartitionDefaults  `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Labels         map[string]string  `yaml:"labels,omitempty" json:"labels,omitempty"`
}

type ReconciliationSpec struct {
	Mode          string `yaml:"mode" json:"mode"`
	Interval      string `yaml:"interval" json:"interval"`
	JitterPercent int    `yaml:"jitterPercent,omitempty" json:"jitterPercent,omitempty"`
}

type PartitionDefaults struct {
	TargetPusher string                 `yaml:"targetPusher,omitempty" json:"targetPusher,omitempty"`
	Target       targetdomain.Placement `yaml:"target,omitempty" json:"target,omitempty"`
}
