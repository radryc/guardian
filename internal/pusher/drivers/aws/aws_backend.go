package awsdriver

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	awsroot "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

type AWSBackend struct {
	profile       string
	defaultRegion string
	dryRun        bool

	mu   sync.Mutex
	cfgs map[string]awsroot.Config
}

func NewAWSBackend(profile, defaultRegion string, dryRun bool) *AWSBackend {
	if defaultRegion == "" {
		defaultRegion = "us-east-1"
	}
	return &AWSBackend{
		profile:       profile,
		defaultRegion: defaultRegion,
		dryRun:        dryRun,
		cfgs:          make(map[string]awsroot.Config),
	}
}

// loadConfig returns a cached config per region. The config's credentials
// provider is a shared aws.CredentialsCache, so concurrent operations
// single-flight credential resolution (SSO refresh / assume-role /
// credential_process) instead of spawning one resolution per call, which
// otherwise fails with "failed to refresh cached credentials".
func (b *AWSBackend) loadConfig(ctx context.Context, region string) (awsroot.Config, error) {
	if region == "" {
		region = b.defaultRegion
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cfgs == nil {
		b.cfgs = make(map[string]awsroot.Config)
	}
	if cfg, ok := b.cfgs[region]; ok {
		return cfg, nil
	}
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	if b.profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(b.profile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return awsroot.Config{}, err
	}
	// Ensure credentials are cached/shared even if the SDK returned a
	// non-caching provider.
	cfg.Credentials = awsroot.NewCredentialsCache(cfg.Credentials)
	b.cfgs[region] = cfg
	return cfg, nil
}

func (b *AWSBackend) dry() bool { return b.dryRun }

// --- EFS ---

func (b *AWSBackend) UpsertFileSystem(ctx context.Context, fs FileSystem) (string, error) {
	if b.dry() {
		fmt.Printf("  [dry-run] would upsert EFS: %s\n", fs.Name)
		return "fs-dry-run", nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return "", err
	}
	client := efs.NewFromConfig(cfg)

	out, err := client.CreateFileSystem(ctx, &efs.CreateFileSystemInput{
		CreationToken: awsroot.String(fs.ID),
		Encrypted:     awsroot.Bool(fs.Encrypted),
		Tags:          toEFSTags(fs.Tags),
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "FileSystemAlreadyExists") {
			desc, descErr := client.DescribeFileSystems(ctx, &efs.DescribeFileSystemsInput{
				CreationToken: awsroot.String(fs.ID),
			})
			if descErr != nil {
				return "", fmt.Errorf("EFS exists but cannot describe: %w", descErr)
			}
			if len(desc.FileSystems) == 0 {
				return "", fmt.Errorf("EFS exists but not found by creation token")
			}
			fsID := awsroot.ToString(desc.FileSystems[0].FileSystemId)
			if len(fs.Tags) > 0 {
				_, _ = client.TagResource(ctx, &efs.TagResourceInput{
					ResourceId: awsroot.String(fsID),
					Tags:       toEFSTags(fs.Tags),
				})
			}
			return fsID, nil
		}
		return "", fmt.Errorf("create EFS filesystem: %w", err)
	}
	return awsroot.ToString(out.FileSystemId), nil
}

func (b *AWSBackend) GetFileSystem(ctx context.Context, fsID string) (FileSystem, bool, error) {
	if b.dry() {
		return FileSystem{}, false, nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return FileSystem{}, false, err
	}
	client := efs.NewFromConfig(cfg)

	out, err := client.DescribeFileSystems(ctx, &efs.DescribeFileSystemsInput{
		FileSystemId: awsroot.String(fsID),
	})
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "FileSystemNotFound") {
			return FileSystem{}, false, nil
		}
		return FileSystem{}, false, err
	}
	if len(out.FileSystems) == 0 {
		return FileSystem{}, false, nil
	}
	f := out.FileSystems[0]
	tags := tagsFromEFSDescription(f.Tags)
	return FileSystem{
		ID:   awsroot.ToString(f.FileSystemId),
		Name: awsroot.ToString(f.Name),
		Tags: tags,
		Hash: tags["guardian-hash"],
	}, true, nil
}

func (b *AWSBackend) DeleteFileSystem(ctx context.Context, fsID string) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would delete EFS: %s\n", fsID)
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := efs.NewFromConfig(cfg)
	_, err = client.DeleteFileSystem(ctx, &efs.DeleteFileSystemInput{
		FileSystemId: awsroot.String(fsID),
	})
	return err
}

// --- SSM Parameter Store ---

func (b *AWSBackend) UpsertParameter(ctx context.Context, param Parameter) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would put SSM parameter: %s\n", param.Name)
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := ssm.NewFromConfig(cfg)

	paramType := ssmtypes.ParameterTypeString
	if param.Type == "SecureString" {
		paramType = ssmtypes.ParameterTypeSecureString
	}

	// AWS rejects Tags together with Overwrite, so always overwrite the value
	// without tags and apply tags separately (AddTagsToResource).
	_, err = client.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      awsroot.String(param.Name),
		Value:     awsroot.String(param.Value),
		Type:      paramType,
		Overwrite: awsroot.Bool(true),
	})
	if err != nil {
		return err
	}
	if len(param.Tags) > 0 {
		if _, tagErr := client.AddTagsToResource(ctx, &ssm.AddTagsToResourceInput{
			ResourceType: ssmtypes.ResourceTypeForTaggingParameter,
			ResourceId:   awsroot.String(param.Name),
			Tags:         toSSMTags(param.Tags),
		}); tagErr != nil {
			// Non-fatal: the parameter value is already correct; tagging can be
			// retried on the next apply.
			fmt.Printf("  warn: tag SSM parameter %s: %v\n", param.Name, tagErr)
		}
	}
	return nil
}

func (b *AWSBackend) GetParameter(ctx context.Context, name string) (Parameter, bool, error) {
	if b.dry() {
		return Parameter{}, false, nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return Parameter{}, false, err
	}
	client := ssm.NewFromConfig(cfg)

	out, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name: awsroot.String(name),
	})
	if err != nil {
		if strings.Contains(err.Error(), "ParameterNotFound") {
			return Parameter{}, false, nil
		}
		return Parameter{}, false, err
	}
	tags := map[string]string{}
	if tagsOut, tagsErr := client.ListTagsForResource(ctx, &ssm.ListTagsForResourceInput{
		ResourceType: ssmtypes.ResourceTypeForTaggingParameter,
		ResourceId:   awsroot.String(name),
	}); tagsErr == nil {
		for _, tag := range tagsOut.TagList {
			tags[awsroot.ToString(tag.Key)] = awsroot.ToString(tag.Value)
		}
	}
	return Parameter{
		Name:  awsroot.ToString(out.Parameter.Name),
		Value: awsroot.ToString(out.Parameter.Value),
		Type:  string(out.Parameter.Type),
		Hash:  tags["guardian-hash"],
		Tags:  tags,
	}, true, nil
}

func (b *AWSBackend) DeleteParameter(ctx context.Context, name string) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would delete SSM parameter: %s\n", name)
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := ssm.NewFromConfig(cfg)
	_, err = client.DeleteParameter(ctx, &ssm.DeleteParameterInput{
		Name: awsroot.String(name),
	})
	return err
}

// --- Secrets Manager ---

func (b *AWSBackend) UpsertSecret(ctx context.Context, secret Secret) (string, error) {
	if b.dry() {
		fmt.Printf("  [dry-run] would upsert Secrets Manager secret: %s\n", secret.Name)
		return "arn:aws:secretsmanager:region:account:secret:dry-run", nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return "", err
	}
	client := secretsmanager.NewFromConfig(cfg)

	out, err := client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
		Name:         awsroot.String(secret.Name),
		SecretString: awsroot.String(secret.Value),
		Tags:         toSecretsManagerTags(secret.Tags),
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "ResourceExistsException") {
			_, upErr := client.UpdateSecret(ctx, &secretsmanager.UpdateSecretInput{
				SecretId:     awsroot.String(secret.Name),
				SecretString: awsroot.String(secret.Value),
			})
			if upErr != nil {
				return "", fmt.Errorf("update secret: %w", upErr)
			}
			desc, _ := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
				SecretId: awsroot.String(secret.Name),
			})
			if desc != nil {
				return awsroot.ToString(desc.ARN), nil
			}
			return secret.Name, nil
		}
		return "", fmt.Errorf("create secret: %w", err)
	}
	return awsroot.ToString(out.ARN), nil
}

func (b *AWSBackend) GetSecret(ctx context.Context, secretID string) (Secret, bool, error) {
	if b.dry() {
		return Secret{}, false, nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return Secret{}, false, err
	}
	client := secretsmanager.NewFromConfig(cfg)

	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: awsroot.String(secretID),
	})
	if err != nil {
		if strings.Contains(err.Error(), "ResourceNotFoundException") {
			return Secret{}, false, nil
		}
		return Secret{}, false, err
	}
	return Secret{
		ID:    awsroot.ToString(out.ARN),
		Name:  awsroot.ToString(out.Name),
		Value: awsroot.ToString(out.SecretString),
	}, true, nil
}

func (b *AWSBackend) DeleteSecret(ctx context.Context, secretID string) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would delete secret: %s\n", secretID)
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := secretsmanager.NewFromConfig(cfg)
	_, err = client.DeleteSecret(ctx, &secretsmanager.DeleteSecretInput{
		SecretId:                   awsroot.String(secretID),
		ForceDeleteWithoutRecovery: awsroot.Bool(true),
	})
	return err
}

// --- ECS ---

func (b *AWSBackend) UpsertService(ctx context.Context, svc ECSService) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would upsert ECS service: %s/%s (family: %s, image: %s)\n",
			svc.Cluster, svc.Name, svc.TaskFamily, svc.Container.Image)
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := ecs.NewFromConfig(cfg)

	if err := b.ensureCluster(ctx, client, svc); err != nil {
		return fmt.Errorf("ensure ECS cluster: %w", err)
	}

	taskDef, err := b.upsertTaskDefinition(ctx, client, svc, cfg.Region)
	if err != nil {
		return fmt.Errorf("register task definition: %w", err)
	}

	return b.upsertECSService(ctx, client, svc, taskDef)
}

// ensureCluster creates the ECS cluster used by the service if it does not
// already exist. The Compute driver defaults the cluster name to the target
// account id, so on a fresh account the cluster must be provisioned before a
// service can be placed.
func (b *AWSBackend) ensureCluster(ctx context.Context, client *ecs.Client, svc ECSService) error {
	cluster := strings.TrimSpace(svc.Cluster)
	if cluster == "" {
		return nil
	}
	out, err := client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
		Clusters: []string{cluster},
	})
	if err != nil {
		if isAWSNotFound(err) {
			return b.createCluster(ctx, client, cluster, svc.Tags)
		}
		return err
	}
	for _, c := range out.Clusters {
		if awsroot.ToString(c.Status) == "ACTIVE" {
			return nil
		}
	}
	return b.createCluster(ctx, client, cluster, svc.Tags)
}

func (b *AWSBackend) createCluster(ctx context.Context, client *ecs.Client, cluster string, tags map[string]string) error {
	_, err := client.CreateCluster(ctx, &ecs.CreateClusterInput{
		ClusterName: awsroot.String(cluster),
		Tags:        toECSTags(tags),
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}

func (b *AWSBackend) upsertTaskDefinition(ctx context.Context, client *ecs.Client, svc ECSService, region string) (string, error) {
	container := ecstypes.ContainerDefinition{
		Name:         awsroot.String(svc.Container.Name),
		Image:        awsroot.String(svc.Container.Image),
		PortMappings: toECSPortMappings(svc.Container.Ports),
		LogConfiguration: &ecstypes.LogConfiguration{
			LogDriver: ecstypes.LogDriverAwslogs,
			Options: map[string]string{
				"awslogs-group":         svc.LogGroup,
				"awslogs-region":        region,
				"awslogs-stream-prefix": svc.Container.Name,
			},
		},
	}

	if len(svc.Container.Command) > 0 {
		container.Command = svc.Container.Command
	}
	if len(svc.Container.Env) > 0 {
		container.Environment = toECSEnv(svc.Container.Env)
	}
	if len(svc.Container.Secrets) > 0 {
		container.Secrets = toECSSecrets(svc.Container.Secrets)
	}
	if svc.Container.CPU > 0 {
		container.Cpu = int32(svc.Container.CPU)
	}
	if svc.Container.Memory > 0 {
		container.Memory = awsroot.Int32(int32(svc.Container.Memory))
	}

	taskCPU := fmt.Sprintf("%d", svc.Container.CPU)
	taskMem := fmt.Sprintf("%d", svc.Container.Memory)
	if svc.Container.CPU == 0 {
		taskCPU = "256"
	}
	if svc.Container.Memory == 0 {
		taskMem = "512"
	}

	// ECS requires an execution role when container secrets are present. Use the
	// asset-provided role when set, otherwise provision/reuse a default one.
	execRole := svc.ExecRole
	if execRole == "" && len(svc.Container.Secrets) > 0 {
		role, rerr := b.ensureExecutionRole(ctx)
		if rerr != nil {
			return "", fmt.Errorf("ensure ECS execution role: %w", rerr)
		}
		execRole = role
	}

	out, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:                  awsroot.String(svc.TaskFamily),
		ContainerDefinitions:    []ecstypes.ContainerDefinition{container},
		TaskRoleArn:             awsroot.String(svc.TaskRole),
		ExecutionRoleArn:        awsroot.String(execRole),
		NetworkMode:             ecstypes.NetworkModeAwsvpc,
		RequiresCompatibilities: toLaunchTypes(svc.LaunchType),
		Cpu:                     awsroot.String(taskCPU),
		Memory:                  awsroot.String(taskMem),
	})
	if err != nil {
		return "", err
	}
	return awsroot.ToString(out.TaskDefinition.TaskDefinitionArn), nil
}

// ensureExecutionRole returns the ARN of a shared ECS task execution role,
// creating it (with the managed ECS execution policy plus secrets read) on
// first use.
func (b *AWSBackend) ensureExecutionRole(ctx context.Context) (string, error) {
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return "", err
	}
	client := iam.NewFromConfig(cfg)
	const roleName = "guardian-ecs-execution-role"

	if out, gerr := client.GetRole(ctx, &iam.GetRoleInput{RoleName: awsroot.String(roleName)}); gerr == nil {
		return awsroot.ToString(out.Role.Arn), nil
	} else if !strings.Contains(gerr.Error(), "NoSuchEntity") {
		return "", gerr
	}

	trust := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ecs-tasks.amazonaws.com"},"Action":"sts:AssumeRole"}]}`
	created, cerr := client.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 awsroot.String(roleName),
		AssumeRolePolicyDocument: awsroot.String(trust),
	})
	if cerr != nil {
		if out, gerr := client.GetRole(ctx, &iam.GetRoleInput{RoleName: awsroot.String(roleName)}); gerr == nil {
			return awsroot.ToString(out.Role.Arn), nil
		}
		return "", cerr
	}
	_, _ = client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  awsroot.String(roleName),
		PolicyArn: awsroot.String("arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"),
	})
	_, _ = client.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
		RoleName:       awsroot.String(roleName),
		PolicyName:     awsroot.String("guardian-secrets-read"),
		PolicyDocument: awsroot.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["secretsmanager:GetSecretValue","ssm:GetParameters","kms:Decrypt"],"Resource":"*"}]}`),
	})
	return awsroot.ToString(created.Role.Arn), nil
}

// discoverVPCID returns the default VPC id, or the first VPC in the account
// when no default exists. Target groups must be created inside a VPC.
func (b *AWSBackend) discoverVPCID(ctx context.Context, cfg awsroot.Config) (string, error) {
	client := ec2.NewFromConfig(cfg)
	out, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return "", err
	}
	if len(out.Vpcs) == 0 {
		return "", fmt.Errorf("no VPC found; create a default VPC or specify subnets")
	}
	for _, v := range out.Vpcs {
		if v.IsDefault != nil && *v.IsDefault {
			return awsroot.ToString(v.VpcId), nil
		}
	}
	return awsroot.ToString(out.Vpcs[0].VpcId), nil
}

// discoverSubnets returns subnet IDs to place resources in. It prefers the
// default VPC's subnets and falls back to the first VPC found.
func (b *AWSBackend) discoverSubnets(ctx context.Context, cfg awsroot.Config) ([]string, error) {
	client := ec2.NewFromConfig(cfg)
	out, err := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{Name: awsroot.String("default-for-az"), Values: []string{"true"}},
		},
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Subnets))
	for _, s := range out.Subnets {
		ids = append(ids, awsroot.ToString(s.SubnetId))
	}
	if len(ids) > 0 {
		return ids, nil
	}

	vpcs, verr := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if verr != nil {
		return nil, verr
	}
	if len(vpcs.Vpcs) == 0 {
		return nil, fmt.Errorf("no VPC found; create a default VPC or specify subnets")
	}
	vpcID := awsroot.ToString(vpcs.Vpcs[0].VpcId)
	subs, serr := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{{Name: awsroot.String("vpc-id"), Values: []string{vpcID}}},
	})
	if serr != nil {
		return nil, serr
	}
	for _, s := range subs.Subnets {
		ids = append(ids, awsroot.ToString(s.SubnetId))
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no subnets found in account")
	}
	return ids, nil
}

func (b *AWSBackend) upsertECSService(ctx context.Context, client *ecs.Client, svc ECSService, taskDefARN string) error {
	subnets := svc.Subnets
	assignPublic := svc.AssignPublicIP
	if len(subnets) == 0 {
		cfg, cerr := b.loadConfig(ctx, "")
		if cerr != nil {
			return cerr
		}
		discovered, derr := b.discoverSubnets(ctx, cfg)
		if derr != nil {
			return fmt.Errorf("discover subnets: %w", derr)
		}
		subnets = discovered
		// Default VPC subnets are public; Fargate needs a public IP to pull images.
		assignPublic = true
	}

	netConf := &ecstypes.NetworkConfiguration{
		AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
			Subnets:        subnets,
			SecurityGroups: svc.SecurityGroups,
			AssignPublicIp: ecstypes.AssignPublicIpDisabled,
		},
	}
	if assignPublic {
		netConf.AwsvpcConfiguration.AssignPublicIp = ecstypes.AssignPublicIpEnabled
	}

	launchType := ecstypes.LaunchTypeFargate
	if svc.LaunchType == "EC2" {
		launchType = ecstypes.LaunchTypeEc2
	}

	createIn := &ecs.CreateServiceInput{
		Cluster:              awsroot.String(svc.Cluster),
		ServiceName:          awsroot.String(svc.Name),
		TaskDefinition:       awsroot.String(taskDefARN),
		DesiredCount:         awsroot.Int32(int32(svc.DesiredCount)),
		LaunchType:           launchType,
		NetworkConfiguration: netConf,
		Tags:                 toECSTags(svc.Tags),
	}

	if svc.TargetGroupARN != "" {
		createIn.LoadBalancers = []ecstypes.LoadBalancer{
			{
				TargetGroupArn: awsroot.String(svc.TargetGroupARN),
				ContainerName:  awsroot.String(svc.Container.Name),
				ContainerPort:  awsroot.Int32(int32(portOrDefault(svc.Container.Ports))),
			},
		}
	}

	// CreateService is only idempotent when EVERY parameter matches an existing
	// service. On any difference ECS returns "Creation of service was not
	// idempotent" (not "already exists"), so probe first and update in place.
	existing, derr := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  awsroot.String(svc.Cluster),
		Services: []string{svc.Name},
	})
	if derr != nil && !isAWSNotFound(derr) {
		return derr
	}
	if derr == nil && len(existing.Services) > 0 && awsroot.ToString(existing.Services[0].Status) == "ACTIVE" {
		_, upErr := client.UpdateService(ctx, &ecs.UpdateServiceInput{
			Cluster:              awsroot.String(svc.Cluster),
			Service:              awsroot.String(svc.Name),
			TaskDefinition:       awsroot.String(taskDefARN),
			DesiredCount:         awsroot.Int32(int32(svc.DesiredCount)),
			NetworkConfiguration: netConf,
			ForceNewDeployment:   true,
		})
		return upErr
	}

	_, err := client.CreateService(ctx, createIn)
	if err != nil {
		return fmt.Errorf("create ECS service: %w", err)
	}
	return nil
}

func (b *AWSBackend) GetService(ctx context.Context, cluster, name string) (ECSService, bool, error) {
	if b.dry() {
		return ECSService{}, false, nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return ECSService{}, false, err
	}
	client := ecs.NewFromConfig(cfg)

	out, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  awsroot.String(cluster),
		Services: []string{name},
	})
	if err != nil {
		if isAWSNotFound(err) {
			return ECSService{}, false, nil
		}
		return ECSService{}, false, err
	}
	if len(out.Services) == 0 || out.Services[0].Status == nil || *out.Services[0].Status == "INACTIVE" {
		return ECSService{}, false, nil
	}
	svc := out.Services[0]
	service := ECSService{
		Name:         awsroot.ToString(svc.ServiceName),
		Cluster:      awsroot.ToString(svc.ClusterArn),
		DesiredCount: int(svc.DesiredCount),
		TaskFamily:   taskFamilyFromDefinitionARN(awsroot.ToString(svc.TaskDefinition)),
	}
	if serviceArn := awsroot.ToString(svc.ServiceArn); serviceArn != "" {
		if tagsOut, tagsErr := client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
			ResourceArn: awsroot.String(serviceArn),
		}); tagsErr == nil {
			service.Tags = tagsFromECSTags(tagsOut.Tags)
			service.Hash = service.Tags["guardian-hash"]
		}
	}
	return service, true, nil
}

func taskFamilyFromDefinitionARN(arn string) string {
	if arn == "" {
		return ""
	}
	idx := strings.LastIndex(arn, "/")
	if idx < 0 {
		return ""
	}
	rest := arn[idx+1:]
	if rev := strings.LastIndex(rest, ":"); rev >= 0 {
		return rest[:rev]
	}
	return rest
}

func (b *AWSBackend) DeleteService(ctx context.Context, cluster, name string) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would delete ECS service: %s/%s\n", cluster, name)
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := ecs.NewFromConfig(cfg)

	_, err = client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:      awsroot.String(cluster),
		Service:      awsroot.String(name),
		DesiredCount: awsroot.Int32(0),
	})
	if err != nil {
		if isAWSNotFound(err) {
			return nil
		}
		return err
	}
	_, err = client.DeleteService(ctx, &ecs.DeleteServiceInput{
		Cluster: awsroot.String(cluster),
		Service: awsroot.String(name),
		Force:   awsroot.Bool(true),
	})
	if isAWSNotFound(err) {
		return nil
	}
	return err
}

// AttachServiceToTargetGroup registers an ECS service with an ELBv2 target
// group. Fargate tasks are registered by ENI IP, so the target group must use
// the "ip" target type.
func (b *AWSBackend) AttachServiceToTargetGroup(ctx context.Context, cluster, service, container, tgARN string, port int, sgID string) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would attach ECS service %s/%s to target group %s\n", cluster, service, tgARN)
		return nil
	}
	if strings.TrimSpace(tgARN) == "" {
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := ecs.NewFromConfig(cfg)
	updateIn := &ecs.UpdateServiceInput{
		Cluster: awsroot.String(cluster),
		Service: awsroot.String(service),
		LoadBalancers: []ecstypes.LoadBalancer{{
			TargetGroupArn: awsroot.String(tgARN),
			ContainerName:  awsroot.String(container),
			ContainerPort:  awsroot.Int32(int32(port)),
		}},
		ForceNewDeployment: true,
	}
	// Place the tasks in the load balancer's security group so the LB's
	// self-referencing ingress rule authorizes health checks and traffic.
	if strings.TrimSpace(sgID) != "" {
		if subnets, derr := b.discoverSubnets(ctx, cfg); derr == nil && len(subnets) > 0 {
			updateIn.NetworkConfiguration = &ecstypes.NetworkConfiguration{
				AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
					Subnets:        subnets,
					SecurityGroups: []string{sgID},
					AssignPublicIp: ecstypes.AssignPublicIpEnabled,
				},
			}
		}
	}
	_, err = client.UpdateService(ctx, updateIn)
	return err
}

// --- ELBv2 (ALB/NLB) ---

func (b *AWSBackend) UpsertLoadBalancer(ctx context.Context, lb LoadBalancer) (string, error) {
	if b.dry() {
		fmt.Printf("  [dry-run] would create %s LB: %s\n", lb.Type, lb.Name)
		return "arn:aws:elasticloadbalancing:region:account:loadbalancer/dry-run", nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return "", err
	}
	client := elasticloadbalancingv2.NewFromConfig(cfg)

	lbType := elbv2types.LoadBalancerTypeEnumApplication
	if lb.Type == "network" {
		lbType = elbv2types.LoadBalancerTypeEnumNetwork
	}

	scheme := elbv2types.LoadBalancerSchemeEnumInternetFacing
	if lb.Scheme == "internal" {
		scheme = elbv2types.LoadBalancerSchemeEnumInternal
	}

	lbSubnets := lb.Subnets
	if len(lbSubnets) == 0 {
		discovered, derr := b.discoverSubnets(ctx, cfg)
		if derr != nil {
			return "", fmt.Errorf("discover subnets: %w", derr)
		}
		lbSubnets = discovered
	}

	if existing, ok, gerr := b.GetLoadBalancer(ctx, lb.Name); gerr != nil {
		return "", gerr
	} else if ok {
		if len(lb.SecurityGroups) > 0 {
			if _, serr := client.SetSecurityGroups(ctx, &elasticloadbalancingv2.SetSecurityGroupsInput{
				LoadBalancerArn: awsroot.String(existing.ARN),
				SecurityGroups:  lb.SecurityGroups,
			}); serr != nil {
				return "", fmt.Errorf("set load balancer security groups: %w", serr)
			}
		}
		return existing.ARN, nil
	}

	out, err := client.CreateLoadBalancer(ctx, &elasticloadbalancingv2.CreateLoadBalancerInput{
		Name:           awsroot.String(lb.Name),
		Type:           lbType,
		Scheme:         scheme,
		Subnets:        lbSubnets,
		SecurityGroups: lb.SecurityGroups,
		Tags:           toELBv2Tags(lb.Tags),
	})
	if err != nil {
		if strings.Contains(err.Error(), "DuplicateLoadBalancerName") {
			if existing, ok, gerr := b.GetLoadBalancer(ctx, lb.Name); gerr == nil && ok {
				return existing.ARN, nil
			}
		}
		return "", fmt.Errorf("create load balancer: %w", err)
	}
	if len(out.LoadBalancers) == 0 {
		return "", fmt.Errorf("create load balancer: no load balancer returned")
	}
	return awsroot.ToString(out.LoadBalancers[0].LoadBalancerArn), nil
}

// EnsureLoadBalancerSecurityGroup creates (or reuses) a security group for a
// load balancer, allowing inbound traffic on the listener ports plus
// self-referencing traffic so the LB can reach targets that share the group.
func (b *AWSBackend) EnsureLoadBalancerSecurityGroup(ctx context.Context, name, scheme string, ports []int) (string, error) {
	if b.dry() {
		return "sg-dry-run", nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return "", err
	}
	client := ec2.NewFromConfig(cfg)

	vpcID, err := b.discoverVPCID(ctx, cfg)
	if err != nil {
		return "", err
	}

	sgName := strings.TrimSpace(name)
	if sgName == "" {
		sgName = "guardian-lb"
	}
	describe := func() (string, bool) {
		out, derr := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
			Filters: []ec2types.Filter{
				{Name: awsroot.String("group-name"), Values: []string{sgName}},
				{Name: awsroot.String("vpc-id"), Values: []string{vpcID}},
			},
		})
		if derr != nil || len(out.SecurityGroups) == 0 {
			return "", false
		}
		return awsroot.ToString(out.SecurityGroups[0].GroupId), true
	}
	if id, ok := describe(); ok {
		return id, nil
	}

	created, err := client.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
		GroupName:   awsroot.String(sgName),
		Description: awsroot.String("Guardian managed load balancer"),
		VpcId:       awsroot.String(vpcID),
		TagSpecifications: []ec2types.TagSpecification{{
			ResourceType: ec2types.ResourceTypeSecurityGroup,
			Tags: []ec2types.Tag{
				{Key: awsroot.String("guardian-managed"), Value: awsroot.String("true")},
			},
		}},
	})
	if err != nil {
		if id, ok := describe(); ok {
			return id, nil
		}
		return "", err
	}
	sgID := awsroot.ToString(created.GroupId)

	cidr := "0.0.0.0/0"
	if scheme == "internal" {
		vpcs, verr := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{VpcIds: []string{vpcID}})
		if verr == nil && len(vpcs.Vpcs) > 0 {
			cidr = awsroot.ToString(vpcs.Vpcs[0].CidrBlock)
		}
	}
	perms := []ec2types.IpPermission{}
	seen := map[int]struct{}{}
	for _, port := range ports {
		if port <= 0 {
			continue
		}
		if _, ok := seen[port]; ok {
			continue
		}
		seen[port] = struct{}{}
		perms = append(perms, ec2types.IpPermission{
			IpProtocol: awsroot.String("tcp"),
			FromPort:   awsroot.Int32(int32(port)),
			ToPort:     awsroot.Int32(int32(port)),
			IpRanges:   []ec2types.IpRange{{CidrIp: awsroot.String(cidr)}},
		})
	}
	// Self-reference so the LB can forward to targets that reuse this group.
	perms = append(perms, ec2types.IpPermission{
		IpProtocol: awsroot.String("-1"),
		UserIdGroupPairs: []ec2types.UserIdGroupPair{{
			GroupId: awsroot.String(sgID),
		}},
	})
	if len(perms) > 0 {
		if _, aerr := client.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
			GroupId:       awsroot.String(sgID),
			IpPermissions: perms,
		}); aerr != nil && !strings.Contains(aerr.Error(), "InvalidPermission.Duplicate") {
			return "", aerr
		}
	}
	return sgID, nil
}

func (b *AWSBackend) GetLoadBalancer(ctx context.Context, name string) (LoadBalancer, bool, error) {
	if b.dry() {
		return LoadBalancer{}, false, nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return LoadBalancer{}, false, err
	}
	client := elasticloadbalancingv2.NewFromConfig(cfg)

	out, err := client.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{
		Names: []string{name},
	})
	if err != nil {
		if isAWSNotFound(err) {
			return LoadBalancer{}, false, nil
		}
		return LoadBalancer{}, false, err
	}
	if len(out.LoadBalancers) == 0 {
		return LoadBalancer{}, false, nil
	}
	lb := out.LoadBalancers[0]
	result := LoadBalancer{
		ARN:     awsroot.ToString(lb.LoadBalancerArn),
		Name:    awsroot.ToString(lb.LoadBalancerName),
		DNSName: awsroot.ToString(lb.DNSName),
		Type:    string(lb.Type),
		Scheme:  string(lb.Scheme),
	}
	if result.ARN != "" {
		if tagsOut, tagsErr := client.DescribeTags(ctx, &elasticloadbalancingv2.DescribeTagsInput{
			ResourceArns: []string{result.ARN},
		}); tagsErr == nil {
			for _, desc := range tagsOut.TagDescriptions {
				if awsroot.ToString(desc.ResourceArn) != result.ARN {
					continue
				}
				result.Tags = tagsFromELBv2(desc.Tags)
				result.Hash = result.Tags["guardian-hash"]
			}
		}
	}
	return result, true, nil
}

func (b *AWSBackend) DeleteLoadBalancer(ctx context.Context, arn string) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would delete LB: %s\n", arn)
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := elasticloadbalancingv2.NewFromConfig(cfg)

	_, err = client.DeleteLoadBalancer(ctx, &elasticloadbalancingv2.DeleteLoadBalancerInput{
		LoadBalancerArn: awsroot.String(arn),
	})
	if isAWSNotFound(err) {
		return nil
	}
	return err
}

func (b *AWSBackend) UpsertTargetGroup(ctx context.Context, tg TargetGroup) (string, error) {
	if b.dry() {
		fmt.Printf("  [dry-run] would create target group: %s (port %d)\n", tg.Name, tg.Port)
		return "arn:aws:elasticloadbalancing:region:account:targetgroup/dry-run", nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return "", err
	}
	client := elasticloadbalancingv2.NewFromConfig(cfg)

	proto := elbv2types.ProtocolEnumHttp
	if tg.Protocol == "HTTPS" {
		proto = elbv2types.ProtocolEnumHttps
	} else if tg.Protocol == "TCP" {
		proto = elbv2types.ProtocolEnumTcp
	} else if tg.Protocol == "GRPC" {
		proto = elbv2types.ProtocolEnumHttp
	}

	targetType := elbv2types.TargetTypeEnumIp
	if tg.TargetType == "instance" {
		targetType = elbv2types.TargetTypeEnumInstance
	}

	healthPath := tg.HealthPath
	if healthPath == "" {
		healthPath = "/"
	}

	if existing, ok, gerr := b.GetTargetGroup(ctx, tg.Name); gerr != nil {
		return "", gerr
	} else if ok {
		return existing.ARN, nil
	}

	vpcID := strings.TrimSpace(tg.VPCID)
	if vpcID == "" {
		discovered, verr := b.discoverVPCID(ctx, cfg)
		if verr != nil {
			return "", fmt.Errorf("discover VPC for target group: %w", verr)
		}
		vpcID = discovered
	}

	healthPort := tg.HealthPort
	if healthPort == "" {
		healthPort = "traffic-port"
	}

	out, err := client.CreateTargetGroup(ctx, &elasticloadbalancingv2.CreateTargetGroupInput{
		Name:                awsroot.String(tg.Name),
		Port:                awsroot.Int32(int32(tg.Port)),
		Protocol:            proto,
		VpcId:               awsroot.String(vpcID),
		TargetType:          targetType,
		HealthCheckPath:     awsroot.String(healthPath),
		HealthCheckProtocol: toELBv2HealthProto(tg.HealthProto),
		HealthCheckPort:     awsroot.String(healthPort),
		Tags:                toELBv2Tags(tg.Tags),
	})
	if err != nil {
		if strings.Contains(err.Error(), "DuplicateTargetGroupName") {
			if existing, ok, gerr := b.GetTargetGroup(ctx, tg.Name); gerr == nil && ok {
				return existing.ARN, nil
			}
		}
		return "", fmt.Errorf("create target group: %w", err)
	}
	if len(out.TargetGroups) == 0 {
		return "", fmt.Errorf("create target group: no target group returned")
	}
	return awsroot.ToString(out.TargetGroups[0].TargetGroupArn), nil
}

func (b *AWSBackend) GetTargetGroup(ctx context.Context, name string) (TargetGroup, bool, error) {
	if b.dry() {
		return TargetGroup{}, false, nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return TargetGroup{}, false, err
	}
	client := elasticloadbalancingv2.NewFromConfig(cfg)

	out, err := client.DescribeTargetGroups(ctx, &elasticloadbalancingv2.DescribeTargetGroupsInput{
		Names: []string{name},
	})
	if err != nil {
		if isAWSNotFound(err) {
			return TargetGroup{}, false, nil
		}
		return TargetGroup{}, false, err
	}
	if len(out.TargetGroups) == 0 {
		return TargetGroup{}, false, nil
	}
	tg := out.TargetGroups[0]
	return TargetGroup{
		ARN:      awsroot.ToString(tg.TargetGroupArn),
		Name:     awsroot.ToString(tg.TargetGroupName),
		Port:     int(awsroot.ToInt32(tg.Port)),
		Protocol: string(tg.Protocol),
	}, true, nil
}

func (b *AWSBackend) DeleteTargetGroup(ctx context.Context, arn string) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would delete target group: %s\n", arn)
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := elasticloadbalancingv2.NewFromConfig(cfg)

	_, err = client.DeleteTargetGroup(ctx, &elasticloadbalancingv2.DeleteTargetGroupInput{
		TargetGroupArn: awsroot.String(arn),
	})
	if isAWSNotFound(err) {
		return nil
	}
	return err
}

func (b *AWSBackend) UpsertListener(ctx context.Context, listener Listener) (string, error) {
	if b.dry() {
		fmt.Printf("  [dry-run] would create listener on %s port %d\n", listener.LoadBalancerARN, listener.Port)
		return "arn:aws:elasticloadbalancing:region:account:listener/dry-run", nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return "", err
	}
	client := elasticloadbalancingv2.NewFromConfig(cfg)

	proto := elbv2types.ProtocolEnumHttp
	if listener.Protocol == "HTTPS" {
		proto = elbv2types.ProtocolEnumHttps
	} else if listener.Protocol == "TCP" {
		proto = elbv2types.ProtocolEnumTcp
	}

	defaultActions := []elbv2types.Action{
		{
			Type: elbv2types.ActionTypeEnumForward,
			ForwardConfig: &elbv2types.ForwardActionConfig{
				TargetGroups: []elbv2types.TargetGroupTuple{
					{TargetGroupArn: awsroot.String(listener.DefaultActionARN)},
				},
			},
		},
	}

	if existing, ok, gerr := b.GetListener(ctx, listener.LoadBalancerARN, listener.Port); gerr != nil {
		return "", gerr
	} else if ok {
		_, uerr := client.ModifyListener(ctx, &elasticloadbalancingv2.ModifyListenerInput{
			ListenerArn:    awsroot.String(existing.ARN),
			Port:           awsroot.Int32(int32(listener.Port)),
			Protocol:       proto,
			DefaultActions: defaultActions,
		})
		if uerr != nil {
			return "", fmt.Errorf("update listener: %w", uerr)
		}
		return existing.ARN, nil
	}

	out, err := client.CreateListener(ctx, &elasticloadbalancingv2.CreateListenerInput{
		LoadBalancerArn: awsroot.String(listener.LoadBalancerARN),
		Port:            awsroot.Int32(int32(listener.Port)),
		Protocol:        proto,
		DefaultActions:  defaultActions,
		Tags:            toELBv2Tags(listener.Tags),
	})
	if err != nil {
		if strings.Contains(err.Error(), "DuplicateListener") {
			if existing, ok, gerr := b.GetListener(ctx, listener.LoadBalancerARN, listener.Port); gerr == nil && ok {
				return existing.ARN, nil
			}
		}
		return "", fmt.Errorf("create listener: %w", err)
	}
	if len(out.Listeners) == 0 {
		return "", fmt.Errorf("create listener: no listener returned")
	}
	return awsroot.ToString(out.Listeners[0].ListenerArn), nil
}

func (b *AWSBackend) GetListener(ctx context.Context, lbARN string, port int) (Listener, bool, error) {
	if b.dry() {
		return Listener{}, false, nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return Listener{}, false, err
	}
	client := elasticloadbalancingv2.NewFromConfig(cfg)

	out, err := client.DescribeListeners(ctx, &elasticloadbalancingv2.DescribeListenersInput{
		LoadBalancerArn: awsroot.String(lbARN),
	})
	if err != nil {
		return Listener{}, false, err
	}
	for _, l := range out.Listeners {
		if int(awsroot.ToInt32(l.Port)) == port {
			return Listener{
				ARN:             awsroot.ToString(l.ListenerArn),
				LoadBalancerARN: awsroot.ToString(l.LoadBalancerArn),
				Port:            int(awsroot.ToInt32(l.Port)),
				Protocol:        string(l.Protocol),
			}, true, nil
		}
	}
	return Listener{}, false, nil
}

func (b *AWSBackend) DeleteListener(ctx context.Context, arn string) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would delete listener: %s\n", arn)
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := elasticloadbalancingv2.NewFromConfig(cfg)

	_, err = client.DeleteListener(ctx, &elasticloadbalancingv2.DeleteListenerInput{
		ListenerArn: awsroot.String(arn),
	})
	if isAWSNotFound(err) {
		return nil
	}
	return err
}

// --- S3 ---

func (b *AWSBackend) UpsertBucket(ctx context.Context, bucket BucketSpec) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would create S3 bucket: %s\n", bucket.Name)
		return nil
	}
	cfg, err := b.loadConfig(ctx, bucket.Region)
	if err != nil {
		return err
	}
	client := s3.NewFromConfig(cfg)

	createIn := &s3.CreateBucketInput{
		Bucket: awsroot.String(bucket.Name),
	}
	if bucket.Region != "" && bucket.Region != "us-east-1" {
		createIn.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(bucket.Region),
		}
	}

	_, err = client.CreateBucket(ctx, createIn)
	if err != nil {
		if !strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("create bucket: %w", err)
		}
	}

	if bucket.Versioning {
		if _, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
			Bucket: awsroot.String(bucket.Name),
			VersioningConfiguration: &s3types.VersioningConfiguration{
				Status: s3types.BucketVersioningStatusEnabled,
			},
		}); err != nil {
			return fmt.Errorf("enable bucket versioning: %w", err)
		}
	}

	if len(bucket.Tags) > 0 {
		existing := map[string]string{}
		if tagsOut, tagsErr := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
			Bucket: awsroot.String(bucket.Name),
		}); tagsErr == nil {
			for _, tag := range tagsOut.TagSet {
				existing[awsroot.ToString(tag.Key)] = awsroot.ToString(tag.Value)
			}
		}
		merged := make(map[string]string, len(existing)+len(bucket.Tags))
		for key, value := range existing {
			merged[key] = value
		}
		for key, value := range bucket.Tags {
			merged[key] = value
		}
		if _, err := client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
			Bucket: awsroot.String(bucket.Name),
			Tagging: &s3types.Tagging{
				TagSet: toS3Tags(merged),
			},
		}); err != nil {
			return fmt.Errorf("tag bucket: %w", err)
		}
	}
	return nil
}

func (b *AWSBackend) GetBucket(ctx context.Context, name string) (BucketSpec, bool, error) {
	if b.dry() {
		return BucketSpec{}, false, nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return BucketSpec{}, false, err
	}
	client := s3.NewFromConfig(cfg)

	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: awsroot.String(name),
	})
	if err != nil {
		if isAWSNotFound(err) {
			return BucketSpec{}, false, nil
		}
		return BucketSpec{}, false, err
	}
	result := BucketSpec{Name: name}
	tags := map[string]string{}
	if tagsOut, tagsErr := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
		Bucket: awsroot.String(name),
	}); tagsErr == nil {
		for _, tag := range tagsOut.TagSet {
			tags[awsroot.ToString(tag.Key)] = awsroot.ToString(tag.Value)
		}
		result.Tags = tags
		result.Hash = tags["guardian-hash"]
	}
	if versioningOut, versioningErr := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: awsroot.String(name),
	}); versioningErr == nil {
		result.Versioning = versioningOut.Status == s3types.BucketVersioningStatusEnabled
	}
	return result, true, nil
}

func (b *AWSBackend) DeleteBucket(ctx context.Context, name string) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would delete S3 bucket: %s\n", name)
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := s3.NewFromConfig(cfg)

	_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: awsroot.String(name),
	})
	if isAWSNotFound(err) {
		return nil
	}
	return err
}

// --- CloudWatch Logs ---

func (b *AWSBackend) UpsertLogGroup(ctx context.Context, group LogGroup) error {
	if b.dry() {
		fmt.Printf("  [dry-run] would create log group: %s\n", group.Name)
		return nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return err
	}
	client := cloudwatchlogs.NewFromConfig(cfg)

	_, err = client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: awsroot.String(group.Name),
		Tags:         toCWLTags(group.Tags),
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return err
	}
	return nil
}

func (b *AWSBackend) GetLogGroup(ctx context.Context, name string) (LogGroup, bool, error) {
	if b.dry() {
		return LogGroup{}, false, nil
	}
	cfg, err := b.loadConfig(ctx, "")
	if err != nil {
		return LogGroup{}, false, err
	}
	client := cloudwatchlogs.NewFromConfig(cfg)

	out, err := client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
		LogGroupNamePrefix: awsroot.String(name),
	})
	if err != nil {
		return LogGroup{}, false, err
	}
	for _, lg := range out.LogGroups {
		if awsroot.ToString(lg.LogGroupName) == name {
			return LogGroup{
				Name: awsroot.ToString(lg.LogGroupName),
			}, true, nil
		}
	}
	return LogGroup{}, false, nil
}

// --- Stub CDK operations (passthrough to CLIBackend when available) ---

func (b *AWSBackend) Synthesize(ctx context.Context, req StackRequest) error {
	return fmt.Errorf("AWSBackend does not support CDK synthesize; use CLIBackend")
}

func (b *AWSBackend) CheckEnvironment(ctx context.Context, req StackRequest) error {
	cfg, err := b.loadConfig(ctx, req.Target.Region)
	if err != nil {
		return err
	}
	client := cloudformation.NewFromConfig(cfg)
	_, err = client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: awsroot.String("CDKToolkit"),
	})
	return err
}

func (b *AWSBackend) GetStack(ctx context.Context, req StackRequest) (StackState, bool, error) {
	cfg, err := b.loadConfig(ctx, req.Target.Region)
	if err != nil {
		return StackState{}, false, err
	}
	client := cloudformation.NewFromConfig(cfg)
	out, err := client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: awsroot.String(req.Manifest.StackName),
	})
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return StackState{}, false, nil
		}
		return StackState{}, false, err
	}
	if len(out.Stacks) == 0 {
		return StackState{}, false, nil
	}
	s := out.Stacks[0]
	return StackState{
		ID:     awsroot.ToString(s.StackId),
		Name:   awsroot.ToString(s.StackName),
		Status: string(s.StackStatus),
	}, true, nil
}

func (b *AWSBackend) DetectDrift(ctx context.Context, req StackRequest) (StackDriftStatus, error) {
	return StackDriftUnknown, nil
}

func (b *AWSBackend) DeployStack(ctx context.Context, req StackRequest) (StackState, error) {
	return StackState{}, fmt.Errorf("AWSBackend does not support stack deploy; use CLIBackend")
}

func (b *AWSBackend) DeleteStack(ctx context.Context, req StackRequest) error {
	cfg, err := b.loadConfig(ctx, req.Target.Region)
	if err != nil {
		return err
	}
	client := cloudformation.NewFromConfig(cfg)
	_, err = client.DeleteStack(ctx, &cloudformation.DeleteStackInput{
		StackName: awsroot.String(req.Manifest.StackName),
	})
	return err
}

// --- Helper functions ---

func isAWSNotFound(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "notfound") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "resourcenotfound") ||
		strings.Contains(msg, "clusternotfound") ||
		strings.Contains(msg, "statuscode: 404")
}

func portOrDefault(ports []PortDef) int {
	for _, p := range ports {
		if p.ContainerPort > 0 {
			return p.ContainerPort
		}
	}
	return 80
}

func toEFSTags(tags map[string]string) []efstypes.Tag {
	var out []efstypes.Tag
	for k, v := range tags {
		out = append(out, efstypes.Tag{Key: awsroot.String(k), Value: awsroot.String(v)})
	}
	return out
}

func tagsFromEFSDescription(tags []efstypes.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[awsroot.ToString(tag.Key)] = awsroot.ToString(tag.Value)
	}
	return out
}

func toS3Tags(tags map[string]string) []s3types.Tag {
	out := make([]s3types.Tag, 0, len(tags))
	for k, v := range tags {
		out = append(out, s3types.Tag{Key: awsroot.String(k), Value: awsroot.String(v)})
	}
	sort.Slice(out, func(i, j int) bool {
		return awsroot.ToString(out[i].Key) < awsroot.ToString(out[j].Key)
	})
	return out
}

func toSSMTags(tags map[string]string) []ssmtypes.Tag {
	var out []ssmtypes.Tag
	for k, v := range tags {
		out = append(out, ssmtypes.Tag{Key: awsroot.String(k), Value: awsroot.String(v)})
	}
	return out
}

func toSecretsManagerTags(tags map[string]string) []smtypes.Tag {
	var out []smtypes.Tag
	for k, v := range tags {
		out = append(out, smtypes.Tag{Key: awsroot.String(k), Value: awsroot.String(v)})
	}
	return out
}

func toECSTags(tags map[string]string) []ecstypes.Tag {
	var out []ecstypes.Tag
	for k, v := range tags {
		out = append(out, ecstypes.Tag{Key: awsroot.String(k), Value: awsroot.String(v)})
	}
	return out
}

func tagsFromECSTags(tags []ecstypes.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[awsroot.ToString(tag.Key)] = awsroot.ToString(tag.Value)
	}
	return out
}

func tagsFromELBv2(tags []elbv2types.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[awsroot.ToString(tag.Key)] = awsroot.ToString(tag.Value)
	}
	return out
}

func toELBv2Tags(tags map[string]string) []elbv2types.Tag {
	var out []elbv2types.Tag
	for k, v := range tags {
		out = append(out, elbv2types.Tag{Key: awsroot.String(k), Value: awsroot.String(v)})
	}
	return out
}

func toCWLTags(tags map[string]string) map[string]string {
	return tags
}

func toECSPortMappings(ports []PortDef) []ecstypes.PortMapping {
	var out []ecstypes.PortMapping
	for _, p := range ports {
		proto := ecstypes.TransportProtocolTcp
		hostPort := int32(p.HostPort)
		if hostPort == 0 {
			hostPort = int32(p.ContainerPort)
		}
		out = append(out, ecstypes.PortMapping{
			Name:          awsroot.String(p.Name),
			ContainerPort: awsroot.Int32(int32(p.ContainerPort)),
			HostPort:      awsroot.Int32(hostPort),
			Protocol:      proto,
		})
	}
	return out
}

func toECSEnv(env map[string]string) []ecstypes.KeyValuePair {
	var out []ecstypes.KeyValuePair
	for k, v := range env {
		out = append(out, ecstypes.KeyValuePair{Name: awsroot.String(k), Value: awsroot.String(v)})
	}
	return out
}

func toECSSecrets(secrets map[string]string) []ecstypes.Secret {
	var out []ecstypes.Secret
	for k, v := range secrets {
		out = append(out, ecstypes.Secret{
			Name:      awsroot.String(k),
			ValueFrom: awsroot.String(v),
		})
	}
	return out
}

func toLaunchTypes(launchType string) []ecstypes.Compatibility {
	switch launchType {
	case "EC2":
		return []ecstypes.Compatibility{ecstypes.CompatibilityEc2}
	case "FARGATE":
		return []ecstypes.Compatibility{ecstypes.CompatibilityFargate}
	default:
		return []ecstypes.Compatibility{ecstypes.CompatibilityFargate}
	}
}

func toELBv2HealthProto(proto string) elbv2types.ProtocolEnum {
	switch strings.ToUpper(proto) {
	case "HTTPS":
		return elbv2types.ProtocolEnumHttps
	case "TCP":
		return elbv2types.ProtocolEnumTcp
	case "HTTP":
		return elbv2types.ProtocolEnumHttp
	default:
		return elbv2types.ProtocolEnumHttp
	}
}
