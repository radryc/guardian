package awssetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	awsroot "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

const trustPolicyDocument = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::%s:root"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}`

const trustPolicyDocumentRolesAnywhere = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::%s:root"
      },
      "Action": "sts:AssumeRole"
    },
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "rolesanywhere.amazonaws.com"
      },
      "Action": [
        "sts:AssumeRole",
        "sts:TagSession",
        "sts:SetSourceIdentity"
      ],
      "Condition": {
        "ArnEquals": {
          "aws:SourceArn": "%s"
        }
      }
    }
  ]
}`

const roleDescription = "CDK deployment role managed by Guardian. Allows CloudFormation stack create/update/delete and related resource provisioning."

// trustPolicy is the parsed assume-role policy document.
type trustPolicy struct {
	Version   string           `json:"Version"`
	Statement []trustStatement `json:"Statement"`
}

type trustStatement struct {
	Effect    string         `json:"Effect"`
	Principal map[string]any `json:"Principal,omitempty"`
	Action    any            `json:"Action,omitempty"`
	Condition map[string]any `json:"Condition,omitempty"`
}

// normalizeTrustPrincipals expands bare account ids to account-root ARNs and
// trims duplicates and whitespace.
func normalizeTrustPrincipals(principals []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(principals))
	for _, principal := range principals {
		value := strings.TrimSpace(principal)
		if value == "" {
			continue
		}
		if isBareAccountID(value) {
			value = fmt.Sprintf("arn:aws:iam::%s:root", value)
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func isBareAccountID(value string) bool {
	if len(value) != 12 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// appendTrustPrincipal adds an assume-role statement for the principal when it
// is not already trusted. It returns true when the document changed.
func appendTrustPrincipal(policy *trustPolicy, principal string) bool {
	for _, statement := range policy.Statement {
		awsPrincipal, ok := statement.Principal["AWS"]
		if !ok {
			continue
		}
		switch typed := awsPrincipal.(type) {
		case string:
			if typed == principal {
				return false
			}
		case []any:
			for _, item := range typed {
				if value, ok := item.(string); ok && value == principal {
					return false
				}
			}
		}
	}
	policy.Statement = append(policy.Statement, trustStatement{
		Effect:    "Allow",
		Principal: map[string]any{"AWS": principal},
		Action:    "sts:AssumeRole",
	})
	return true
}

// buildTrustPolicy renders the default deploy-role trust policy with optional
// extra principals.
func buildTrustPolicy(accountID string, extraPrincipals []string) (string, error) {
	var policy trustPolicy
	if err := json.Unmarshal([]byte(fmt.Sprintf(trustPolicyDocument, accountID)), &policy); err != nil {
		return "", err
	}
	for _, principal := range normalizeTrustPrincipals(extraPrincipals) {
		appendTrustPrincipal(&policy, principal)
	}
	raw, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// AddTrustPrincipals merges additional assume-role principals into the deploy
// role's trust policy. It is used by the multi-account pusher model where a
// fleet in a tools account assumes GuardianCdkDeployRole in target accounts.
func AddTrustPrincipals(ctx context.Context, cfg Config, principals []string) error {
	principals = normalizeTrustPrincipals(principals)
	if len(principals) == 0 {
		return nil
	}
	if cfg.DryRun {
		for _, principal := range principals {
			fmt.Printf("  would trust principal %s for role %s\n", principal, cfg.RoleName)
		}
		return nil
	}

	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return err
	}
	client := iam.NewFromConfig(awsCfg)
	out, err := client.GetRole(ctx, &iam.GetRoleInput{
		RoleName: awsroot.String(cfg.RoleName),
	})
	if err != nil {
		return fmt.Errorf("get role %s: %w", cfg.RoleName, err)
	}
	document, err := url.QueryUnescape(awsroot.ToString(out.Role.AssumeRolePolicyDocument))
	if err != nil {
		return fmt.Errorf("decode trust policy: %w", err)
	}
	var policy trustPolicy
	if err := json.Unmarshal([]byte(document), &policy); err != nil {
		return fmt.Errorf("parse trust policy: %w", err)
	}
	changed := false
	for _, principal := range principals {
		if appendTrustPrincipal(&policy, principal) {
			changed = true
		}
	}
	if !changed {
		return nil
	}
	raw, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return err
	}
	_, err = client.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
		RoleName:       awsroot.String(cfg.RoleName),
		PolicyDocument: awsroot.String(string(raw)),
	})
	if err != nil {
		return fmt.Errorf("update trust policy for role %s: %w", cfg.RoleName, err)
	}
	return nil
}

type RoleInfo struct {
	Name        string
	ARN         string
	Exists      bool
	CreatedAt   time.Time
	PolicyARN   string
	AttachedArn string
}

func GetRole(ctx context.Context, cfg Config, accountID string) (*RoleInfo, error) {
	if cfg.DryRun {
		return &RoleInfo{Name: cfg.RoleName, Exists: false}, nil
	}

	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	client := iam.NewFromConfig(awsCfg)
	out, err := client.GetRole(ctx, &iam.GetRoleInput{
		RoleName: awsroot.String(cfg.RoleName),
	})
	if err != nil {
		if isNoSuchEntity(err) {
			return &RoleInfo{Name: cfg.RoleName, Exists: false}, nil
		}
		return nil, fmt.Errorf("get role %s: %w", cfg.RoleName, err)
	}

	info := &RoleInfo{
		Name:      cfg.RoleName,
		ARN:       awsroot.ToString(out.Role.Arn),
		Exists:    true,
		CreatedAt: awsroot.ToTime(out.Role.CreateDate),
	}

	policies, err := client.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
		RoleName: awsroot.String(cfg.RoleName),
	})
	if err == nil {
		for _, p := range policies.AttachedPolicies {
			info.AttachedArn = awsroot.ToString(p.PolicyArn)
			break
		}
	}

	return info, nil
}

func EnsureRole(ctx context.Context, cfg Config, accountID string) (*RoleInfo, error) {
	existing, err := GetRole(ctx, cfg, accountID)
	if err != nil {
		return nil, err
	}
	if existing.Exists {
		return existing, nil
	}

	if cfg.DryRun {
		fmt.Printf("  would create IAM role: %s\n", cfg.RoleName)
		if cfg.PolicyName != "" {
			fmt.Printf("  would attach policy: %s\n", cfg.PolicyName)
		} else {
			fmt.Printf("  would attach inline policy: %s\n", InlinePolicyName)
		}
		return &RoleInfo{Name: cfg.RoleName, Exists: true, PolicyARN: cfg.PolicyName}, nil
	}

	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	client := iam.NewFromConfig(awsCfg)

	trustPolicy, err := buildTrustPolicy(accountID, cfg.TrustPrincipals)
	if err != nil {
		return nil, fmt.Errorf("build trust policy: %w", err)
	}

	createOut, err := client.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 awsroot.String(cfg.RoleName),
		AssumeRolePolicyDocument: awsroot.String(trustPolicy),
		Description:              awsroot.String(roleDescription),
		Tags: []iamtypes.Tag{
			{Key: awsroot.String("managed-by"), Value: awsroot.String("guardian")},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create role %s: %w", cfg.RoleName, err)
	}

	policyARN := ""
	if cfg.PolicyName != "" {
		policyARN = "arn:aws:iam::aws:policy/" + cfg.PolicyName
		_, err = client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			RoleName:  awsroot.String(cfg.RoleName),
			PolicyArn: awsroot.String(policyARN),
		})
		if err != nil {
			return nil, fmt.Errorf("attach policy %s to role %s: %w", policyARN, cfg.RoleName, err)
		}
	} else {
		if err := ensureInlinePolicy(ctx, cfg, InlinePolicyName, DeployPolicy(accountID, cfg.Region)); err != nil {
			return nil, err
		}
	}

	time.Sleep(2 * time.Second)

	_, err = client.GetRole(ctx, &iam.GetRoleInput{
		RoleName: awsroot.String(cfg.RoleName),
	})
	if err != nil {
		return nil, fmt.Errorf("verify role %s after creation: %w", cfg.RoleName, err)
	}

	return &RoleInfo{
		Name:      cfg.RoleName,
		ARN:       awsroot.ToString(createOut.Role.Arn),
		Exists:    true,
		CreatedAt: awsroot.ToTime(createOut.Role.CreateDate),
		PolicyARN: policyARN,
	}, nil
}

// ensureInlinePolicy puts (or updates) the named inline policy on the role.
func ensureInlinePolicy(ctx context.Context, cfg Config, name, document string) error {
	if cfg.DryRun {
		fmt.Printf("  would put inline policy %s on %s\n", name, cfg.RoleName)
		return nil
	}
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return err
	}
	client := iam.NewFromConfig(awsCfg)
	_, err = client.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
		RoleName:       awsroot.String(cfg.RoleName),
		PolicyName:     awsroot.String(name),
		PolicyDocument: awsroot.String(document),
	})
	if err != nil {
		return fmt.Errorf("put inline policy %s on role %s: %w", name, cfg.RoleName, err)
	}
	return nil
}

// EnsureInlinePolicy puts (or updates) the named inline policy on the role.
func EnsureInlinePolicy(ctx context.Context, cfg Config, name, document string) error {
	return ensureInlinePolicy(ctx, cfg, name, document)
}

// AttachManagedPolicy attaches a managed policy (name or ARN) to the role.
func AttachManagedPolicy(ctx context.Context, cfg Config, policy string) error {
	policyARN := strings.TrimSpace(policy)
	if policyARN == "" {
		return fmt.Errorf("policy is required")
	}
	if !strings.HasPrefix(policyARN, "arn:") {
		policyARN = "arn:aws:iam::aws:policy/" + policyARN
	}
	if cfg.DryRun {
		fmt.Printf("  would attach %s to %s\n", policyARN, cfg.RoleName)
		return nil
	}
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return err
	}
	client := iam.NewFromConfig(awsCfg)
	_, err = client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  awsroot.String(cfg.RoleName),
		PolicyArn: awsroot.String(policyARN),
	})
	if err != nil {
		return fmt.Errorf("attach %s to role %s: %w", policyARN, cfg.RoleName, err)
	}
	return nil
}

// DeleteInlinePolicy removes an inline policy from the role (best effort).
func DeleteInlinePolicy(ctx context.Context, cfg Config, name string) error {
	if cfg.DryRun {
		fmt.Printf("  would delete inline policy %s from %s\n", name, cfg.RoleName)
		return nil
	}
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return err
	}
	client := iam.NewFromConfig(awsCfg)
	_, err = client.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
		RoleName:   awsroot.String(cfg.RoleName),
		PolicyName: awsroot.String(name),
	})
	if err != nil && !isNoSuchEntity(err) {
		return fmt.Errorf("delete inline policy %s from role %s: %w", name, cfg.RoleName, err)
	}
	return nil
}

// AttachedRolePolicies lists the managed policy ARNs attached to the role.
func AttachedRolePolicies(ctx context.Context, cfg Config) ([]string, error) {
	if cfg.DryRun {
		return nil, nil
	}
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client := iam.NewFromConfig(awsCfg)
	out, err := client.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
		RoleName: awsroot.String(cfg.RoleName),
	})
	if err != nil {
		return nil, fmt.Errorf("list attached policies for %s: %w", cfg.RoleName, err)
	}
	arns := make([]string, 0, len(out.AttachedPolicies))
	for _, policy := range out.AttachedPolicies {
		arns = append(arns, awsroot.ToString(policy.PolicyArn))
	}
	return arns, nil
}

// DetachManagedPolicy detaches a managed policy from the role (best effort).
func DetachManagedPolicy(ctx context.Context, cfg Config, policyARN string) error {
	if cfg.DryRun {
		fmt.Printf("  would detach %s from %s\n", policyARN, cfg.RoleName)
		return nil
	}
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return err
	}
	client := iam.NewFromConfig(awsCfg)
	_, err = client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
		RoleName:  awsroot.String(cfg.RoleName),
		PolicyArn: awsroot.String(policyARN),
	})
	if err != nil && !isNoSuchEntity(err) {
		return fmt.Errorf("detach %s from role %s: %w", policyARN, cfg.RoleName, err)
	}
	return nil
}

// EnsureManagedPolicy creates or updates the named customer-managed policy and
// returns its ARN. The document is only replaced when it changed.
func EnsureManagedPolicy(ctx context.Context, cfg Config, name, document string) (string, error) {
	if cfg.DryRun {
		fmt.Printf("  would create/update managed policy %s\n", name)
		return fmt.Sprintf("arn:aws:iam::%s:policy/%s", accountPlaceholder, name), nil
	}
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return "", err
	}
	client := iam.NewFromConfig(awsCfg)
	accountID, err := currentAccountID(ctx, cfg)
	if err != nil {
		return "", err
	}
	policyARN := fmt.Sprintf("arn:aws:iam::%s:policy/%s", accountID, name)

	existing, err := client.GetPolicy(ctx, &iam.GetPolicyInput{PolicyArn: awsroot.String(policyARN)})
	if err != nil && !isNoSuchEntity(err) {
		return "", fmt.Errorf("get policy %s: %w", name, err)
	}
	if err == nil {
		version := awsroot.ToString(existing.Policy.DefaultVersionId)
		current, err := client.GetPolicyVersion(ctx, &iam.GetPolicyVersionInput{
			PolicyArn: awsroot.String(policyARN),
			VersionId: awsroot.String(version),
		})
		if err != nil {
			return "", fmt.Errorf("get policy version %s: %w", name, err)
		}
		decoded, err := url.QueryUnescape(awsroot.ToString(current.PolicyVersion.Document))
		if err != nil {
			return "", fmt.Errorf("decode policy %s: %w", name, err)
		}
		if samePolicyDocument(decoded, document) {
			return policyARN, nil
		}
		_, err = client.CreatePolicyVersion(ctx, &iam.CreatePolicyVersionInput{
			PolicyArn:      awsroot.String(policyARN),
			PolicyDocument: awsroot.String(document),
			SetAsDefault:   true,
		})
		if err != nil {
			return "", fmt.Errorf("update policy %s: %w", name, err)
		}
		prunePolicyVersions(ctx, client, policyARN)
		return policyARN, nil
	}

	_, err = client.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     awsroot.String(name),
		PolicyDocument: awsroot.String(document),
		Description:    awsroot.String("Scoped execution policy for Guardian-managed CloudFormation stacks"),
	})
	if err != nil {
		return "", fmt.Errorf("create policy %s: %w", name, err)
	}
	return policyARN, nil
}

// samePolicyDocument compares two policy documents semantically.
func samePolicyDocument(a, b string) bool {
	var left, right any
	if err := json.Unmarshal([]byte(a), &left); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(b), &right); err != nil {
		return false
	}
	leftRaw, _ := json.Marshal(left)
	rightRaw, _ := json.Marshal(right)
	return string(leftRaw) == string(rightRaw)
}

const accountPlaceholder = "000000000000"

// prunePolicyVersions deletes non-default policy versions so repeated updates
// do not hit the five-version limit. Best effort.
func prunePolicyVersions(ctx context.Context, client *iam.Client, policyARN string) {
	versions, err := client.ListPolicyVersions(ctx, &iam.ListPolicyVersionsInput{
		PolicyArn: awsroot.String(policyARN),
	})
	if err != nil {
		return
	}
	for _, version := range versions.Versions {
		if version.IsDefaultVersion {
			continue
		}
		_, _ = client.DeletePolicyVersion(ctx, &iam.DeletePolicyVersionInput{
			PolicyArn: awsroot.String(policyARN),
			VersionId: version.VersionId,
		})
	}
}

func currentAccountID(ctx context.Context, cfg Config) (string, error) {
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return "", err
	}
	identity, err := sts.NewFromConfig(awsCfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("get caller identity: %w", err)
	}
	return awsroot.ToString(identity.Account), nil
}

func UpdateRoleTrustPolicyForRolesAnywhere(ctx context.Context, cfg Config, accountID, trustAnchorARN string) error {
	if cfg.DryRun {
		fmt.Printf("  would update role trust policy with rolesanywhere.amazonaws.com for anchor %s\n", trustAnchorARN)
		return nil
	}

	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return err
	}

	client := iam.NewFromConfig(awsCfg)
	trustPolicy := fmt.Sprintf(trustPolicyDocumentRolesAnywhere, accountID, trustAnchorARN)

	_, err = client.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
		RoleName:       awsroot.String(cfg.RoleName),
		PolicyDocument: awsroot.String(trustPolicy),
	})
	if err != nil {
		return fmt.Errorf("update trust policy for role %s: %w", cfg.RoleName, err)
	}
	return nil
}

func loadAWSConfig(ctx context.Context, cfg Config) (awsroot.Config, error) {
	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	if cfg.Profile != "" {
		loadOpts = append(loadOpts, awsconfig.WithSharedConfigProfile(cfg.Profile))
	}
	return awsconfig.LoadDefaultConfig(ctx, loadOpts...)
}

func isNoSuchEntity(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return strings.EqualFold(apiErr.ErrorCode(), "NoSuchEntity")
	}
	return strings.Contains(strings.ToLower(err.Error()), "nosuchentity")
}

// PassExecutionRolesPolicyName is the inline policy added to the CDK bootstrap
// deploy role so the CLI can pass per-intent execution roles
// (guardian-cfn-exec-*) to CloudFormation.
const PassExecutionRolesPolicyName = "GuardianPassExecutionRoles"

// EnsureDeployRolePassesExecutionRoles lets the CDK bootstrap deploy role hand
// the per-intent execution roles to CloudFormation. Without it `cdk deploy
// --role-arn arn:...:role/guardian-cfn-exec-...` fails with AccessDenied on
// iam:PassRole.
func EnsureDeployRolePassesExecutionRoles(ctx context.Context, cfg Config, accountID string) error {
	roleName := fmt.Sprintf("cdk-hnb659fds-deploy-role-%s-%s", accountID, cfg.Region)
	document := renderPolicy([]policyStatement{
		{
			Sid:      "PassGuardianExecutionRoles",
			Effect:   "Allow",
			Action:   []string{"iam:PassRole"},
			Resource: fmt.Sprintf("arn:aws:iam::%s:role/%s*", accountID, "guardian-cfn-exec-"),
		},
	})
	if cfg.DryRun {
		fmt.Printf("  would allow %s to pass guardian-cfn-exec-* roles\n", roleName)
		return nil
	}
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return err
	}
	client := iam.NewFromConfig(awsCfg)
	if _, err := client.GetRole(ctx, &iam.GetRoleInput{RoleName: awsroot.String(roleName)}); err != nil {
		if isNoSuchEntity(err) {
			// A non-default bootstrap qualifier or role name; per-intent roles
			// then rely on the account bootstrap role instead.
			fmt.Printf("  note: %s not found, skipping per-intent execution role grants\n", roleName)
			return nil
		}
		return fmt.Errorf("get %s: %w", roleName, err)
	}
	if _, err := client.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
		RoleName:       awsroot.String(roleName),
		PolicyName:     awsroot.String(PassExecutionRolesPolicyName),
		PolicyDocument: awsroot.String(document),
	}); err != nil {
		return fmt.Errorf("allow %s to pass execution roles: %w", roleName, err)
	}
	fmt.Printf("  %s may pass guardian-cfn-exec-* execution roles\n", roleName)
	return nil
}
