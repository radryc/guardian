package assets

import (
	"fmt"
	"regexp"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

// cfnTypePattern matches CloudFormation resource type names, for example
// AWS::SQS::Queue or AWS::Logs::LogGroup.
var cfnTypePattern = regexp.MustCompile(`^AWS::[A-Za-z0-9]+::[A-Za-z0-9]+$`)

// AWSResourceSpec describes an AWS resource rendered by the fallback
// `aws.fallback.cfn-resource` template: a raw CloudFormation type plus its
// properties, for resource types that have no curated Guardian template yet.
type AWSResourceSpec struct {
	CfnType string `json:"cfnType" yaml:"cfnType"`
	// LogicalID is the CloudFormation logical ID when this asset was derived
	// from a stack template. It is metadata for observed resource assets.
	LogicalID string `json:"logicalId,omitempty" yaml:"logicalId,omitempty"`
	// Condition is the CloudFormation condition guarding the resource.
	Condition string `json:"condition,omitempty" yaml:"condition,omitempty"`
	// Properties are passed through to the CloudFormation resource.
	Properties map[string]any `json:"properties,omitempty" yaml:"properties,omitempty"`
	// Outputs are literal output values exported under assetName.key.
	Outputs map[string]string `json:"outputs,omitempty" yaml:"outputs,omitempty"`
	// Existing marks resources discovered by a scan: they are observed-only
	// until CDK import adoption lands, and are never created by APPLY.
	Existing bool `json:"existing,omitempty" yaml:"existing,omitempty"`
}

type awsResourceDefinition struct{}

func init() {
	Register(awsResourceDefinition{})
}

func (awsResourceDefinition) Type() string { return assetdomain.TypeAWSResource }

func (awsResourceDefinition) NewSpec() any { return &AWSResourceSpec{} }

func (awsResourceDefinition) Validate(spec any, _ ValidationContext) error {
	typed, ok := spec.(*AWSResourceSpec)
	if !ok {
		return fmt.Errorf("internal AWSResource spec type mismatch")
	}
	if err := requireString(typed.CfnType, "cfnType"); err != nil {
		return err
	}
	if !cfnTypePattern.MatchString(strings.TrimSpace(typed.CfnType)) {
		return fmt.Errorf("property cfnType must look like AWS::<Service>::<Type>, got %q", typed.CfnType)
	}
	for key := range typed.Outputs {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("property outputs must not contain empty keys")
		}
	}
	return nil
}
