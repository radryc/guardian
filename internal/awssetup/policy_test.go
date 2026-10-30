package awssetup

import (
	"encoding/json"
	"path"
	"strings"
	"testing"
)

func parsePolicy(t *testing.T, document string) map[string]any {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(document), &parsed); err != nil {
		t.Fatalf("policy is not valid JSON: %v", err)
	}
	if parsed["Version"] != "2012-10-17" {
		t.Fatalf("policy version = %v", parsed["Version"])
	}
	if _, ok := parsed["Statement"].([]any); !ok {
		t.Fatalf("policy has no statement list")
	}
	return parsed
}

func policyActions(t *testing.T, document string) []string {
	t.Helper()
	parsed := parsePolicy(t, document)
	statements, _ := parsed["Statement"].([]any)
	var actions []string
	for _, statement := range statements {
		entry, _ := statement.(map[string]any)
		switch typed := entry["Action"].(type) {
		case string:
			actions = append(actions, typed)
		case []any:
			for _, action := range typed {
				if text, ok := action.(string); ok {
					actions = append(actions, text)
				}
			}
		}
	}
	return actions
}

func hasAction(actions []string, want string) bool {
	for _, action := range actions {
		if action == want || action == "*" {
			return true
		}
		if strings.Contains(action, "*") {
			if ok, err := path.Match(action, want); err == nil && ok {
				return true
			}
		}
	}
	return false
}

func TestPoliciesFitIAMLimits(t *testing.T) {
	// Managed policies are capped at 6144 characters and role inline policies
	// at 10240; keep a margin so a future action addition fails the build
	// instead of the bootstrap.
	if size := len(ExecutionPolicy("111122223333", "us-east-1")); size > 6000 {
		t.Errorf("execution policy size = %d, want <= 6000 (IAM managed policy limit 6144)", size)
	}
	if size := len(ExecutionReadPolicy("111122223333", "us-east-1")); size > 6000 {
		t.Errorf("execution read policy size = %d, want <= 6000 (IAM managed policy limit 6144)", size)
	}
	if size := len(DeployPolicy("111122223333", "us-east-1")); size > 10000 {
		t.Errorf("deploy policy size = %d, want <= 10000 (role inline policy limit 10240)", size)
	}
}

func TestDeployPolicyScoped(t *testing.T) {
	document := DeployPolicy("111122223333", "eu-west-1")
	if strings.Contains(document, "AdministratorAccess") {
		t.Fatal("deploy policy must not reference AdministratorAccess")
	}
	actions := policyActions(t, document)
	for _, want := range []string{
		"cloudformation:CreateStack",
		"cloudformation:DeleteStack",
		"cloudformation:GetTemplate",
		"cloudformation:DetectStackDrift",
		"cloudformation:DescribeStackResourceDrifts",
		"s3:PutObject",
		"s3:CreateBucket",
		"ecs:CreateService",
		"elasticloadbalancing:CreateLoadBalancer",
		"elasticfilesystem:CreateFileSystem",
		"rds:CreateDBCluster",
		"ssm:PutParameter",
		"secretsmanager:CreateSecret",
		"iam:CreateRole",
		"iam:PassRole",
		"sts:AssumeRole",
		"sts:GetCallerIdentity",
		"cloudcontrol:ListResources",
		"cloudwatch:GetMetricData",
		"cloudwatch:DescribeAlarms",
		"cloudwatch:DescribeAlarmHistory",
		"cloudwatch:ListTagsForResource",
		"logs:StartQuery",
		"xray:GetTraceSummaries",
	} {
		if !hasAction(actions, want) {
			t.Errorf("deploy policy missing %s", want)
		}
	}
	for _, forbidden := range []string{"iam:CreateUser", "organizations:*", "account:*", "cloudtrail:*"} {
		if hasAction(actions, forbidden) {
			t.Errorf("deploy policy must not include %s", forbidden)
		}
	}
	for _, want := range []string{
		"arn:aws:s3:::cdk-hnb659fds-assets-111122223333-eu-west-1",
		"arn:aws:iam::111122223333:role/cdk-hnb659fds-*-111122223333-eu-west-1",
		"arn:aws:ecr:eu-west-1:111122223333:repository/cdk-hnb659fds-container-assets-111122223333-eu-west-1",
		"arn:aws:iam::111122223333:role/guardian*",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("deploy policy missing resource %s", want)
		}
	}
}

func TestExecutionPolicyScoped(t *testing.T) {
	document := ExecutionPolicy("111122223333", "eu-west-1")
	if strings.Contains(document, "AdministratorAccess") {
		t.Fatal("execution policy must not reference AdministratorAccess")
	}
	actions := policyActions(t, document)
	for _, want := range []string{
		"s3:CreateBucket",
		"ecs:CreateService",
		"elasticloadbalancing:CreateLoadBalancer",
		"elasticfilesystem:CreateFileSystem",
		"rds:CreateDBCluster",
		"iam:CreateRole",
		"iam:PassRole",
		"lambda:CreateFunction",
		"kms:CreateGrant",
		"logs:CreateLogGroup",
	} {
		if !hasAction(actions, want) {
			t.Errorf("execution policy missing %s", want)
		}
	}
	if hasAction(actions, "cloudformation:CreateStack") {
		t.Error("execution policy must not include CloudFormation deploy actions")
	}
	if hasAction(actions, "cloudwatch:GetMetricData") {
		t.Error("execution policy must not include observability read actions")
	}
	if !strings.Contains(document, `"kms:ViaService"`) {
		t.Error("execution policy should scope KMS via service")
	}
}

func TestExecutionPolicyCoversTemplateResourceFamilies(t *testing.T) {
	// Regression: the EFS/EC2/SSM actions required by the generated templates
	// (and the CDK bootstrap version parameter) must live in the execution
	// policy, not only in the deploy policy. Missing them shows up as
	// AccessDenied during `cdk deploy` in the CFN execution role.
	// Reads live in the companion policy; merge both for coverage checks.
	actions := append(
		policyActions(t, ExecutionPolicy("111122223333", "us-east-1")),
		policyActions(t, ExecutionReadPolicy("111122223333", "us-east-1"))...,
	)
	for _, want := range []string{
		"ssm:GetParameters",
		"ec2:CreateVpc",
		"ec2:CreateSubnet",
		"ec2:CreateInternetGateway",
		"ec2:AttachInternetGateway",
		"ec2:CreateRouteTable",
		"ec2:AssociateRouteTable",
		"ec2:AllocateAddress",
		"ec2:CreateNatGateway",
		"ec2:CreateNetworkInterface",
		"ec2:DeleteNetworkInterface",
		"ec2:ModifyNetworkInterfaceAttribute",
		"elasticfilesystem:CreateFileSystem",
		"elasticfilesystem:DeleteFileSystem",
		"elasticfilesystem:DescribeReplicationConfigurations",
		"elasticfilesystem:CreateMountTarget",
		"elasticfilesystem:DeleteMountTarget",
	} {
		if !hasAction(actions, want) {
			t.Errorf("execution policy missing %s", want)
		}
	}
}

func TestResolvePolicyMode(t *testing.T) {
	cases := []struct {
		input    string
		wantMode string
		wantName string
	}{
		{"", PolicyScoped, ""},
		{"scoped", PolicyScoped, ""},
		{"admin", "managed", "AdministratorAccess"},
		{"PowerUserAccess", "managed", "PowerUserAccess"},
	}
	for _, tc := range cases {
		mode, name := ResolvePolicyMode(tc.input)
		if mode != tc.wantMode || name != tc.wantName {
			t.Errorf("ResolvePolicyMode(%q) = %q, %q; want %q, %q", tc.input, mode, name, tc.wantMode, tc.wantName)
		}
	}
}

func TestResolveExecutionPolicy(t *testing.T) {
	cases := map[string]string{
		"":       PolicyScoped,
		"scoped": PolicyScoped,
		"admin":  "arn:aws:iam::aws:policy/AdministratorAccess",
		"arn:aws:iam::111122223333:policy/Custom": "arn:aws:iam::111122223333:policy/Custom",
	}
	for input, want := range cases {
		if got := ResolveExecutionPolicy(input); got != want {
			t.Errorf("ResolveExecutionPolicy(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestConfigDefaultsToScoped(t *testing.T) {
	cfg := Config{}.WithDefaults()
	if cfg.PolicyMode != PolicyScoped {
		t.Fatalf("default policy mode = %q", cfg.PolicyMode)
	}
	if cfg.PolicyName != "" {
		t.Fatalf("scoped mode must not attach a managed policy, got %q", cfg.PolicyName)
	}

	admin := Config{PolicyMode: PolicyAdmin}.WithDefaults()
	if admin.PolicyMode != "managed" || admin.PolicyName != "AdministratorAccess" {
		t.Fatalf("admin defaults = %+v", admin)
	}
}
