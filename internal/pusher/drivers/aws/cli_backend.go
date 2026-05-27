package awsdriver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	awsroot "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

type CLIBackend struct {
	cdk                string
	stateDir           string
	assumeRoleName     string
	externalID         string
	bootstrapStackName string
}

func NewCLIBackend(cdkBinary, stateDir, assumeRoleName, externalID, bootstrapStackName string) (*CLIBackend, error) {
	if strings.TrimSpace(cdkBinary) == "" {
		cdkBinary = "cdk"
	}
	resolved, err := exec.LookPath(cdkBinary)
	if err != nil {
		return nil, fmt.Errorf("locate cdk binary: %w", err)
	}
	if strings.TrimSpace(stateDir) == "" {
		stateDir = "/var/lib/guardian/pusher-aws"
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create aws pusher state dir %q: %w", stateDir, err)
	}
	if strings.TrimSpace(bootstrapStackName) == "" {
		bootstrapStackName = "CDKToolkit"
	}
	return &CLIBackend{
		cdk:                resolved,
		stateDir:           stateDir,
		assumeRoleName:     strings.TrimSpace(assumeRoleName),
		externalID:         strings.TrimSpace(externalID),
		bootstrapStackName: strings.TrimSpace(bootstrapStackName),
	}, nil
}

func (b *CLIBackend) Synthesize(ctx context.Context, req StackRequest) error {
	if req.Manifest.PrebuiltAssemblyDir != "" {
		return validatePrebuiltAssembly(req.WorkspaceDir)
	}
	if err := b.installDependencies(ctx, req); err != nil {
		return err
	}
	outputDir, cleanup, err := b.makeAssemblyDir()
	if err != nil {
		return err
	}
	defer cleanup()
	_, err = b.runCDK(ctx, req, outputDir, "synth", req.Manifest.StackID)
	return err
}

func (b *CLIBackend) CheckEnvironment(ctx context.Context, req StackRequest) error {
	client, err := b.cloudFormationClient(ctx, req)
	if err != nil {
		return err
	}
	_, err = client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: awsroot.String(b.bootstrapStackName),
	})
	if err != nil {
		if stackDoesNotExist(err) {
			return fmt.Errorf("cdk bootstrap stack %q not found in %s/%s", b.bootstrapStackName, req.Target.Account, req.Target.Region)
		}
		return fmt.Errorf("describe bootstrap stack %q: %w", b.bootstrapStackName, err)
	}
	return nil
}

func (b *CLIBackend) GetStack(ctx context.Context, req StackRequest) (StackState, bool, error) {
	client, err := b.cloudFormationClient(ctx, req)
	if err != nil {
		return StackState{}, false, err
	}
	out, err := client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: awsroot.String(req.Manifest.StackName),
	})
	if err != nil {
		if stackDoesNotExist(err) {
			return StackState{}, false, nil
		}
		return StackState{}, false, fmt.Errorf("describe stack %q: %w", req.Manifest.StackName, err)
	}
	if len(out.Stacks) == 0 {
		return StackState{}, false, nil
	}
	return fromCloudFormationStack(out.Stacks[0]), true, nil
}

func (b *CLIBackend) DetectDrift(ctx context.Context, req StackRequest) (StackDriftStatus, error) {
	client, err := b.cloudFormationClient(ctx, req)
	if err != nil {
		return StackDriftUnknown, err
	}
	out, err := client.DetectStackDrift(ctx, &cloudformation.DetectStackDriftInput{
		StackName: awsroot.String(req.Manifest.StackName),
	})
	if err != nil {
		if stackDoesNotExist(err) {
			return StackDriftUnknown, nil
		}
		return StackDriftUnknown, fmt.Errorf("detect drift for stack %q: %w", req.Manifest.StackName, err)
	}
	for {
		status, err := client.DescribeStackDriftDetectionStatus(ctx, &cloudformation.DescribeStackDriftDetectionStatusInput{
			StackDriftDetectionId: out.StackDriftDetectionId,
		})
		if err != nil {
			return StackDriftUnknown, fmt.Errorf("describe drift detection for stack %q: %w", req.Manifest.StackName, err)
		}
		switch status.DetectionStatus {
		case cftypes.StackDriftDetectionStatusDetectionComplete:
			if status.StackDriftStatus == cftypes.StackDriftStatusInSync {
				return StackDriftInSync, nil
			}
			return StackDriftDrifted, nil
		case cftypes.StackDriftDetectionStatusDetectionFailed:
			if status.DetectionStatusReason == nil {
				return StackDriftUnknown, fmt.Errorf("cloudformation drift detection failed")
			}
			return StackDriftUnknown, fmt.Errorf("cloudformation drift detection failed: %s", awsroot.ToString(status.DetectionStatusReason))
		default:
			select {
			case <-ctx.Done():
				return StackDriftUnknown, ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func (b *CLIBackend) DeployStack(ctx context.Context, req StackRequest) (StackState, error) {
	if req.Manifest.PrebuiltAssemblyDir != "" {
		if err := validatePrebuiltAssembly(req.WorkspaceDir); err != nil {
			return StackState{}, err
		}
	} else {
		if err := b.installDependencies(ctx, req); err != nil {
			return StackState{}, err
		}
	}
	outputDir, cleanup, err := b.makeAssemblyDir()
	if err != nil {
		return StackState{}, err
	}
	defer cleanup()
	if _, err := b.runCDK(ctx, req, outputDir, "deploy", req.Manifest.StackID, "--require-approval", "never", "--ci", "true"); err != nil {
		return StackState{}, err
	}
	stack, ok, err := b.GetStack(ctx, req)
	if err != nil {
		return StackState{}, err
	}
	if !ok {
		return StackState{}, fmt.Errorf("stack %q was not found after deploy", req.Manifest.StackName)
	}
	return stack, nil
}

func (b *CLIBackend) DeleteStack(ctx context.Context, req StackRequest) error {
	client, err := b.cloudFormationClient(ctx, req)
	if err != nil {
		return err
	}
	_, err = client.DeleteStack(ctx, &cloudformation.DeleteStackInput{
		StackName: awsroot.String(req.Manifest.StackName),
	})
	if err != nil {
		if stackDoesNotExist(err) {
			return nil
		}
		return fmt.Errorf("delete stack %q: %w", req.Manifest.StackName, err)
	}
	waiter := cloudformation.NewStackDeleteCompleteWaiter(client)
	if err := waiter.Wait(ctx, &cloudformation.DescribeStacksInput{
		StackName: awsroot.String(req.Manifest.StackName),
	}, 10*time.Minute); err != nil && !stackDoesNotExist(err) {
		return fmt.Errorf("wait for stack %q deletion: %w", req.Manifest.StackName, err)
	}
	return nil
}

func (b *CLIBackend) installDependencies(ctx context.Context, req StackRequest) error {
	manager, args, err := packageManagerCommand(req.WorkspaceDir, req.Manifest.PackageManager)
	if err != nil || manager == "" {
		return err
	}
	binary, err := exec.LookPath(manager)
	if err != nil {
		return fmt.Errorf("locate %s binary: %w", manager, err)
	}
	if _, err := b.runProcess(ctx, req, req.WorkspaceDir, binary, args...); err != nil {
		return fmt.Errorf("%s %s: %w", manager, strings.Join(args, " "), err)
	}
	return nil
}

func packageManagerCommand(workspaceDir, preferred string) (string, []string, error) {
	if strings.TrimSpace(workspaceDir) == "" {
		return "", nil, nil
	}
	if !fileExists(filepath.Join(workspaceDir, "package.json")) {
		return "", nil, nil
	}
	manager := strings.ToLower(strings.TrimSpace(preferred))
	switch manager {
	case "":
		switch {
		case fileExists(filepath.Join(workspaceDir, "pnpm-lock.yaml")):
			manager = "pnpm"
		case fileExists(filepath.Join(workspaceDir, "yarn.lock")):
			manager = "yarn"
		default:
			manager = "npm"
		}
	case "none":
		return "", nil, nil
	}
	switch manager {
	case "npm":
		if fileExists(filepath.Join(workspaceDir, "package-lock.json")) || fileExists(filepath.Join(workspaceDir, "npm-shrinkwrap.json")) {
			return "npm", []string{"ci"}, nil
		}
		return "npm", []string{"install"}, nil
	case "pnpm":
		if fileExists(filepath.Join(workspaceDir, "pnpm-lock.yaml")) {
			return "pnpm", []string{"install", "--frozen-lockfile"}, nil
		}
		return "pnpm", []string{"install"}, nil
	case "yarn":
		if fileExists(filepath.Join(workspaceDir, "yarn.lock")) {
			return "yarn", []string{"install", "--frozen-lockfile"}, nil
		}
		return "yarn", []string{"install"}, nil
	default:
		return "", nil, fmt.Errorf("unsupported package manager %q", manager)
	}
}

func (b *CLIBackend) makeAssemblyDir() (string, func(), error) {
	dir, err := os.MkdirTemp(b.stateDir, "cdk-assembly-*")
	if err != nil {
		return "", nil, fmt.Errorf("create cloud assembly dir: %w", err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

func (b *CLIBackend) runCDK(ctx context.Context, req StackRequest, outputDir string, args ...string) ([]byte, error) {
	baseArgs := []string{args[0]}
	baseArgs = append(baseArgs, args[1:]...)
	baseArgs = append(baseArgs, "--app", req.AppCommand, "--output", outputDir)
	command := args[0]
	if command == "deploy" && b.bootstrapStackName != "" {
		baseArgs = append(baseArgs, "--toolkit-stack-name", b.bootstrapStackName)
	}
	contextKeys := make([]string, 0, len(req.Context))
	for key := range req.Context {
		contextKeys = append(contextKeys, key)
	}
	sort.Strings(contextKeys)
	for _, key := range contextKeys {
		baseArgs = append(baseArgs, "-c", key+"="+req.Context[key])
	}
	if command == "deploy" {
		tagKeys := make([]string, 0, len(req.Tags))
		for key := range req.Tags {
			tagKeys = append(tagKeys, key)
		}
		sort.Strings(tagKeys)
		for _, key := range tagKeys {
			baseArgs = append(baseArgs, "--tags", key+"="+req.Tags[key])
		}
	}
	out, err := b.runProcess(ctx, req, req.WorkspaceDir, b.cdk, baseArgs...)
	if err != nil {
		return nil, fmt.Errorf("cdk %s: %w", strings.Join(baseArgs, " "), err)
	}
	return out, nil
}

func (b *CLIBackend) runProcess(ctx context.Context, req StackRequest, dir, binary string, args ...string) ([]byte, error) {
	env, err := b.commandEnv(ctx, req)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil, err
	}
	return output, nil
}

func (b *CLIBackend) commandEnv(ctx context.Context, req StackRequest) ([]string, error) {
	cfg, err := b.awsConfig(ctx, req)
	if err != nil {
		return nil, err
	}
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieve aws credentials: %w", err)
	}
	env := []string{
		"AWS_REGION=" + req.Target.Region,
		"AWS_DEFAULT_REGION=" + req.Target.Region,
		"CDK_DEFAULT_ACCOUNT=" + req.Target.Account,
		"CDK_DEFAULT_REGION=" + req.Target.Region,
		"AWS_ACCESS_KEY_ID=" + creds.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + creds.SecretAccessKey,
	}
	if creds.SessionToken != "" {
		env = append(env, "AWS_SESSION_TOKEN="+creds.SessionToken)
	}
	keys := make([]string, 0, len(req.Env))
	for key := range req.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+req.Env[key])
	}
	return env, nil
}

func (b *CLIBackend) awsConfig(ctx context.Context, req StackRequest) (awsroot.Config, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(req.Target.Region))
	if err != nil {
		return awsroot.Config{}, fmt.Errorf("load aws config for %s/%s: %w", req.Target.Account, req.Target.Region, err)
	}
	roleName := strings.TrimSpace(b.assumeRoleName)
	if roleName == "" {
		return cfg, nil
	}
	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", req.Target.Account, roleName)
	provider := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), roleARN, func(options *stscreds.AssumeRoleOptions) {
		options.RoleSessionName = sessionName(req.PartitionName, req.IntentName, req.AssetName)
		if b.externalID != "" {
			options.ExternalID = awsroot.String(b.externalID)
		}
	})
	cfg.Credentials = awsroot.NewCredentialsCache(provider)
	return cfg, nil
}

func (b *CLIBackend) cloudFormationClient(ctx context.Context, req StackRequest) (*cloudformation.Client, error) {
	cfg, err := b.awsConfig(ctx, req)
	if err != nil {
		return nil, err
	}
	return cloudformation.NewFromConfig(cfg), nil
}

func sessionName(partition, intent, asset string) string {
	candidate := "guardian"
	for _, part := range []string{partition, intent, asset} {
		part = sanitizeSessionPart(part)
		if part == "" {
			continue
		}
		candidate += "-" + part
	}
	if len(candidate) <= 64 {
		return candidate
	}
	return candidate[:64]
}

func sanitizeSessionPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}

func fromCloudFormationStack(stack cftypes.Stack) StackState {
	state := StackState{
		ID:      awsroot.ToString(stack.StackId),
		Name:    awsroot.ToString(stack.StackName),
		Status:  string(stack.StackStatus),
		Tags:    map[string]string{},
		Outputs: map[string]string{},
	}
	for _, tag := range stack.Tags {
		if tag.Key == nil || tag.Value == nil {
			continue
		}
		state.Tags[*tag.Key] = *tag.Value
	}
	for _, output := range stack.Outputs {
		if output.OutputKey == nil || output.OutputValue == nil {
			continue
		}
		state.Outputs[*output.OutputKey] = *output.OutputValue
	}
	return state
}

func stackDoesNotExist(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if strings.EqualFold(apiErr.ErrorCode(), "ValidationError") && strings.Contains(strings.ToLower(apiErr.ErrorMessage()), "does not exist") {
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not exist")
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func validatePrebuiltAssembly(workspaceDir string) error {
	if strings.TrimSpace(workspaceDir) == "" {
		return fmt.Errorf("prebuilt assembly workspace is required")
	}
	manifestPath := filepath.Join(workspaceDir, "manifest.json")
	if !fileExists(manifestPath) {
		return fmt.Errorf("prebuilt assembly missing manifest.json at %s", manifestPath)
	}
	return nil
}
