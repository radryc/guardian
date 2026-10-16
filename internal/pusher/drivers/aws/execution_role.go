package awsdriver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	iamsdk "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// ExecutionRolePrefix names the per-intent CloudFormation execution roles.
// One role per partition/intent carries only the actions the intent's selected
// templates declare, instead of the account-wide bootstrap role.
const ExecutionRolePrefix = "guardian-cfn-exec-"

// Execution role modes accepted by the pusher.
const (
	ExecutionRoleShared    = "shared"
	ExecutionRolePerIntent = "per-intent"
)

// ExecutionRoleName derives the role name for a partition/intent (IAM names
// are limited to 64 characters).
func ExecutionRoleName(partition, intent string) string {
	name := ExecutionRolePrefix + sanitizeRoleName(partition) + "-" + sanitizeRoleName(intent)
	name = strings.Trim(name, "-")
	if len(name) > 64 {
		name = strings.Trim(name[:64], "-")
	}
	if name == ExecutionRolePrefix || name == "" {
		return ""
	}
	return name
}

func sanitizeRoleName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	prevDash := false
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '=' || r == ',' || r == '.' || r == '@' || r == '-' || r == '_':
			b.WriteRune(r)
			prevDash = r == '-'
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// ExecutionRoleTrustPolicy lets CloudFormation assume the role for one stack.
func ExecutionRoleTrustPolicy() string {
	document := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect":    "Allow",
				"Principal": map[string]any{"Service": "cloudformation.amazonaws.com"},
				"Action":    "sts:AssumeRole",
			},
		},
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return ""
	}
	return string(raw)
}

// SplitExecutionRoleDocuments splits declared actions into inline write and
// read policy documents. Empty strings mean "no actions of that kind".
func SplitExecutionRoleDocuments(actions []string) (write, read string) {
	var writeActions, readActions []string
	for _, action := range actions {
		action = strings.TrimSpace(action)
		if action == "" {
			continue
		}
		idx := strings.Index(action, ":")
		if idx < 0 {
			writeActions = append(writeActions, action)
			continue
		}
		verb := action[idx+1:]
		switch {
		case strings.HasPrefix(verb, "Describe"), strings.HasPrefix(verb, "Get"), strings.HasPrefix(verb, "List"):
			readActions = append(readActions, action)
		default:
			writeActions = append(writeActions, action)
		}
	}
	// CloudFormation's resource handlers perform reads of their own (for
	// example elasticfilesystem:DescribeBackupPolicy while resolving an EFS
	// ARN), so every service the intent uses also gets its read wildcards.
	services := map[string]bool{}
	for _, action := range append(append([]string{}, writeActions...), readActions...) {
		if idx := strings.Index(action, ":"); idx > 0 {
			services[action[:idx]] = true
		}
	}
	for service := range services {
		readActions = append(readActions, service+":Describe*", service+":Get*", service+":List*")
	}
	sort.Strings(writeActions)
	sort.Strings(readActions)
	return renderInlinePolicy(writeActions), renderInlinePolicy(readActions)
}

func renderInlinePolicy(actions []string) string {
	if len(actions) == 0 {
		return ""
	}
	document := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect":   "Allow",
				"Action":   actions,
				"Resource": "*",
			},
		},
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return ""
	}
	return string(raw)
}

const (
	executionRoleWritePolicyName = "guardian-write"
	executionRoleReadPolicyName  = "guardian-read"
)

// EnsureExecutionRole creates or updates a per-intent execution role with the
// declared actions and returns its ARN.
func (b *AWSBackend) EnsureExecutionRole(ctx context.Context, roleName string, actions []string) (string, error) {
	if strings.TrimSpace(roleName) == "" {
		return "", fmt.Errorf("execution role name is required")
	}
	if b.dry() {
		return fmt.Sprintf("arn:aws:iam::000000000000:role/%s", roleName), nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return "", err
	}
	client := iamsdk.NewFromConfig(cfg)

	if _, err := client.CreateRole(ctx, &iamsdk.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(ExecutionRoleTrustPolicy()),
		Description:              aws.String("Guardian per-intent CloudFormation execution role"),
		Tags: []iamtypes.Tag{
			{Key: aws.String("guardian-managed"), Value: aws.String("true")},
		},
	}); err != nil {
		var exists *iamtypes.EntityAlreadyExistsException
		if !asEntityExists(err, &exists) {
			return "", fmt.Errorf("create execution role %s: %w", roleName, err)
		}
	}

	writeDoc, readDoc := SplitExecutionRoleDocuments(actions)
	if writeDoc != "" {
		if _, err := client.PutRolePolicy(ctx, &iamsdk.PutRolePolicyInput{
			RoleName:       aws.String(roleName),
			PolicyName:     aws.String(executionRoleWritePolicyName),
			PolicyDocument: aws.String(writeDoc),
		}); err != nil {
			return "", fmt.Errorf("put write policy on %s: %w", roleName, err)
		}
	}
	if readDoc != "" {
		if _, err := client.PutRolePolicy(ctx, &iamsdk.PutRolePolicyInput{
			RoleName:       aws.String(roleName),
			PolicyName:     aws.String(executionRoleReadPolicyName),
			PolicyDocument: aws.String(readDoc),
		}); err != nil {
			return "", fmt.Errorf("put read policy on %s: %w", roleName, err)
		}
	}

	account := ""
	if cfg.Credentials != nil {
		if creds, err := cfg.Credentials.Retrieve(ctx); err == nil {
			account = creds.AccountID
		}
	}
	return fmt.Sprintf("arn:aws:iam::%s:role/%s", account, roleName), nil
}

// DeleteExecutionRole removes a per-intent execution role and its inline
// policies (best effort; missing roles are ignored).
func (b *AWSBackend) DeleteExecutionRole(ctx context.Context, roleName string) error {
	if strings.TrimSpace(roleName) == "" || b.dry() {
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := iamsdk.NewFromConfig(cfg)
	for _, policy := range []string{executionRoleWritePolicyName, executionRoleReadPolicyName} {
		if _, err := client.DeleteRolePolicy(ctx, &iamsdk.DeleteRolePolicyInput{
			RoleName:   aws.String(roleName),
			PolicyName: aws.String(policy),
		}); err != nil && !asNoSuchEntity(err) {
			return fmt.Errorf("delete policy %s on %s: %w", policy, roleName, err)
		}
	}
	if _, err := client.DeleteRole(ctx, &iamsdk.DeleteRoleInput{RoleName: aws.String(roleName)}); err != nil && !asNoSuchEntity(err) {
		return fmt.Errorf("delete execution role %s: %w", roleName, err)
	}
	return nil
}

func asEntityExists(err error, target **iamtypes.EntityAlreadyExistsException) bool {
	var typed *iamtypes.EntityAlreadyExistsException
	if errors.As(err, &typed) {
		*target = typed
		return true
	}
	return false
}

func asNoSuchEntity(err error) bool {
	var typed *iamtypes.NoSuchEntityException
	return errors.As(err, &typed)
}
