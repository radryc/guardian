package awsobserve

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	awsroot "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	driftPollInterval = 2 * time.Second
	driftMaxWait      = 2 * time.Minute
	maxRefreshStacks  = 500
)

// Refresher reads live CloudFormation state on demand so the cache can be
// primed before the first EventBridge event arrives and revalidated during a
// Guardian diff. It only performs read operations (plus DetectStackDrift, which
// is non-mutating but billable).
type Refresher struct {
	Account              string
	AssumeRoleName       string
	AssumeRoleExternalID string

	mu   sync.Mutex
	cfgs map[string]awsroot.Config
}

// NewRefresher builds a refresher using the pusher's credential chain and
// optional target-account role assumption.
func NewRefresher(account, assumeRoleName, externalID string) *Refresher {
	return &Refresher{
		Account:              strings.TrimSpace(account),
		AssumeRoleName:       strings.TrimSpace(assumeRoleName),
		AssumeRoleExternalID: strings.TrimSpace(externalID),
		cfgs:                 map[string]awsroot.Config{},
	}
}

func (r *Refresher) awsConfig(ctx context.Context, region string) (awsroot.Config, error) {
	if region == "" {
		region = "us-east-1"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cfg, ok := r.cfgs[region]; ok {
		return cfg, nil
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return awsroot.Config{}, fmt.Errorf("load aws config for region %s: %w", region, err)
	}
	if r.AssumeRoleName != "" && r.Account != "" {
		roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", r.Account, r.AssumeRoleName)
		provider := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), roleARN, func(options *stscreds.AssumeRoleOptions) {
			options.RoleSessionName = "guardian-aws-observe"
			if r.AssumeRoleExternalID != "" {
				options.ExternalID = awsroot.String(r.AssumeRoleExternalID)
			}
		})
		cfg.Credentials = awsroot.NewCredentialsCache(provider)
	}
	r.cfgs[region] = cfg
	return cfg, nil
}

// Refresh fetches live stack state. When stackNames is empty every stack in the
// region is listed. When detectDrift is set, stacks in a stable state get a
// fresh DetectStackDrift pass (billable).
func (r *Refresher) Refresh(ctx context.Context, region string, stackNames []string, detectDrift bool) ([]StackState, error) {
	cfg, err := r.awsConfig(ctx, region)
	if err != nil {
		return nil, err
	}
	client := cloudformation.NewFromConfig(cfg)
	states, err := describeStacks(ctx, client, region, r.Account, stackNames)
	if err != nil {
		return nil, err
	}
	if detectDrift {
		for i := range states {
			if !stableStackStatus(states[i].StackStatus) {
				continue
			}
			driftStatus, drifted, driftResources, err := detectStackDrift(ctx, client, states[i].StackName)
			if err != nil {
				// Drift detection is best effort: keep the DescribeStacks state.
				continue
			}
			if driftStatus != "" {
				states[i].DriftStatus = driftStatus
			}
			states[i].DriftedResourceCount = drifted
			states[i].DriftedResources = driftResources
		}
	}
	return states, nil
}

func describeStacks(ctx context.Context, client *cloudformation.Client, region, account string, stackNames []string) ([]StackState, error) {
	states := make([]StackState, 0, len(stackNames))
	if len(stackNames) > 0 {
		for _, name := range stackNames {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			out, err := client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{StackName: awsroot.String(name)})
			if err != nil {
				return nil, fmt.Errorf("describe stack %s: %w", name, err)
			}
			for _, stack := range out.Stacks {
				states = append(states, stackStateFromSDK(account, region, stack))
			}
		}
		return states, nil
	}
	var nextToken *string
	for {
		out, err := client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{NextToken: nextToken})
		if err != nil {
			return nil, fmt.Errorf("describe stacks: %w", err)
		}
		for _, stack := range out.Stacks {
			states = append(states, stackStateFromSDK(account, region, stack))
			if len(states) >= maxRefreshStacks {
				return states, nil
			}
		}
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		nextToken = out.NextToken
	}
	return states, nil
}

func stackStateFromSDK(account, region string, stack cftypes.Stack) StackState {
	state := StackState{
		Account:     account,
		Region:      region,
		StackID:     awsroot.ToString(stack.StackId),
		StackName:   awsroot.ToString(stack.StackName),
		StackStatus: string(stack.StackStatus),
	}
	if stack.DriftInformation != nil {
		state.DriftStatus = string(stack.DriftInformation.StackDriftStatus)
	}
	if len(stack.Tags) > 0 {
		state.Tags = make(map[string]string, len(stack.Tags))
		for _, tag := range stack.Tags {
			key := strings.TrimSpace(awsroot.ToString(tag.Key))
			if key == "" {
				continue
			}
			state.Tags[key] = awsroot.ToString(tag.Value)
		}
	}
	if len(stack.Outputs) > 0 {
		state.Outputs = make(map[string]string, len(stack.Outputs))
		for _, output := range stack.Outputs {
			key := strings.TrimSpace(awsroot.ToString(output.OutputKey))
			if key == "" {
				continue
			}
			state.Outputs[key] = awsroot.ToString(output.OutputValue)
		}
	}
	return state
}

func detectStackDrift(ctx context.Context, client *cloudformation.Client, stackName string) (string, int, []DriftedResource, error) {
	out, err := client.DetectStackDrift(ctx, &cloudformation.DetectStackDriftInput{StackName: awsroot.String(stackName)})
	if err != nil {
		return "", 0, nil, err
	}
	detectionID := awsroot.ToString(out.StackDriftDetectionId)
	if detectionID == "" {
		return "", 0, nil, fmt.Errorf("empty drift detection id for %s", stackName)
	}
	deadline := time.Now().Add(driftMaxWait)
	for {
		status, err := client.DescribeStackDriftDetectionStatus(ctx, &cloudformation.DescribeStackDriftDetectionStatusInput{
			StackDriftDetectionId: awsroot.String(detectionID),
		})
		if err != nil {
			return "", 0, nil, err
		}
		switch status.DetectionStatus {
		case cftypes.StackDriftDetectionStatusDetectionComplete:
			drifted := 0
			if status.DriftedStackResourceCount != nil {
				drifted = int(*status.DriftedStackResourceCount)
			}
			return string(status.StackDriftStatus), drifted, driftedResources(ctx, client, stackName), nil
		case cftypes.StackDriftDetectionStatusDetectionFailed:
			return "", 0, nil, fmt.Errorf("drift detection failed for %s: %s", stackName, awsroot.ToString(status.DetectionStatusReason))
		}
		if time.Now().After(deadline) {
			return "", 0, nil, fmt.Errorf("drift detection timed out for %s", stackName)
		}
		select {
		case <-ctx.Done():
			return "", 0, nil, ctx.Err()
		case <-time.After(driftPollInterval):
		}
	}
}

// driftedResources lists the CloudFormation resources that differ from the
// expected template. It is best effort: the stack-level drift count remains
// authoritative if the resource detail call fails.
func driftedResources(ctx context.Context, client *cloudformation.Client, stackName string) []DriftedResource {
	out, err := client.DescribeStackResourceDrifts(ctx, &cloudformation.DescribeStackResourceDriftsInput{
		StackName: awsroot.String(stackName),
	})
	if err != nil {
		return nil
	}
	resources := make([]DriftedResource, 0, len(out.StackResourceDrifts))
	for _, resource := range out.StackResourceDrifts {
		status := strings.ToUpper(string(resource.StackResourceDriftStatus))
		if status == "IN_SYNC" || status == "NOT_CHECKED" {
			continue
		}
		resources = append(resources, DriftedResource{
			LogicalID:    awsroot.ToString(resource.LogicalResourceId),
			ResourceType: awsroot.ToString(resource.ResourceType),
			Status:       status,
		})
	}
	return resources
}

// SSMClient returns an SSM client configured with the refresher's credentials
// and optional target-account role. The pusher uses it to read onboarding
// descriptors from Parameter Store.
func (r *Refresher) SSMClient(ctx context.Context, region string) (*ssm.Client, error) {
	cfg, err := r.awsConfig(ctx, region)
	if err != nil {
		return nil, err
	}
	return ssm.NewFromConfig(cfg), nil
}

// Template returns the live CloudFormation template body for a stack.
func (r *Refresher) Template(ctx context.Context, region, stackName string) (string, error) {
	cfg, err := r.awsConfig(ctx, region)
	if err != nil {
		return "", err
	}
	client := cloudformation.NewFromConfig(cfg)
	out, err := client.GetTemplate(ctx, &cloudformation.GetTemplateInput{StackName: awsroot.String(stackName)})
	if err != nil {
		return "", fmt.Errorf("get template %s: %w", stackName, err)
	}
	return awsroot.ToString(out.TemplateBody), nil
}

// StackResources returns the live logical-to-physical resource mapping for a
// stack. ListStackResources is used instead of DescribeStackResources because
// it is paginated for large stacks; the fields are the same.
func (r *Refresher) StackResources(ctx context.Context, region, stackName string) ([]StackResource, error) {
	cfg, err := r.awsConfig(ctx, region)
	if err != nil {
		return nil, err
	}
	client := cloudformation.NewFromConfig(cfg)
	var nextToken *string
	resources := make([]StackResource, 0, 64)
	for {
		out, err := client.ListStackResources(ctx, &cloudformation.ListStackResourcesInput{
			StackName: awsroot.String(stackName),
			NextToken: nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list stack resources %s: %w", stackName, err)
		}
		for _, summary := range out.StackResourceSummaries {
			resource := StackResource{
				LogicalID:    awsroot.ToString(summary.LogicalResourceId),
				PhysicalID:   awsroot.ToString(summary.PhysicalResourceId),
				ResourceType: awsroot.ToString(summary.ResourceType),
				Status:       string(summary.ResourceStatus),
				StatusReason: awsroot.ToString(summary.ResourceStatusReason),
			}
			if summary.LastUpdatedTimestamp != nil {
				resource.UpdatedAt = *summary.LastUpdatedTimestamp
			}
			if summary.DriftInformation != nil {
				resource.DriftStatus = string(summary.DriftInformation.StackResourceDriftStatus)
			}
			if resource.LogicalID != "" {
				resources = append(resources, resource)
			}
		}
		if out.NextToken == nil || awsroot.ToString(out.NextToken) == "" {
			break
		}
		nextToken = out.NextToken
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].LogicalID < resources[j].LogicalID })
	return resources, nil
}

func stableStackStatus(status string) bool {
	switch cftypes.StackStatus(status) {
	case cftypes.StackStatusCreateComplete,
		cftypes.StackStatusUpdateComplete,
		cftypes.StackStatusUpdateRollbackComplete,
		cftypes.StackStatusImportComplete:
		return true
	default:
		return false
	}
}
