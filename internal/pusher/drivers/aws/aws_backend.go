package awsdriver

import (
	"context"
	"fmt"
	"strings"

	awsroot "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
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
}

func NewAWSBackend(profile, defaultRegion string, dryRun bool) *AWSBackend {
	if defaultRegion == "" {
		defaultRegion = "us-east-1"
	}
	return &AWSBackend{profile: profile, defaultRegion: defaultRegion, dryRun: dryRun}
}

func (b *AWSBackend) loadConfig(ctx context.Context, region string) (awsroot.Config, error) {
	if region == "" {
		region = b.defaultRegion
	}
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	if b.profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(b.profile))
	}
	return awsconfig.LoadDefaultConfig(ctx, opts...)
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
			return awsroot.ToString(desc.FileSystems[0].FileSystemId), nil
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
	return FileSystem{
		ID:   awsroot.ToString(f.FileSystemId),
		Name: awsroot.ToString(f.Name),
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

	_, err = client.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      awsroot.String(param.Name),
		Value:     awsroot.String(param.Value),
		Type:      paramType,
		Tags:      toSSMTags(param.Tags),
		Overwrite: awsroot.Bool(false),
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "ParameterAlreadyExists") {
			_, err = client.PutParameter(ctx, &ssm.PutParameterInput{
				Name:      awsroot.String(param.Name),
				Value:     awsroot.String(param.Value),
				Type:      paramType,
				Overwrite: awsroot.Bool(true),
			})
		}
		if err != nil {
			return err
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
	return Parameter{
		Name:  awsroot.ToString(out.Parameter.Name),
		Value: awsroot.ToString(out.Parameter.Value),
		Type:  string(out.Parameter.Type),
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
		Tags:          toSecretsManagerTags(secret.Tags),
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

	taskDef, err := b.upsertTaskDefinition(ctx, client, svc)
	if err != nil {
		return fmt.Errorf("register task definition: %w", err)
	}

	return b.upsertECSService(ctx, client, svc, taskDef)
}

func (b *AWSBackend) upsertTaskDefinition(ctx context.Context, client *ecs.Client, svc ECSService) (string, error) {
	container := ecstypes.ContainerDefinition{
		Name:  awsroot.String(svc.Container.Name),
		Image: awsroot.String(svc.Container.Image),
		PortMappings: toECSPortMappings(svc.Container.Ports),
		LogConfiguration: &ecstypes.LogConfiguration{
			LogDriver: ecstypes.LogDriverAwslogs,
			Options: map[string]string{
				"awslogs-group":         svc.LogGroup,
				"awslogs-region":        "",
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

	out, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:                  awsroot.String(svc.TaskFamily),
		ContainerDefinitions:    []ecstypes.ContainerDefinition{container},
		TaskRoleArn:             awsroot.String(svc.TaskRole),
		ExecutionRoleArn:        awsroot.String(svc.ExecRole),
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

func (b *AWSBackend) upsertECSService(ctx context.Context, client *ecs.Client, svc ECSService, taskDefARN string) error {
	netConf := &ecstypes.NetworkConfiguration{
		AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
			Subnets:        svc.Subnets,
			SecurityGroups: svc.SecurityGroups,
			AssignPublicIp: ecstypes.AssignPublicIpDisabled,
		},
	}
	if svc.AssignPublicIP {
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
		Tags:                  toECSTags(svc.Tags),
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

	_, err := client.CreateService(ctx, createIn)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			_, upErr := client.UpdateService(ctx, &ecs.UpdateServiceInput{
				Cluster:        awsroot.String(svc.Cluster),
				Service:        awsroot.String(svc.Name),
				TaskDefinition: awsroot.String(taskDefARN),
				DesiredCount:   awsroot.Int32(int32(svc.DesiredCount)),
			})
			return upErr
		}
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
	return ECSService{
		Name:         awsroot.ToString(out.Services[0].ServiceName),
		Cluster:      awsroot.ToString(out.Services[0].ClusterArn),
		DesiredCount: int(out.Services[0].DesiredCount),
	}, true, nil
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

	out, err := client.CreateLoadBalancer(ctx, &elasticloadbalancingv2.CreateLoadBalancerInput{
		Name:          awsroot.String(lb.Name),
		Type:          lbType,
		Scheme:        scheme,
		Subnets:       lb.Subnets,
		SecurityGroups: lb.SecurityGroups,
		Tags:           toELBv2Tags(lb.Tags),
	})
	if err != nil {
		return "", fmt.Errorf("create load balancer: %w", err)
	}
	if len(out.LoadBalancers) == 0 {
		return "", fmt.Errorf("create load balancer: no load balancer returned")
	}
	return awsroot.ToString(out.LoadBalancers[0].LoadBalancerArn), nil
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
	return LoadBalancer{
		ARN:     awsroot.ToString(lb.LoadBalancerArn),
		Name:    awsroot.ToString(lb.LoadBalancerName),
		DNSName: awsroot.ToString(lb.DNSName),
		Type:    string(lb.Type),
		Scheme:  string(lb.Scheme),
	}, true, nil
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

	out, err := client.CreateTargetGroup(ctx, &elasticloadbalancingv2.CreateTargetGroupInput{
		Name:       awsroot.String(tg.Name),
		Port:       awsroot.Int32(int32(tg.Port)),
		Protocol:   proto,
		VpcId:      awsroot.String(tg.VPCID),
		TargetType: targetType,
		HealthCheckPath:     awsroot.String(healthPath),
		HealthCheckProtocol: toELBv2HealthProto(tg.HealthProto),
		HealthCheckPort:     awsroot.String(tg.HealthPort),
		Tags:                 toELBv2Tags(tg.Tags),
	})
	if err != nil {
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

	out, err := client.CreateListener(ctx, &elasticloadbalancingv2.CreateListenerInput{
		LoadBalancerArn: awsroot.String(listener.LoadBalancerARN),
		Port:            awsroot.Int32(int32(listener.Port)),
		Protocol:        proto,
		DefaultActions: []elbv2types.Action{
			{
				Type: elbv2types.ActionTypeEnumForward,
				ForwardConfig: &elbv2types.ForwardActionConfig{
					TargetGroups: []elbv2types.TargetGroupTuple{
						{TargetGroupArn: awsroot.String(listener.DefaultActionARN)},
					},
				},
			},
		},
		Tags: toELBv2Tags(listener.Tags),
	})
	if err != nil {
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
				ARN:              awsroot.ToString(l.ListenerArn),
				LoadBalancerARN:  awsroot.ToString(l.LoadBalancerArn),
				Port:             int(awsroot.ToInt32(l.Port)),
				Protocol:         string(l.Protocol),
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
	if bucket.Region != "us-east-1" {
		createIn.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(bucket.Region),
		}
	}

	_, err = client.CreateBucket(ctx, createIn)
	if err != nil {
		if strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") || strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return fmt.Errorf("create bucket: %w", err)
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
	return BucketSpec{Name: name}, true, nil
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
		Tags:          toCWLTags(group.Tags),
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
