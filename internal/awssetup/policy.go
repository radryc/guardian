package awssetup

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cdkgen"
)

// Policy modes accepted by `guardianctl aws setup --policy`.
const (
	// PolicyScoped applies the built-in least-privilege deploy policy
	// (deploy + scan + observed/drift + CloudWatch read).
	PolicyScoped = "scoped"
	// PolicyAdmin attaches AdministratorAccess (broad; for labs only).
	PolicyAdmin = "admin"
)

// ExecutionPolicyName is the customer-managed policy attached to the CDK
// bootstrap CloudFormation execution role when --execution-policy=scoped.
const ExecutionPolicyName = "GuardianCfnExecutionPolicy"

// InlinePolicyName is the inline policy put on GuardianCdkDeployRole when
// --policy=scoped.
const InlinePolicyName = "GuardianDeployAccess"

type policyStatement struct {
	Sid       string         `json:"Sid,omitempty"`
	Effect    string         `json:"Effect"`
	Action    any            `json:"Action"`
	Resource  any            `json:"Resource"`
	Condition map[string]any `json:"Condition,omitempty"`
}

func renderPolicy(statements []policyStatement) string {
	document := map[string]any{
		"Version":   "2012-10-17",
		"Statement": statements,
	}
	raw, err := json.Marshal(document)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// DeployPolicy is the scoped inline policy for GuardianCdkDeployRole: enough
// to run CDK deploys (CloudFormation + CDK asset publishing + bootstrap role
// assumption), the native AWS drivers, account scans, observed drift
// detection, and the read-only CloudWatch/X-Ray surface.
func DeployPolicy(accountID, region string) string {
	statements := []policyStatement{
		{
			Sid:    "CloudFormationDeploy",
			Effect: "Allow",
			Action: []string{
				"cloudformation:CreateStack",
				"cloudformation:UpdateStack",
				"cloudformation:DeleteStack",
				"cloudformation:Describe*",
				"cloudformation:List*",
				"cloudformation:Get*",
				"cloudformation:List*",
				"cloudformation:DescribeChangeSet",
				"cloudformation:CreateChangeSet",
				"cloudformation:ExecuteChangeSet",
				"cloudformation:DeleteChangeSet",
				"cloudformation:ContinueUpdateRollback",
				"cloudformation:CancelUpdateStack",
				"cloudformation:UpdateTerminationProtection",
				"cloudformation:TagResource",
				"cloudformation:UntagResource",
				"cloudformation:ValidateTemplate",
				"cloudformation:GetStackPolicy",
				"cloudformation:SetStackPolicy",
				"cloudformation:DetectStackDrift",
				"cloudformation:Describe*",
			},
			Resource: "*",
		},
		{
			Sid:    "CdkAssetsBucket",
			Effect: "Allow",
			Action: []string{
				"s3:GetObject",
				"s3:PutObject",
				"s3:DeleteObject",
				"s3:AbortMultipartUpload",
				"s3:List*",
				"s3:GetBucketLocation",
			},
			Resource: []string{
				fmt.Sprintf("arn:aws:s3:::cdk-hnb659fds-assets-%s-%s", accountID, region),
				fmt.Sprintf("arn:aws:s3:::cdk-hnb659fds-assets-%s-%s/*", accountID, region),
			},
		},
		{
			Sid:    "GuardianBuckets",
			Effect: "Allow",
			Action: []string{
				"s3:CreateBucket",
				"s3:DeleteBucket",
				"s3:HeadBucket",
				"s3:GetBucketVersioning",
				"s3:PutBucketVersioning",
				"s3:GetBucketTagging",
				"s3:PutBucketTagging",
				"s3:DeleteBucketTagging",
				"s3:GetEncryptionConfiguration",
				"s3:PutEncryptionConfiguration",
				"s3:GetBucketPolicy",
				"s3:PutBucketPolicy",
				"s3:DeleteBucketPolicy",
				"s3:GetObject",
				"s3:PutObject",
				"s3:DeleteObject",
				"s3:ListBucket",
			},
			Resource: []string{
				"arn:aws:s3:::guardian-*",
				"arn:aws:s3:::guardian-*/*",
			},
		},
		{
			Sid:      "BucketDiscovery",
			Effect:   "Allow",
			Action:   []string{"s3:ListAllMyBuckets", "s3:GetBucketLocation", "s3:GetBucketTagging"},
			Resource: "*",
		},
		{
			Sid:      "CdkImageAssetToken",
			Effect:   "Allow",
			Action:   []string{"ecr:GetAuthorizationToken"},
			Resource: "*",
		},
		{
			Sid:    "CdkImageAssets",
			Effect: "Allow",
			Action: []string{
				"ecr:BatchCheckLayerAvailability",
				"ecr:BatchGetImage",
				"ecr:GetDownloadUrlForLayer",
				"ecr:InitiateLayerUpload",
				"ecr:UploadLayerPart",
				"ecr:CompleteLayerUpload",
				"ecr:PutImage",
			},
			Resource: fmt.Sprintf("arn:aws:ecr:%s:%s:repository/cdk-hnb659fds-container-assets-%s-%s", region, accountID, accountID, region),
		},
		{
			Sid:      "CdkBootstrapRoles",
			Effect:   "Allow",
			Action:   []string{"sts:AssumeRole", "sts:TagSession"},
			Resource: fmt.Sprintf("arn:aws:iam::%s:role/cdk-hnb659fds-*-%s-%s", accountID, accountID, region),
		},
		{
			Sid:    "Ecs",
			Effect: "Allow",
			Action: []string{
				"ecs:CreateCluster",
				"ecs:DeleteCluster",
				"ecs:DescribeClusters",
				"ecs:ListClusters",
				"ecs:CreateService",
				"ecs:UpdateService",
				"ecs:DeleteService",
				"ecs:DescribeServices",
				"ecs:ListServices",
				"ecs:RegisterTaskDefinition",
				"ecs:DeregisterTaskDefinition",
				"ecs:DescribeTaskDefinition",
				"ecs:ListTasks",
				"ecs:DescribeTasks",
				"ecs:TagResource",
				"ecs:UntagResource",
				"ecs:ListTagsForResource",
			},
			Resource: "*",
		},
		{
			Sid:    "LoadBalancing",
			Effect: "Allow",
			Action: []string{
				"elasticloadbalancing:CreateLoadBalancer",
				"elasticloadbalancing:DeleteLoadBalancer",
				"elasticloadbalancing:DescribeLoadBalancers",
				"elasticloadbalancing:ModifyLoadBalancerAttributes",
				"elasticloadbalancing:CreateTargetGroup",
				"elasticloadbalancing:DeleteTargetGroup",
				"elasticloadbalancing:DescribeTargetGroups",
				"elasticloadbalancing:ModifyTargetGroup",
				"elasticloadbalancing:RegisterTargets",
				"elasticloadbalancing:DeregisterTargets",
				"elasticloadbalancing:DescribeTargetHealth",
				"elasticloadbalancing:CreateListener",
				"elasticloadbalancing:DeleteListener",
				"elasticloadbalancing:ModifyListener",
				"elasticloadbalancing:DescribeListeners",
				"elasticloadbalancing:SetSecurityGroups",
				"elasticloadbalancing:SetSubnets",
				"elasticloadbalancing:AddTags",
				"elasticloadbalancing:RemoveTags",
				"elasticloadbalancing:DescribeTags",
			},
			Resource: "*",
		},
		{
			Sid:    "FileSystems",
			Effect: "Allow",
			Action: []string{
				"elasticfilesystem:CreateFileSystem",
				"elasticfilesystem:DeleteFileSystem",
				"elasticfilesystem:Describe*",
				"elasticfilesystem:CreateMountTarget",
				"elasticfilesystem:DeleteMountTarget",
				"elasticfilesystem:CreateAccessPoint",
				"elasticfilesystem:DeleteAccessPoint",
				"elasticfilesystem:DescribeAccessPoints",
				"elasticfilesystem:PutLifecycleConfiguration",
				"elasticfilesystem:PutFileSystemPolicy",
				"elasticfilesystem:TagResource",
				"elasticfilesystem:UntagResource",
				"elasticfilesystem:ListTagsForResource",
			},
			Resource: "*",
		},
		{
			Sid:    "Databases",
			Effect: "Allow",
			Action: []string{
				"rds:CreateDBCluster",
				"rds:ModifyDBCluster",
				"rds:DeleteDBCluster",
				"rds:DescribeDBClusters",
				"rds:CreateDBInstance",
				"rds:DeleteDBInstance",
				"rds:DescribeDBInstances",
				"rds:CreateDBSubnetGroup",
				"rds:DeleteDBSubnetGroup",
				"rds:Describe*",
				"rds:DeleteDBClusterSnapshot",
				"rds:AddTagsToResource",
				"rds:ListTagsForResource",
			},
			Resource: "*",
		},
		{
			Sid:    "Parameters",
			Effect: "Allow",
			Action: []string{
				"ssm:PutParameter",
				"ssm:DeleteParameter",
				"ssm:Get*",
				"ssm:DescribeParameters",
				"ssm:AddTagsToResource",
				"ssm:ListTagsForResource",
			},
			Resource: "*",
		},
		{
			Sid:    "Secrets",
			Effect: "Allow",
			Action: []string{
				"secretsmanager:CreateSecret",
				"secretsmanager:UpdateSecret",
				"secretsmanager:PutSecretValue",
				"secretsmanager:DeleteSecret",
				"secretsmanager:RestoreSecret",
				"secretsmanager:DescribeSecret",
				"secretsmanager:GetSecretValue",
				"secretsmanager:ListSecrets",
				"secretsmanager:TagResource",
				"secretsmanager:ListTagsForResource",
			},
			Resource: "*",
		},
		{
			Sid:    "Logs",
			Effect: "Allow",
			Action: []string{
				"logs:CreateLogGroup",
				"logs:DeleteLogGroup",
				"logs:DescribeLogGroups",
				"logs:PutRetentionPolicy",
				"logs:TagLogGroup",
				"logs:List*",
				"logs:TagResource",
				"logs:DescribeLogStreams",
				"logs:GetLogEvents",
				"logs:StartQuery",
				"logs:GetQueryResults",
				"logs:StopQuery",
			},
			Resource: "*",
		},
		{
			Sid:    "NetworkAndSecurityGroups",
			Effect: "Allow",
			Action: []string{
				"ec2:CreateVpc",
				"ec2:DeleteVpc",
				"ec2:ModifyVpcAttribute",
				"ec2:DescribeVpcs",
				"ec2:CreateSubnet",
				"ec2:DeleteSubnet",
				"ec2:ModifySubnetAttribute",
				"ec2:DescribeSubnets",
				"ec2:CreateInternetGateway",
				"ec2:DeleteInternetGateway",
				"ec2:AttachInternetGateway",
				"ec2:DetachInternetGateway",
				"ec2:DescribeInternetGateways",
				"ec2:CreateRouteTable",
				"ec2:DeleteRouteTable",
				"ec2:CreateRoute",
				"ec2:DeleteRoute",
				"ec2:ReplaceRoute",
				"ec2:AssociateRouteTable",
				"ec2:DisassociateRouteTable",
				"ec2:DescribeRouteTables",
				"ec2:AllocateAddress",
				"ec2:ReleaseAddress",
				"ec2:DescribeAddresses",
				"ec2:CreateNatGateway",
				"ec2:DeleteNatGateway",
				"ec2:DescribeNatGateways",
				"ec2:CreateSecurityGroup",
				"ec2:DeleteSecurityGroup",
				"ec2:AuthorizeSecurityGroupIngress",
				"ec2:AuthorizeSecurityGroupEgress",
				"ec2:RevokeSecurityGroupIngress",
				"ec2:RevokeSecurityGroupEgress",
				"ec2:Describe*",
				"ec2:CreateTags",
				"ec2:DeleteTags",
			},
			Resource: "*",
		},
		{
			Sid:    "Discovery",
			Effect: "Allow",
			Action: []string{
				"ec2:Describe*",
			},
			Resource: "*",
		},
		{
			Sid:    "TaskRoles",
			Effect: "Allow",
			Action: []string{
				"iam:CreateRole",
				"iam:DeleteRole",
				"iam:GetRole",
				"iam:ListRoles",
				"iam:PutRolePolicy",
				"iam:DeleteRolePolicy",
				"iam:GetRolePolicy",
				"iam:List*",
				"iam:AttachRolePolicy",
				"iam:DetachRolePolicy",
				"iam:TagRole",
				"iam:ListRoleTags",
			},
			Resource: []string{
				fmt.Sprintf("arn:aws:iam::%s:role/guardian*", accountID),
				fmt.Sprintf("arn:aws:iam::%s:role/cdk-hnb659fds-*", accountID),
			},
		},
		{
			Sid:    "AttachManagedPolicies",
			Effect: "Allow",
			Action: []string{"iam:AttachRolePolicy"},
			Resource: []string{
				"arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy",
				"arn:aws:iam::aws:policy/service-role/AmazonEC2ContainerServiceforEC2Role",
			},
		},
		{
			Sid:      "PassRoles",
			Effect:   "Allow",
			Action:   []string{"iam:PassRole"},
			Resource: []string{fmt.Sprintf("arn:aws:iam::%s:role/guardian*", accountID), fmt.Sprintf("arn:aws:iam::%s:role/cdk-hnb659fds-*", accountID)},
		},
		{
			Sid:      "ServiceLinkedRoles",
			Effect:   "Allow",
			Action:   []string{"iam:CreateServiceLinkedRole"},
			Resource: fmt.Sprintf("arn:aws:iam::%s:role/aws-service-role/*", accountID),
		},
		{
			Sid:      "ScanInventory",
			Effect:   "Allow",
			Action:   []string{"cloudcontrol:ListResources", "cloudcontrol:ListResourceRequests"},
			Resource: "*",
		},
		{
			Sid:    "ObservabilityRead",
			Effect: "Allow",
			Action: []string{
				"cloudwatch:Get*",
				"cloudwatch:GetMetricData",
				"cloudwatch:ListMetrics",
				"xray:Get*",
				"xray:BatchGetTraces",
			},
			Resource: "*",
		},
		{
			Sid:      "Identity",
			Effect:   "Allow",
			Action:   []string{"sts:GetCallerIdentity"},
			Resource: "*",
		},
	}
	return renderPolicy(statements)
}

// platformExecutionActions are required regardless of which templates an
// intent uses (the CDK bootstrap version parameter).
var platformExecutionActions = []string{
	// KMS grants for encrypted resources (S3, RDS, Secrets Manager, Logs, ECS)
	// are issued through the service, independent of the templates in use.
	"kms:CreateGrant",
	"kms:Decrypt",
	"kms:DescribeKey",
	"kms:GenerateDataKey",
	"kms:ListGrants",
	"kms:ListResourceTags",
	"kms:RevokeGrant",
	"kms:TagResource",
	"kms:UntagResource",
}

// commonExecutionActions cover resource families that user CDK apps and
// fallback resources commonly create but no catalog template declares. They
// belong to the account-wide bootstrap role; per-intent roles stay limited to
// the templates that intent uses.
var commonExecutionActions = []string{
	"secretsmanager:CreateSecret",
	"secretsmanager:DeleteSecret",
	"secretsmanager:DescribeSecret",
	"secretsmanager:GetRandomPassword",
	"secretsmanager:ListSecrets",
	"secretsmanager:PutSecretValue",
	"secretsmanager:TagResource",
	"secretsmanager:UntagResource",
	"secretsmanager:UpdateSecret",
	"ssm:AddTagsToResource",
	"ssm:DeleteParameter",
	"ssm:ListTagsForResource",
	"ssm:PutParameter",
	"ssm:RemoveTagsFromResource",
	"logs:CreateLogGroup",
	"logs:DeleteLogGroup",
	"logs:PutRetentionPolicy",
	"logs:TagResource",
	"logs:UntagResource",
	"sns:CreateTopic",
	"sns:DeleteTopic",
	"sns:SetTopicAttributes",
	"sns:Subscribe",
	"sns:TagResource",
	"sns:Unsubscribe",
	"sns:UntagResource",
	"sqs:CreateQueue",
	"sqs:DeleteQueue",
	"sqs:SetQueueAttributes",
	"sqs:TagQueue",
	"sqs:UntagQueue",
	"lambda:AddPermission",
	"lambda:CreateFunction",
	"lambda:DeleteFunction",
	"lambda:GetFunction",
	"lambda:ListTags",
	"lambda:RemovePermission",
	"lambda:TagResource",
	"lambda:UntagResource",
	"lambda:UpdateFunctionCode",
	"lambda:UpdateFunctionConfiguration",
}

// catalogExecutionActions is the union of every catalog template's declared
// actions plus the platform baseline. The account execution policies are
// rendered from this, so a new template automatically extends them.
func catalogExecutionActions() []string {
	actions := append([]string{}, cdkgen.PlatformExecutionActions...)
	actions = append(actions, platformExecutionActions...)
	actions = append(actions, commonExecutionActions...)
	actions = append(actions, cdkgen.CatalogActions(nil)...)
	sort.Strings(actions)
	out := make([]string, 0, len(actions))
	seen := map[string]bool{}
	for _, action := range actions {
		if seen[action] {
			continue
		}
		seen[action] = true
		out = append(out, action)
	}
	return out
}

// splitReadWriteActions splits actions into write and read sets by verb.
func splitReadWriteActions(actions []string) (write, read []string) {
	for _, action := range actions {
		idx := strings.Index(action, ":")
		if idx < 0 {
			write = append(write, action)
			continue
		}
		verb := action[idx+1:]
		switch {
		case strings.HasPrefix(verb, "Describe"), strings.HasPrefix(verb, "Get"), strings.HasPrefix(verb, "List"):
			read = append(read, action)
		default:
			write = append(write, action)
		}
	}
	return write, read
}

// ExecutionReadPolicyName is the customer-managed read-only policy attached
// alongside ExecutionPolicyName. CloudFormation drift detection and several
// resource handlers call read APIs on the execution role, and keeping reads in
// a separate document keeps both policies under the IAM size limit.
const ExecutionReadPolicyName = "GuardianCfnExecutionReadPolicy"

// ExecutionReadPolicy is the read-only companion to ExecutionPolicy: every
// Describe/Get/List action the composed templates and CloudFormation drift
// detection need.
func ExecutionReadPolicy(accountID, region string) string {
	_ = accountID
	_ = region
	_, read := splitReadWriteActions(catalogExecutionActions())
	// Drift detection and read-before-write also need the bootstrap parameter
	// and account-level reads.
	read = append(read,
		"cloudformation:DescribeStacks",
		"cloudformation:DescribeStackResources",
		"cloudformation:ListStackResources",
		"cloudformation:GetTemplate",
	)
	sort.Strings(read)
	statements := []policyStatement{
		{
			Sid:      "ReadOnly",
			Effect:   "Allow",
			Action:   read,
			Resource: "*",
		},
	}
	return renderPolicy(statements)
}

// ExecutionPolicy is the scoped policy for the CDK bootstrap CloudFormation
// execution role: it creates the resources our templates manage (S3, ECS,
// EFS, RDS, ELB, EC2 security groups, IAM task roles, logs, secrets, SSM,
// Lambda custom resources) without granting account administration.
func ExecutionPolicy(accountID, region string) string {
	write, _ := splitReadWriteActions(catalogExecutionActions())
	generic := make([]string, 0, len(write))
	s3Actions := make([]string, 0, 16)
	iamActions := make([]string, 0, 16)
	kmsActions := make([]string, 0, 8)
	for _, action := range write {
		switch {
		case strings.HasPrefix(action, "s3:"):
			s3Actions = append(s3Actions, action)
		case strings.HasPrefix(action, "iam:"):
			iamActions = append(iamActions, action)
		case strings.HasPrefix(action, "kms:"):
			kmsActions = append(kmsActions, action)
		default:
			generic = append(generic, action)
		}
	}
	sort.Strings(generic)
	sort.Strings(s3Actions)
	sort.Strings(iamActions)
	sort.Strings(kmsActions)

	statements := []policyStatement{
		// Everything the catalog templates declare (network, ECS, ELB, EFS,
		// RDS, SQS/SNS, Lambda, Logs, SSM, Secrets, ...) except the families
		// that get resource-scoped statements below.
		{
			Sid:      "TemplateResources",
			Effect:   "Allow",
			Action:   generic,
			Resource: "*",
		},
		{
			// The account-wide role also serves user CDK apps and fallback
			// resources, whose bucket names are not guardian-*; per-intent
			// roles stay limited to their templates.
			Sid:      "Buckets",
			Effect:   "Allow",
			Action:   s3Actions,
			Resource: "*",
		},
		{
			Sid:    "TaskRoles",
			Effect: "Allow",
			Action: iamActions,
			Resource: []string{
				fmt.Sprintf("arn:aws:iam::%s:role/guardian*", accountID),
				fmt.Sprintf("arn:aws:iam::%s:role/cdk-hnb659fds-*", accountID),
			},
		},
		{
			Sid:    "AttachManagedPolicies",
			Effect: "Allow",
			Action: []string{"iam:AttachRolePolicy"},
			Resource: []string{
				"arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy",
			},
		},
		{
			Sid:      "PassRoles",
			Effect:   "Allow",
			Action:   []string{"iam:PassRole"},
			Resource: []string{fmt.Sprintf("arn:aws:iam::%s:role/guardian*", accountID), fmt.Sprintf("arn:aws:iam::%s:role/cdk-hnb659fds-*", accountID)},
		},
		{
			Sid:      "ServiceLinkedRoles",
			Effect:   "Allow",
			Action:   []string{"iam:CreateServiceLinkedRole"},
			Resource: fmt.Sprintf("arn:aws:iam::%s:role/aws-service-role/*", accountID),
		},
		{
			Sid:    "CustomResources",
			Effect: "Allow",
			Action: []string{
				"lambda:CreateFunction",
				"lambda:DeleteFunction",
				"lambda:Update*",
				"lambda:AddPermission",
				"lambda:RemovePermission",
				"lambda:TagResource",
				"lambda:UntagResource",
			},
			Resource: fmt.Sprintf("arn:aws:lambda:%s:%s:function:*", region, accountID),
		},
		{
			Sid:      "KmsForEncryptedResources",
			Effect:   "Allow",
			Action:   kmsActions,
			Resource: "*",
			Condition: map[string]any{
				"StringEquals": map[string]any{
					"kms:ViaService": []string{
						fmt.Sprintf("s3.%s.amazonaws.com", region),
						fmt.Sprintf("rds.%s.amazonaws.com", region),
						fmt.Sprintf("secretsmanager.%s.amazonaws.com", region),
						fmt.Sprintf("logs.%s.amazonaws.com", region),
						fmt.Sprintf("ecs.%s.amazonaws.com", region),
					},
				},
			},
		},
	}
	return renderPolicy(statements)
}
