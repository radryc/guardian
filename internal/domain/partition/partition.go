package partition

import targetdomain "github.com/radryc/guardian/internal/domain/target"

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
	// Sets names virtual sets that should contain this partition. Membership
	// is additive: existing manual memberships are preserved. Unknown sets are
	// created automatically using the reference as their display name.
	Sets []string `yaml:"sets,omitempty" json:"sets,omitempty"`
	// PrimarySet is the optional primary set for this partition. It is added to
	// Sets automatically when set.
	PrimarySet string `yaml:"primarySet,omitempty" json:"primarySet,omitempty"`
}

type ReconciliationSpec struct {
	Mode          string `yaml:"mode" json:"mode"`
	Interval      string `yaml:"interval" json:"interval"`
	JitterPercent int    `yaml:"jitterPercent,omitempty" json:"jitterPercent,omitempty"`
}

type PartitionDefaults struct {
	TargetPusher  string                 `yaml:"targetPusher,omitempty" json:"targetPusher,omitempty"`
	Target        targetdomain.Placement `yaml:"target,omitempty" json:"target,omitempty"`
	Observability ObservabilityDefaults  `yaml:"observability,omitempty" json:"observability,omitempty"`
	Probes        ProbeDefaults          `yaml:"probes,omitempty" json:"probes,omitempty"`
}

type ObservabilityDefaults struct {
	Enabled         *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	AutoInstrument  string `yaml:"autoInstrument,omitempty" json:"autoInstrument,omitempty"`
	ServiceName     string `yaml:"serviceName,omitempty" json:"serviceName,omitempty"`
	OTLPEndpoint    string `yaml:"otlpEndpoint,omitempty" json:"otlpEndpoint,omitempty"`
	CloudWatch      string `yaml:"cloudwatch,omitempty" json:"cloudwatch,omitempty"`
	Instrumentation string `yaml:"instrumentation,omitempty" json:"instrumentation,omitempty"`
}

type ProbeDefaults struct {
	Enabled       *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Interval      string   `yaml:"interval,omitempty" json:"interval,omitempty"`
	Timeout       string   `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retries       *int     `yaml:"retries,omitempty" json:"retries,omitempty"`
	HTTPPaths     []string `yaml:"httpPaths,omitempty" json:"httpPaths,omitempty"`
	TCPPorts      []int    `yaml:"tcpPorts,omitempty" json:"tcpPorts,omitempty"`
	MaxTargets    *int     `yaml:"maxTargets,omitempty" json:"maxTargets,omitempty"`
	AllowExternal *bool    `yaml:"allowExternal,omitempty" json:"allowExternal,omitempty"`
}
