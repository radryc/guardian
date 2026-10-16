package awsdriver

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExecutionRoleNameScopesAndTruncates(t *testing.T) {
	if got := ExecutionRoleName("demo-deploy", "web"); got != "guardian-cfn-exec-demo-deploy-web" {
		t.Fatalf("ExecutionRoleName = %q", got)
	}
	// Unsupported characters are collapsed and the name is capped at 64 chars.
	long := ExecutionRoleName(strings.Repeat("Partition ", 10), strings.Repeat("Intent ", 10))
	if len(long) > 64 {
		t.Fatalf("role name too long (%d): %q", len(long), long)
	}
	if strings.Contains(long, " ") || strings.Contains(long, "_") {
		t.Fatalf("role name has invalid characters: %q", long)
	}
}

func TestExecutionRoleTrustPolicyAllowsCloudFormation(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(ExecutionRoleTrustPolicy()), &parsed); err != nil {
		t.Fatalf("trust policy is not valid JSON: %v", err)
	}
	if !strings.Contains(ExecutionRoleTrustPolicy(), "cloudformation.amazonaws.com") {
		t.Fatalf("trust policy must allow CloudFormation: %s", ExecutionRoleTrustPolicy())
	}
}

func TestSplitExecutionRoleDocuments(t *testing.T) {
	write, read := SplitExecutionRoleDocuments([]string{
		"ec2:CreateVpc",
		"ec2:DescribeVpcs",
		"iam:PassRole",
		"iam:ListRolePolicies",
		"ecs:ListClusters",
	})
	if write == "" || read == "" {
		t.Fatalf("expected both documents, got write=%q read=%q", write, read)
	}
	var writeDoc, readDoc struct {
		Statement []struct {
			Action []string `json:"Action"`
		} `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(write), &writeDoc); err != nil {
		t.Fatalf("write document invalid: %v", err)
	}
	if err := json.Unmarshal([]byte(read), &readDoc); err != nil {
		t.Fatalf("read document invalid: %v", err)
	}
	writeActions := strings.Join(writeDoc.Statement[0].Action, ",")
	if !strings.Contains(writeActions, "ec2:CreateVpc") || !strings.Contains(writeActions, "iam:PassRole") {
		t.Fatalf("write actions = %v", writeDoc.Statement[0].Action)
	}
	if strings.Contains(writeActions, "ec2:DescribeVpcs") {
		t.Fatalf("describe actions must not be in the write document: %v", writeDoc.Statement[0].Action)
	}
	readActions := strings.Join(readDoc.Statement[0].Action, ",")
	for _, want := range []string{"ec2:DescribeVpcs", "iam:ListRolePolicies", "ecs:ListClusters"} {
		if !strings.Contains(readActions, want) {
			t.Fatalf("read actions missing %s: %v", want, readDoc.Statement[0].Action)
		}
	}

	// A service used for writes still gets its read wildcards, because
	// CloudFormation handlers perform reads of their own.
	writeOnly, readOnly := SplitExecutionRoleDocuments([]string{"s3:CreateBucket"})
	if writeOnly == "" || readOnly == "" {
		t.Fatalf("expected a write document plus read wildcards, got (%q, %q)", writeOnly, readOnly)
	}
	if !strings.Contains(readOnly, "s3:Describe*") || !strings.Contains(readOnly, "s3:List*") {
		t.Fatalf("read document missing per-service wildcards: %s", readOnly)
	}
	if w, r := SplitExecutionRoleDocuments(nil); w != "" || r != "" {
		t.Fatalf("empty actions must yield no documents, got (%q, %q)", w, r)
	}
}
