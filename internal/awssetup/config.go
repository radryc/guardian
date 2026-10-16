package awssetup

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Profile            string
	Region             string
	RoleName           string
	BootstrapStackName string
	// PolicyMode selects how the deploy role is authorized: "scoped" puts the
	// built-in least-privilege inline policy on the role, "managed" attaches
	// the managed policy named by PolicyName.
	PolicyMode string
	// PolicyName is the managed policy attached when PolicyMode is "managed"
	// (for example AdministratorAccess).
	PolicyName string
	DryRun     bool
	// TrustPrincipals are additional principals allowed to assume the deploy
	// role (for example the ARN of a pusher task role in a tools account).
	TrustPrincipals []string
	// ExecutionPolicyARN is passed to `cdk bootstrap
	// --cloudformation-execution-policies` so the CloudFormation execution
	// role is scoped (empty keeps the CDK default).
	ExecutionPolicyARN string
	// ForceBootstrap re-runs `cdk bootstrap --force` even when the bootstrap
	// stack already exists (used to apply a new execution policy).
	ForceBootstrap bool
}

func (c Config) Validate() error {
	if c.Region == "" {
		return fmt.Errorf("region is required")
	}
	if c.RoleName == "" {
		return fmt.Errorf("role name is required")
	}
	if c.BootstrapStackName == "" {
		return fmt.Errorf("bootstrap stack name is required")
	}
	return nil
}

func (c Config) WithDefaults() Config {
	if c.Profile == "" {
		c.Profile = os.Getenv("AWS_PROFILE")
	}
	if c.Region == "" {
		if r := os.Getenv("AWS_REGION"); r != "" {
			c.Region = r
		} else if r := os.Getenv("AWS_DEFAULT_REGION"); r != "" {
			c.Region = r
		} else {
			c.Region = "us-east-1"
		}
	}
	if c.RoleName == "" {
		c.RoleName = "GuardianCdkDeployRole"
	}
	if c.BootstrapStackName == "" {
		c.BootstrapStackName = "CDKToolkit"
	}
	if strings.TrimSpace(c.PolicyMode) == "" {
		c.PolicyMode = PolicyScoped
	}
	if c.PolicyMode == PolicyAdmin {
		c.PolicyMode = "managed"
		if c.PolicyName == "" {
			c.PolicyName = "AdministratorAccess"
		}
	}
	if c.PolicyMode == PolicyScoped {
		c.PolicyName = ""
	}
	return c
}

// ResolvePolicyMode maps the --policy flag value to a mode + managed name.
//
//	scoped (default)          -> least-privilege inline policy
//	admin                     -> AdministratorAccess
//	<other>                   -> that managed policy
func ResolvePolicyMode(value string) (mode, name string) {
	switch strings.TrimSpace(value) {
	case "", PolicyScoped:
		return PolicyScoped, ""
	case PolicyAdmin:
		return "managed", "AdministratorAccess"
	default:
		return "managed", strings.TrimSpace(value)
	}
}

// ResolveExecutionPolicy maps the --execution-policy flag value to the policy
// argument passed to `cdk bootstrap --cloudformation-execution-policies`:
//
//	scoped (default)  -> GuardianCfnExecutionPolicy (created/updated)
//	admin             -> AdministratorAccess
//	<arn|name>        -> used as-is
func ResolveExecutionPolicy(value string) string {
	switch strings.TrimSpace(value) {
	case "", PolicyScoped:
		return PolicyScoped
	case PolicyAdmin:
		return "arn:aws:iam::aws:policy/AdministratorAccess"
	default:
		return strings.TrimSpace(value)
	}
}
