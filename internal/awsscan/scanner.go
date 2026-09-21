package awsscan

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	awsroot "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/cloudcontrol"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfntypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
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
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const scanRegionConcurrency = 4
const defaultInventoryDetail = InventoryDetailSummary

type Scanner interface {
	Scan(ctx context.Context, req *ScanRequest) *ScanResult
}

type awsScanner struct {
	account        string
	assumeRoleName string
	externalID     string
	regionPin      string

	mu   sync.Mutex
	cfgs map[string]awsroot.Config
}

func NewScanner(account, assumeRoleName, externalID, regionPin string) Scanner {
	return &awsScanner{
		account:        strings.TrimSpace(account),
		assumeRoleName: strings.TrimSpace(assumeRoleName),
		externalID:     strings.TrimSpace(externalID),
		regionPin:      strings.TrimSpace(regionPin),
		cfgs:           make(map[string]awsroot.Config),
	}
}

func (s *awsScanner) awsConfig(ctx context.Context, region string) (awsroot.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg, ok := s.cfgs[region]; ok {
		return cfg, nil
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return awsroot.Config{}, fmt.Errorf("load aws config for region %s: %w", region, err)
	}
	if s.assumeRoleName != "" && s.account != "" {
		roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", s.account, s.assumeRoleName)
		provider := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), roleARN, func(options *stscreds.AssumeRoleOptions) {
			options.RoleSessionName = "guardian-aws-scan"
			if s.externalID != "" {
				options.ExternalID = awsroot.String(s.externalID)
			}
		})
		cfg.Credentials = awsroot.NewCredentialsCache(provider)
	}
	s.cfgs[region] = cfg
	return cfg, nil
}

func (s *awsScanner) Scan(ctx context.Context, req *ScanRequest) *ScanResult {
	startedAt := time.Now().UTC()
	if req == nil {
		req = &ScanRequest{}
	}
	result := &ScanResult{
		APIVersion: APIVersion,
		Kind:       ResultKind,
		ScanID:     req.ScanID,
		Account:    firstNonEmpty(req.Account, s.account),
		Status:     ScanStatusRunning,
		StartedAt:  startedAt,
		Request:    req,
	}
	fail := func(msg string) *ScanResult {
		result.Status = ScanStatusFailed
		result.FinishedAt = time.Now().UTC()
		result.Errors = append(result.Errors, ScanError{Message: msg})
		fillSummary(result)
		return result
	}

	cfg, err := s.awsConfig(ctx, "us-east-1")
	if err != nil {
		return fail(err.Error())
	}
	identity, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fail(fmt.Sprintf("verify aws credentials: %v", err))
	}
	identityAccount := awsroot.ToString(identity.Account)
	if s.assumeRoleName != "" && s.account != "" && identityAccount != s.account {
		return fail(fmt.Sprintf("assumed role resolved to account %s, expected %s", identityAccount, s.account))
	}
	if identityAccount != result.Account {
		result.Errors = append(result.Errors, ScanError{
			Message: fmt.Sprintf("scan credentials resolve to account %s while scan targets %s", identityAccount, result.Account),
		})
	}

	regions, err := s.resolveRegions(ctx, req)
	if err != nil {
		return fail(err.Error())
	}
	if len(regions) == 0 {
		return fail("no regions to scan")
	}
	result.Regions = regions

	s.scanBuckets(ctx, regions[0], req, regions, result)

	var mu sync.Mutex
	jobs := make(chan string)
	wg := &sync.WaitGroup{}
	workers := scanRegionConcurrency
	if len(regions) < workers {
		workers = len(regions)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for region := range jobs {
				data := s.scanRegion(ctx, req, region)
				mu.Lock()
				result.FileSystems = append(result.FileSystems, data.fileSystems...)
				result.Parameters = append(result.Parameters, data.parameters...)
				result.Secrets = append(result.Secrets, data.secrets...)
				result.Services = append(result.Services, data.services...)
				result.LoadBalancers = append(result.LoadBalancers, data.loadBalancers...)
				result.Stacks = append(result.Stacks, data.stacks...)
				result.Errors = append(result.Errors, data.errors...)
				for regionKey, byType := range data.inventory {
					if result.Inventory == nil {
						result.Inventory = make(map[string]map[string][]InventoryResource)
					}
					if result.Inventory[regionKey] == nil {
						result.Inventory[regionKey] = make(map[string][]InventoryResource)
					}
					for typeName, resources := range byType {
						result.Inventory[regionKey][typeName] = append(result.Inventory[regionKey][typeName], resources...)
					}
				}
				mu.Unlock()
			}
		}()
	}
	for _, region := range regions {
		jobs <- region
	}
	close(jobs)
	wg.Wait()

	sortScanResult(result)
	result.Status = ScanStatusSucceeded
	if ctx.Err() != nil {
		result.Status = ScanStatusFailed
		result.Errors = append(result.Errors, ScanError{Message: fmt.Sprintf("scan canceled: %v", ctx.Err())})
	}
	result.FinishedAt = time.Now().UTC()
	fillSummary(result)
	return result
}

func (s *awsScanner) resolveRegions(ctx context.Context, req *ScanRequest) ([]string, error) {
	requested := req.Regions
	if s.regionPin != "" {
		if len(requested) == 0 || strings.EqualFold(strings.TrimSpace(requested[0]), AllRegions) {
			return []string{s.regionPin}, nil
		}
		for _, region := range requested {
			if strings.EqualFold(strings.TrimSpace(region), s.regionPin) {
				return []string{s.regionPin}, nil
			}
		}
		return []string{s.regionPin}, nil
	}
	if len(requested) == 0 {
		requested = []string{AllRegions}
	}
	wantsAll := false
	for _, region := range requested {
		if strings.EqualFold(strings.TrimSpace(region), AllRegions) {
			wantsAll = true
			break
		}
	}
	if wantsAll {
		cfg, err := s.awsConfig(ctx, "us-east-1")
		if err != nil {
			return nil, err
		}
		out, err := ec2.NewFromConfig(cfg).DescribeRegions(ctx, &ec2.DescribeRegionsInput{AllRegions: awsroot.Bool(false)})
		if err != nil {
			return nil, fmt.Errorf("list enabled regions: %w", err)
		}
		regions := make([]string, 0, len(out.Regions))
		for _, region := range out.Regions {
			if name := awsroot.ToString(region.RegionName); name != "" {
				regions = append(regions, name)
			}
		}
		sort.Strings(regions)
		return regions, nil
	}
	seen := make(map[string]struct{}, len(requested))
	regions := make([]string, 0, len(requested))
	for _, region := range requested {
		name := strings.ToLower(strings.TrimSpace(region))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		regions = append(regions, name)
	}
	sort.Strings(regions)
	return regions, nil
}

type regionScanData struct {
	fileSystems    []FileSystemResource
	parameters     []ParameterResource
	secrets        []SecretResource
	services       []ServiceResource
	loadBalancers  []LoadBalancerResource
	stacks         []StackResource
	inventory      map[string]map[string][]InventoryResource
	errors         []ScanError
}

func (d *regionScanData) addError(region, resourceType, message string) {
	d.errors = append(d.errors, ScanError{Region: region, ResourceType: resourceType, Message: message})
}

func (s *awsScanner) scanRegion(ctx context.Context, req *ScanRequest, region string) regionScanData {
	data := regionScanData{}
	cfg, err := s.awsConfig(ctx, region)
	if err != nil {
		data.addError(region, "", err.Error())
		return data
	}
	data.fileSystems, _ = s.scanFileSystems(ctx, cfg, region, &data)
	data.parameters, _ = s.scanParameters(ctx, cfg, region, &data)
	data.secrets, _ = s.scanSecrets(ctx, cfg, region, &data)
	data.services, _ = s.scanServices(ctx, cfg, region, &data)
	data.loadBalancers, _ = s.scanLoadBalancers(ctx, cfg, region, &data)
	data.stacks, _ = s.scanStacks(ctx, cfg, region, &data)
	if req.Inventory {
		data.inventory = s.scanInventory(ctx, cfg, region, req, &data)
	}
	return data
}

func (s *awsScanner) scanFileSystems(ctx context.Context, cfg awsroot.Config, region string, data *regionScanData) ([]FileSystemResource, error) {
	client := efs.NewFromConfig(cfg)
	paginator := efs.NewDescribeFileSystemsPaginator(client, &efs.DescribeFileSystemsInput{})
	out := make([]FileSystemResource, 0)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			data.addError(region, "AWS::EFS::FileSystem", err.Error())
			return out, err
		}
		for _, fs := range page.FileSystems {
			tags := tagsFromEFS(fs.Tags)
			out = append(out, FileSystemResource{
				ID:             awsroot.ToString(fs.FileSystemId),
				CreationToken:  awsroot.ToString(fs.CreationToken),
				Name:           awsroot.ToString(fs.Name),
				Region:         region,
				LifeCycleState: string(fs.LifeCycleState),
				Encrypted:      awsroot.ToBool(fs.Encrypted),
				Tags:           tags,
				Managed:        ManagedFromTags(tags),
				Stack:          tags["aws:cloudformation:stack-name"],
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *awsScanner) scanParameters(ctx context.Context, cfg awsroot.Config, region string, data *regionScanData) ([]ParameterResource, error) {
	client := ssm.NewFromConfig(cfg)
	paginator := ssm.NewDescribeParametersPaginator(client, &ssm.DescribeParametersInput{})
	out := make([]ParameterResource, 0)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			data.addError(region, "AWS::SSM::Parameter", err.Error())
			return out, err
		}
		for _, meta := range page.Parameters {
			name := awsroot.ToString(meta.Name)
			if strings.HasPrefix(name, "/aws/") {
				continue
			}
			tags := map[string]string{}
			if tagsOut, err := client.ListTagsForResource(ctx, &ssm.ListTagsForResourceInput{
				ResourceType: ssmtypes.ResourceTypeForTaggingParameter,
				ResourceId:   awsroot.String(name),
			}); err == nil {
				tags = tagsFromSSM(tagsOut.TagList)
			}
			paramType := string(meta.Type)
			value := ""
			if paramType == string(ssmtypes.ParameterTypeString) {
				if valueOut, err := client.GetParameter(ctx, &ssm.GetParameterInput{
					Name: awsroot.String(name),
				}); err == nil && valueOut.Parameter != nil {
					value = awsroot.ToString(valueOut.Parameter.Value)
				}
			}
			out = append(out, ParameterResource{
				Name:    name,
				Region:  region,
				Type:    paramType,
				Value:   value,
				Tags:    tags,
				Managed: ManagedFromTags(tags),
				Stack:   tags["aws:cloudformation:stack-name"],
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *awsScanner) scanSecrets(ctx context.Context, cfg awsroot.Config, region string, data *regionScanData) ([]SecretResource, error) {
	client := secretsmanager.NewFromConfig(cfg)
	paginator := secretsmanager.NewListSecretsPaginator(client, &secretsmanager.ListSecretsInput{})
	out := make([]SecretResource, 0)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			data.addError(region, "AWS::SecretsManager::Secret", err.Error())
			return out, err
		}
		for _, secret := range page.SecretList {
			if secret.DeletedDate != nil {
				continue
			}
			tags := tagsFromSecretsManager(secret.Tags)
			out = append(out, SecretResource{
				ARN:         awsroot.ToString(secret.ARN),
				Name:        awsroot.ToString(secret.Name),
				Region:      region,
				Description: awsroot.ToString(secret.Description),
				Tags:        tags,
				Managed:     ManagedFromTags(tags),
				Stack:       tags["aws:cloudformation:stack-name"],
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *awsScanner) scanServices(ctx context.Context, cfg awsroot.Config, region string, data *regionScanData) ([]ServiceResource, error) {
	client := ecs.NewFromConfig(cfg)
	out := make([]ServiceResource, 0)
	taskDefs := make(map[string]*ecstypes.TaskDefinition)

	clusterArns := make([]string, 0)
	clusterPaginator := ecs.NewListClustersPaginator(client, &ecs.ListClustersInput{})
	for clusterPaginator.HasMorePages() {
		page, err := clusterPaginator.NextPage(ctx)
		if err != nil {
			data.addError(region, "AWS::ECS::Cluster", err.Error())
			return out, err
		}
		clusterArns = append(clusterArns, page.ClusterArns...)
	}
	sort.Strings(clusterArns)

	for _, clusterArn := range clusterArns {
		clusterName := clusterNameFromARN(clusterArn)
		serviceArns := make([]string, 0)
		servicePaginator := ecs.NewListServicesPaginator(client, &ecs.ListServicesInput{Cluster: awsroot.String(clusterArn)})
		for servicePaginator.HasMorePages() {
			page, err := servicePaginator.NextPage(ctx)
			if err != nil {
				data.addError(region, "AWS::ECS::Service", fmt.Sprintf("cluster %s: %v", clusterName, err))
				break
			}
			serviceArns = append(serviceArns, page.ServiceArns...)
		}
		for start := 0; start < len(serviceArns); start += 10 {
			end := start + 10
			if end > len(serviceArns) {
				end = len(serviceArns)
			}
			batch := serviceArns[start:end]
			desc, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
				Cluster:  awsroot.String(clusterArn),
				Services: batch,
			})
			if err != nil {
				data.addError(region, "AWS::ECS::Service", fmt.Sprintf("cluster %s: %v", clusterName, err))
				continue
			}
			for _, svc := range desc.Services {
				if awsroot.ToString(svc.Status) == "INACTIVE" {
					continue
				}
				tags := map[string]string{}
				if awsroot.ToString(svc.ServiceArn) != "" {
					if tagsOut, err := client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
						ResourceArn: svc.ServiceArn,
					}); err == nil {
						tags = tagsFromECS(tagsOut.Tags)
					}
				}
				resource := ServiceResource{
					Region:       region,
					Cluster:      clusterArn,
					ClusterName:  clusterName,
					Name:         awsroot.ToString(svc.ServiceName),
					Status:       awsroot.ToString(svc.Status),
					DesiredCount: int(svc.DesiredCount),
					LaunchType:   string(svc.LaunchType),
					Tags:         tags,
					Managed:      ManagedFromTags(tags),
					Stack:        tags["aws:cloudformation:stack-name"],
				}
				taskDefArn := awsroot.ToString(svc.TaskDefinition)
				if taskDefArn != "" {
					taskDef, ok := taskDefs[taskDefArn]
					if !ok {
						taskDefOut, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
							TaskDefinition: awsroot.String(taskDefArn),
						})
						if err != nil {
							data.addError(region, "AWS::ECS::TaskDefinition", fmt.Sprintf("service %s: %v", resource.Name, err))
						} else {
							taskDef = taskDefOut.TaskDefinition
							taskDefs[taskDefArn] = taskDef
						}
					}
					if taskDef != nil {
						resource.TaskFamily = awsroot.ToString(taskDef.Family)
						resource.TaskRole = awsroot.ToString(taskDef.TaskRoleArn)
						resource.ExecRole = awsroot.ToString(taskDef.ExecutionRoleArn)
						container := pickContainer(taskDef.ContainerDefinitions)
						if container != nil {
							resource.Image = awsroot.ToString(container.Image)
							if container.Cpu > 0 {
								resource.CPU = int(container.Cpu)
							}
							if container.Memory != nil {
								resource.Memory = int(awsroot.ToInt32(container.Memory))
							}
							if len(container.Command) > 0 {
								resource.Command = append([]string(nil), container.Command...)
							}
							env := make(map[string]string, len(container.Environment))
							for _, kv := range container.Environment {
								env[awsroot.ToString(kv.Name)] = awsroot.ToString(kv.Value)
							}
							if len(env) > 0 {
								resource.Env = env
							}
							ports := make([]ServicePort, 0, len(container.PortMappings))
							for _, mapping := range container.PortMappings {
								ports = append(ports, ServicePort{
									Name:          awsroot.ToString(mapping.Name),
									ContainerPort: int(awsroot.ToInt32(mapping.ContainerPort)),
									HostPort:      int(awsroot.ToInt32(mapping.HostPort)),
									Protocol:      strings.ToLower(string(mapping.Protocol)),
								})
							}
							if len(ports) > 0 {
								resource.Ports = ports
							}
							if container.LogConfiguration != nil {
								resource.LogGroup = container.LogConfiguration.Options["awslogs-group"]
							}
						}
					}
				}
				if svc.NetworkConfiguration != nil && svc.NetworkConfiguration.AwsvpcConfiguration != nil {
					net := svc.NetworkConfiguration.AwsvpcConfiguration
					resource.Subnets = append([]string(nil), net.Subnets...)
					resource.SecurityGroups = append([]string(nil), net.SecurityGroups...)
					resource.AssignPublicIP = string(net.AssignPublicIp) == string(ecstypes.AssignPublicIpEnabled)
				}
				out = append(out, resource)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ClusterName != out[j].ClusterName {
			return out[i].ClusterName < out[j].ClusterName
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func pickContainer(defs []ecstypes.ContainerDefinition) *ecstypes.ContainerDefinition {
	if len(defs) == 0 {
		return nil
	}
	for i := range defs {
		if awsroot.ToBool(defs[i].Essential) {
			return &defs[i]
		}
	}
	return &defs[0]
}

func (s *awsScanner) scanLoadBalancers(ctx context.Context, cfg awsroot.Config, region string, data *regionScanData) ([]LoadBalancerResource, error) {
	client := elasticloadbalancingv2.NewFromConfig(cfg)
	descriptions := make([]elbv2types.LoadBalancer, 0)
	paginator := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(client, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			data.addError(region, "AWS::ElasticLoadBalancingV2::LoadBalancer", err.Error())
			return nil, err
		}
		descriptions = append(descriptions, page.LoadBalancers...)
	}

	tagsByArn := make(map[string]map[string]string, len(descriptions))
	for start := 0; start < len(descriptions); start += 20 {
		end := start + 20
		if end > len(descriptions) {
			end = len(descriptions)
		}
		arns := make([]string, 0, end-start)
		for _, lb := range descriptions[start:end] {
			arns = append(arns, awsroot.ToString(lb.LoadBalancerArn))
		}
		tagsOut, err := client.DescribeTags(ctx, &elasticloadbalancingv2.DescribeTagsInput{ResourceArns: arns})
		if err != nil {
			data.addError(region, "AWS::ElasticLoadBalancingV2::LoadBalancer", err.Error())
			continue
		}
		for _, desc := range tagsOut.TagDescriptions {
			tagsByArn[awsroot.ToString(desc.ResourceArn)] = tagsFromELBv2(desc.Tags)
		}
	}

	out := make([]LoadBalancerResource, 0, len(descriptions))
	for _, lb := range descriptions {
		arn := awsroot.ToString(lb.LoadBalancerArn)
		tags := tagsByArn[arn]
		resource := LoadBalancerResource{
			Region:  region,
			ARN:     arn,
			Name:    awsroot.ToString(lb.LoadBalancerName),
			Type:    string(lb.Type),
			Scheme:  string(lb.Scheme),
			DNSName: awsroot.ToString(lb.DNSName),
			VPCID:   awsroot.ToString(lb.VpcId),
			Tags:    tags,
			Managed: ManagedFromTags(tags),
			Stack:   tags["aws:cloudformation:stack-name"],
		}
		for _, az := range lb.AvailabilityZones {
			if subnet := awsroot.ToString(az.SubnetId); subnet != "" {
				resource.Subnets = append(resource.Subnets, subnet)
			}
		}
		resource.SecurityGroups = append([]string(nil), lb.SecurityGroups...)
		if listenersOut, err := client.DescribeListeners(ctx, &elasticloadbalancingv2.DescribeListenersInput{
			LoadBalancerArn: awsroot.String(arn),
		}); err == nil {
			listeners := make([]ListenerResource, 0, len(listenersOut.Listeners))
			for _, listener := range listenersOut.Listeners {
				listeners = append(listeners, ListenerResource{
					Port:     int(awsroot.ToInt32(listener.Port)),
					Protocol: string(listener.Protocol),
				})
			}
			sort.Slice(listeners, func(i, j int) bool { return listeners[i].Port < listeners[j].Port })
			resource.Listeners = listeners
		} else {
			data.addError(region, "AWS::ElasticLoadBalancingV2::Listener", fmt.Sprintf("load balancer %s: %v", resource.Name, err))
		}
		if targetGroupsOut, err := client.DescribeTargetGroups(ctx, &elasticloadbalancingv2.DescribeTargetGroupsInput{
			LoadBalancerArn: awsroot.String(arn),
		}); err == nil {
			targetGroups := make([]TargetGroupResource, 0, len(targetGroupsOut.TargetGroups))
			for _, tg := range targetGroupsOut.TargetGroups {
				targetGroups = append(targetGroups, TargetGroupResource{
					Name:       awsroot.ToString(tg.TargetGroupName),
					Port:       int(awsroot.ToInt32(tg.Port)),
					Protocol:   string(tg.Protocol),
					TargetType: string(tg.TargetType),
				})
			}
			sort.Slice(targetGroups, func(i, j int) bool { return targetGroups[i].Name < targetGroups[j].Name })
			resource.TargetGroups = targetGroups
		}
		out = append(out, resource)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *awsScanner) scanStacks(ctx context.Context, cfg awsroot.Config, region string, data *regionScanData) ([]StackResource, error) {
	client := cloudformation.NewFromConfig(cfg)
	summaries := make([]cfntypes.StackSummary, 0)
	paginator := cloudformation.NewListStacksPaginator(client, &cloudformation.ListStacksInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			data.addError(region, "AWS::CloudFormation::Stack", err.Error())
			return nil, err
		}
		summaries = append(summaries, page.StackSummaries...)
	}

	out := make([]StackResource, 0)
	seen := make(map[string]struct{})
	for _, summary := range summaries {
		if summary.StackStatus == cfntypes.StackStatusDeleteComplete {
			continue
		}
		stackID := awsroot.ToString(summary.StackId)
		if _, ok := seen[stackID]; ok {
			continue
		}
		seen[stackID] = struct{}{}
		stackName := awsroot.ToString(summary.StackName)
		resource := StackResource{
			Region:      region,
			ID:          stackID,
			Name:        stackName,
			Status:      string(summary.StackStatus),
			DriftStatus: string(summary.DriftInformation.StackDriftStatus),
		}
		if desc, err := client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{StackName: awsroot.String(stackName)}); err == nil && len(desc.Stacks) > 0 {
			tags := tagsFromCFN(desc.Stacks[0].Tags)
			resource.Tags = tags
			resource.Managed = ManagedFromTags(tags)
			if desc.Stacks[0].DriftInformation != nil {
				resource.DriftStatus = string(desc.Stacks[0].DriftInformation.StackDriftStatus)
			}
		}
		resourceIDs := make(map[string]string)
		resourcePaginator := cloudformation.NewListStackResourcesPaginator(client, &cloudformation.ListStackResourcesInput{StackName: awsroot.String(stackName)})
		for resourcePaginator.HasMorePages() {
			page, err := resourcePaginator.NextPage(ctx)
			if err != nil {
				data.addError(region, "AWS::CloudFormation::Stack", fmt.Sprintf("stack %s resources: %v", stackName, err))
				break
			}
			for _, res := range page.StackResourceSummaries {
				physicalID := awsroot.ToString(res.PhysicalResourceId)
				if physicalID == "" {
					continue
				}
				resourceIDs[awsroot.ToString(res.LogicalResourceId)] = physicalID
			}
		}
		if len(resourceIDs) > 0 {
			resource.ResourceIDs = resourceIDs
		}
		out = append(out, resource)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

var directCoveredInventoryExcludes = []string{
	"AWS::S3::*",
	"AWS::EFS::*",
	"AWS::SSM::Parameter",
	"AWS::SecretsManager::Secret",
	"AWS::ECS::*",
	"AWS::ElasticLoadBalancingV2::*",
	"AWS::CloudFormation::Stack",
	"AWS::CloudFormation::StackSet",
	"AWS::CloudWatchLogs::LogGroup",
}

func (s *awsScanner) scanInventory(ctx context.Context, cfg awsroot.Config, region string, req *ScanRequest, data *regionScanData) map[string]map[string][]InventoryResource {
	cfClient := cloudformation.NewFromConfig(cfg)
	include := req.IncludeResourceTypes
	if len(include) == 0 {
		include = []string{"*"}
	}
	exclude := append(append([]string(nil), req.ExcludeResourceTypes...), directCoveredInventoryExcludes...)

	supported, err := listSupportedResourceTypes(ctx, cfClient, commonTypePrefix(include, exclude))
	if err != nil {
		data.addError(region, "", fmt.Sprintf("list cloudformation resource types: %v", err))
		return nil
	}
	enabled := filterResourceTypes(supported, include, exclude)
	if len(enabled) == 0 {
		return nil
	}

	detail := strings.TrimSpace(req.InventoryDetail)
	if detail == "" {
		detail = defaultInventoryDetail
	}

	client := cloudcontrol.NewFromConfig(cfg)
	out := make(map[string][]InventoryResource)
	for _, typeName := range enabled {
		resources, err := listCloudControlResources(ctx, client, typeName)
		if err != nil {
			data.addError(region, typeName, err.Error())
			continue
		}
		for _, resource := range resources {
			tags := tagsFromProperties(resource.properties)
			if isDefaultInventoryResource(typeName, resource.identifier, tags) {
				continue
			}
			entry := InventoryResource{Identifier: resource.identifier, Tags: tags}
			if detail == InventoryDetailFull && len(resource.properties) > 0 {
				entry.Properties = json.RawMessage(resource.properties)
			}
			out[typeName] = append(out[typeName], entry)
		}
	}
	for typeName := range out {
		entries := out[typeName]
		sort.Slice(entries, func(i, j int) bool { return entries[i].Identifier < entries[j].Identifier })
		out[typeName] = entries
	}
	if len(out) == 0 {
		return nil
	}
	return map[string]map[string][]InventoryResource{region: out}
}

type cloudControlResource struct {
	identifier string
	properties []byte
}

func listSupportedResourceTypes(ctx context.Context, client *cloudformation.Client, prefix string) ([]string, error) {
	seen := make(map[string]struct{})
	filters := &cfntypes.TypeFilters{Category: cfntypes.CategoryAwsTypes}
	if prefix != "" {
		filters.TypeNamePrefix = awsroot.String(prefix)
	}
	for _, provisioningType := range []cfntypes.ProvisioningType{
		cfntypes.ProvisioningTypeFullyMutable,
		cfntypes.ProvisioningTypeImmutable,
	} {
		paginator := cloudformation.NewListTypesPaginator(client, &cloudformation.ListTypesInput{
			Type:              cfntypes.RegistryTypeResource,
			Visibility:        cfntypes.VisibilityPublic,
			ProvisioningType:  provisioningType,
			DeprecatedStatus:  cfntypes.DeprecatedStatusLive,
			Filters:           filters,
		})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, summary := range page.TypeSummaries {
				name := awsroot.ToString(summary.TypeName)
				if name == "" {
					continue
				}
				seen[name] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func listCloudControlResources(ctx context.Context, client *cloudcontrol.Client, typeName string) ([]cloudControlResource, error) {
	paginator := cloudcontrol.NewListResourcesPaginator(client, &cloudcontrol.ListResourcesInput{TypeName: awsroot.String(typeName)})
	out := make([]cloudControlResource, 0)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", typeName, err)
		}
		for _, desc := range page.ResourceDescriptions {
			out = append(out, cloudControlResource{
				identifier: awsroot.ToString(desc.Identifier),
				properties: []byte(awsroot.ToString(desc.Properties)),
			})
		}
	}
	return out, nil
}

func (s *awsScanner) scanBuckets(ctx context.Context, configRegion string, req *ScanRequest, regions []string, result *ScanResult) {
	cfg, err := s.awsConfig(ctx, configRegion)
	if err != nil {
		result.Errors = append(result.Errors, ScanError{ResourceType: "AWS::S3::Bucket", Message: err.Error()})
		return
	}
	client := s3.NewFromConfig(cfg)
	regionSet := make(map[string]struct{}, len(regions))
	for _, region := range regions {
		regionSet[region] = struct{}{}
	}
	wantsAll := len(req.Regions) == 0 || (len(req.Regions) == 1 && strings.EqualFold(strings.TrimSpace(req.Regions[0]), AllRegions))

	buckets := make([]BucketResource, 0)
	paginator := s3.NewListBucketsPaginator(client, &s3.ListBucketsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			result.Errors = append(result.Errors, ScanError{ResourceType: "AWS::S3::Bucket", Message: err.Error()})
			return
		}
		for _, bucket := range page.Buckets {
			name := awsroot.ToString(bucket.Name)
			region := awsroot.ToString(bucket.BucketRegion)
			if region == "" {
				if loc, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{Bucket: awsroot.String(name)}); err == nil {
					region = normalizeBucketRegion(string(loc.LocationConstraint))
				}
			}
			if region == "" {
				region = "us-east-1"
			}
			if !wantsAll {
				if _, ok := regionSet[region]; !ok {
					continue
				}
			}
			tags := map[string]string{}
			if tagsOut, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: awsroot.String(name)}); err == nil {
				tags = tagsFromS3(tagsOut.TagSet)
			}
			versioned := false
			if versioningOut, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: awsroot.String(name)}); err == nil {
				versioned = versioningOut.Status == s3types.BucketVersioningStatusEnabled
			}
			buckets = append(buckets, BucketResource{
				Name:      name,
				Region:    region,
				Versioned: versioned,
				Tags:      tags,
				Managed:   ManagedFromTags(tags),
				Stack:     tags["aws:cloudformation:stack-name"],
			})
		}
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Name < buckets[j].Name })
	result.Buckets = buckets
}

func normalizeBucketRegion(location string) string {
	switch location {
	case "":
		return "us-east-1"
	case "EU":
		return "eu-west-1"
	default:
		return location
	}
}

func ManagedFromTags(tags map[string]string) ManagedInfo {
	info := ManagedInfo{}
	if value, ok := tags["guardian-managed"]; ok && strings.EqualFold(strings.TrimSpace(value), "true") {
		info.Managed = true
	}
	info.Partition = tags["guardian-partition"]
	info.Intent = tags["guardian-intent"]
	info.Asset = tags["guardian-asset"]
	info.AssetType = tags["guardian-type"]
	info.Hash = tags["guardian-hash"]
	return info
}

func filterResourceTypes(supported, include, exclude []string) []string {
	out := make([]string, 0, len(supported))
	for _, name := range supported {
		if !matchesAnyPattern(include, name) {
			continue
		}
		if matchesAnyPattern(exclude, name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

func matchesAnyPattern(patterns []string, value string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == "*" {
			return true
		}
		matched, err := path.Match(pattern, value)
		if err != nil {
			continue
		}
		if matched {
			return true
		}
	}
	return false
}

func commonTypePrefix(patterns ...[]string) string {
	var values []string
	for _, group := range patterns {
		for _, pattern := range group {
			pattern = strings.TrimSpace(pattern)
			if pattern == "" || pattern == "*" {
				return ""
			}
			cut := pattern
			for _, wildcard := range []string{"*", "?", "["} {
				if idx := strings.Index(cut, wildcard); idx >= 0 {
					cut = cut[:idx]
				}
			}
			values = append(values, cut)
		}
	}
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}

func isDefaultInventoryResource(typeName, identifier string, tags map[string]string) bool {
	switch typeName {
	case "AWS::IAM::ManagedPolicy":
		return strings.HasPrefix(identifier, "arn:aws:iam::aws:policy/")
	case "AWS::KMS::Alias":
		return strings.HasPrefix(identifier, "alias/aws/")
	case "AWS::SSM::Document":
		for _, prefix := range []string{"AWS", "Amazon", "Aws", "SSM", "AlertLogic", "CrowdStrike", "Dynatrace", "FalconSensor", "New-Relic", "TrendMicro"} {
			if strings.HasPrefix(identifier, prefix) {
				return true
			}
		}
		return false
	case "AWS::SSM::PatchBaseline":
		return strings.HasPrefix(identifier, "AWS-")
	case "AWS::Events::EventBus":
		return identifier == "default"
	case "AWS::Athena::WorkGroup":
		return identifier == "primary"
	case "AWS::Athena::DataCatalog":
		return identifier == "AwsDataCatalog"
	case "AWS::Backup::BackupVault":
		return identifier == "Default"
	case "AWS::ECS::CapacityProvider":
		return identifier == "FARGATE" || identifier == "FARGATE_SPOT"
	case "AWS::Scheduler::ScheduleGroup":
		return identifier == "default"
	case "AWS::S3::StorageLens":
		return identifier == "default-account-dashboard"
	case "AWS::Cassandra::Keyspace":
		return identifier == "system_multiregion_info"
	case "AWS::CodeDeploy::DeploymentConfig":
		return strings.HasPrefix(identifier, "CodeDeployDefault.")
	case "AWS::ElastiCache::ParameterGroup",
		"AWS::MemoryDB::ParameterGroup",
		"AWS::Neptune::DBClusterParameterGroup",
		"AWS::Neptune::DBParameterGroup",
		"AWS::RDS::DBClusterParameterGroup",
		"AWS::RDS::DBParameterGroup":
		return strings.HasPrefix(identifier, "default.") || strings.HasPrefix(identifier, "default:")
	case "AWS::AppConfig::DeploymentStrategy":
		return strings.HasPrefix(identifier, "AppConfig.")
	case "AWS::XRay::Group":
		return strings.Contains(identifier, ":group/Default")
	case "AWS::XRay::SamplingRule":
		return strings.Contains(identifier, ":sampling-rule/Default")
	case "AWS::RAM::Permission":
		return tags["PermissionType"] == "AWS_MANAGED"
	case "AWS::CloudTrail::Dashboard":
		return tags["Type"] == "MANAGED"
	case "AWS::EC2::PrefixList":
		return tags["OwnerId"] == "AWS"
	default:
		return false
	}
}

func tagsFromProperties(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var props map[string]any
	if err := json.Unmarshal(raw, &props); err != nil {
		return nil
	}
	for _, key := range []string{"Tags", "TagList", "TagsList", "TagSet"} {
		switch typed := props[key].(type) {
		case []any:
			tags := make(map[string]string, len(typed))
			for _, item := range typed {
				entry, ok := item.(map[string]any)
				if !ok {
					continue
				}
				keyValue, _ := entry["Key"].(string)
				valueValue, _ := entry["Value"].(string)
				if keyValue != "" {
					tags[keyValue] = valueValue
				}
			}
			if len(tags) > 0 {
				return tags
			}
		case map[string]any:
			tags := make(map[string]string, len(typed))
			for k, v := range typed {
				if value, ok := v.(string); ok {
					tags[k] = value
				}
			}
			if len(tags) > 0 {
				return tags
			}
		}
	}
	return nil
}

func clusterNameFromARN(arn string) string {
	if arn == "" {
		return "default"
	}
	idx := strings.LastIndex(arn, "/")
	if idx < 0 || idx == len(arn)-1 {
		return "default"
	}
	return arn[idx+1:]
}

func tagsFromEFS(tags []efstypes.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[awsroot.ToString(tag.Key)] = awsroot.ToString(tag.Value)
	}
	return out
}

func tagsFromSSM(tags []ssmtypes.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[awsroot.ToString(tag.Key)] = awsroot.ToString(tag.Value)
	}
	return out
}

func tagsFromSecretsManager(tags []smtypes.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[awsroot.ToString(tag.Key)] = awsroot.ToString(tag.Value)
	}
	return out
}

func tagsFromECS(tags []ecstypes.Tag) map[string]string {
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

func tagsFromCFN(tags []cfntypes.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[awsroot.ToString(tag.Key)] = awsroot.ToString(tag.Value)
	}
	return out
}

func tagsFromS3(tags []s3types.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[awsroot.ToString(tag.Key)] = awsroot.ToString(tag.Value)
	}
	return out
}

func sortScanResult(result *ScanResult) {
	sort.Slice(result.FileSystems, func(i, j int) bool { return result.FileSystems[i].ID < result.FileSystems[j].ID })
	sort.Slice(result.Parameters, func(i, j int) bool { return result.Parameters[i].Name < result.Parameters[j].Name })
	sort.Slice(result.Secrets, func(i, j int) bool { return result.Secrets[i].Name < result.Secrets[j].Name })
	sort.Slice(result.Services, func(i, j int) bool {
		if result.Services[i].Region != result.Services[j].Region {
			return result.Services[i].Region < result.Services[j].Region
		}
		if result.Services[i].ClusterName != result.Services[j].ClusterName {
			return result.Services[i].ClusterName < result.Services[j].ClusterName
		}
		return result.Services[i].Name < result.Services[j].Name
	})
	sort.Slice(result.LoadBalancers, func(i, j int) bool {
		if result.LoadBalancers[i].Region != result.LoadBalancers[j].Region {
			return result.LoadBalancers[i].Region < result.LoadBalancers[j].Region
		}
		return result.LoadBalancers[i].Name < result.LoadBalancers[j].Name
	})
	sort.Slice(result.Stacks, func(i, j int) bool {
		if result.Stacks[i].Region != result.Stacks[j].Region {
			return result.Stacks[i].Region < result.Stacks[j].Region
		}
		return result.Stacks[i].Name < result.Stacks[j].Name
	})
	sort.Slice(result.Errors, func(i, j int) bool {
		if result.Errors[i].Region != result.Errors[j].Region {
			return result.Errors[i].Region < result.Errors[j].Region
		}
		if result.Errors[i].ResourceType != result.Errors[j].ResourceType {
			return result.Errors[i].ResourceType < result.Errors[j].ResourceType
		}
		return result.Errors[i].Message < result.Errors[j].Message
	})
}

func fillSummary(result *ScanResult) {
	result.Summary = ScanSummary{
		RegionCount:       len(result.Regions),
		BucketCount:       len(result.Buckets),
		FileSystemCount:   len(result.FileSystems),
		ParameterCount:    len(result.Parameters),
		SecretCount:       len(result.Secrets),
		ServiceCount:      len(result.Services),
		LoadBalancerCount: len(result.LoadBalancers),
		StackCount:        len(result.Stacks),
		InventoryCount:    result.CountInventory(),
		ManagedCount:      result.CountManaged(),
		ErrorCount:        len(result.Errors),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
