package awssetup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	awsroot "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
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

const roleDescription = "CDK deployment role managed by Guardian. Allows CloudFormation stack create/update/delete and related resource provisioning."

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
		Name:    cfg.RoleName,
		ARN:     awsroot.ToString(out.Role.Arn),
		Exists:  true,
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
		fmt.Printf("  would attach policy: %s\n", cfg.PolicyName)
		return &RoleInfo{Name: cfg.RoleName, Exists: true, PolicyARN: cfg.PolicyName}, nil
	}

	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	client := iam.NewFromConfig(awsCfg)

	trustPolicy := fmt.Sprintf(trustPolicyDocument, accountID)

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

	policyARN := "arn:aws:iam::aws:policy/" + cfg.PolicyName
	_, err = client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  awsroot.String(cfg.RoleName),
		PolicyArn: awsroot.String(policyARN),
	})
	if err != nil {
		return nil, fmt.Errorf("attach policy %s to role %s: %w", policyARN, cfg.RoleName, err)
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
