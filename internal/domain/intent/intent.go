package intent

import (
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	partitiondomain "github.com/rydzu/ainfra/guardian/internal/domain/partition"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
)

type Metadata = partitiondomain.Metadata
type AssetSpec = assetdomain.Spec

const (
	// TypeStandard is the default intent type: DIFF -> CHECK -> APPLY.
	TypeStandard = "standard"
	// TypeObserved is an AWS-only, report-only intent type. Guardian observes
	// the live state and reports drift but never queues APPLY.
	TypeObserved = "observed"
)

const (
	// ImplementationNative applies each asset with the provider-native driver
	// (imperative AWS SDK calls for the AWS pusher). It is the default.
	ImplementationNative = "native"
	// ImplementationCDK compiles the intent's CDK-expressible assets into one
	// generated TypeScript CDK stack that is deployed as a single
	// CloudFormation stack. Assets without a construct (Secret, Config, ...)
	// keep using their native driver.
	ImplementationCDK = "cdk"
)

type Intent struct {
	APIVersion string     `yaml:"apiVersion" json:"apiVersion"`
	Kind       string     `yaml:"kind" json:"kind"`
	Metadata   Metadata   `yaml:"metadata" json:"metadata"`
	Spec       IntentSpec `yaml:"spec" json:"spec"`
}

type IntentSpec struct {
	IntentType     string                 `yaml:"intentType" json:"intentType"`
	Implementation string                 `yaml:"implementation,omitempty" json:"implementation,omitempty"`
	Joins          []string               `yaml:"joins,omitempty" json:"joins,omitempty"`
	TargetPusher   string                 `yaml:"targetPusher" json:"targetPusher"`
	Target         targetdomain.Placement `yaml:"target,omitempty" json:"target,omitempty"`
	Locked         bool                   `yaml:"locked" json:"locked"`
	Hints          []assetdomain.Hint     `yaml:"hints,omitempty" json:"hints,omitempty"`
	Assets         []AssetSpec            `yaml:"assets" json:"assets"`
}
