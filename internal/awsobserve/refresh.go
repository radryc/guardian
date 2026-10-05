package awsobserve

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	awsroot "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
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
			driftStatus, drifted, err := detectStackDrift(ctx, client, states[i].StackName)
			if err != nil {
				// Drift detection is best effort: keep the DescribeStacks state.
				continue
			}
			if driftStatus != "" {
				states[i].DriftStatus = driftStatus
			}
			states[i].DriftedResourceCount = drifted
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
	return state
}

func detectStackDrift(ctx context.Context, client *cloudformation.Client, stackName string) (string, int, error) {
	out, err := client.DetectStackDrift(ctx, &cloudformation.DetectStackDriftInput{StackName: awsroot.String(stackName)})
	if err != nil {
		return "", 0, err
	}
	detectionID := awsroot.ToString(out.StackDriftDetectionId)
	if detectionID == "" {
		return "", 0, fmt.Errorf("empty drift detection id for %s", stackName)
	}
	deadline := time.Now().Add(driftMaxWait)
	for {
		status, err := client.DescribeStackDriftDetectionStatus(ctx, &cloudformation.DescribeStackDriftDetectionStatusInput{
			StackDriftDetectionId: awsroot.String(detectionID),
		})
		if err != nil {
			return "", 0, err
		}
		switch status.DetectionStatus {
		case cftypes.StackDriftDetectionStatusDetectionComplete:
			drifted := 0
			if status.DriftedStackResourceCount != nil {
				drifted = int(*status.DriftedStackResourceCount)
			}
			return string(status.StackDriftStatus), drifted, nil
		case cftypes.StackDriftDetectionStatusDetectionFailed:
			return "", 0, fmt.Errorf("drift detection failed for %s: %s", stackName, awsroot.ToString(status.DetectionStatusReason))
		}
		if time.Now().After(deadline) {
			return "", 0, fmt.Errorf("drift detection timed out for %s", stackName)
		}
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-time.After(driftPollInterval):
		}
	}
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
