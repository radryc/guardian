package awsdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/radryc/guardian/internal/cdkgen"
	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/registry"
)

// GeneratedStackDriver deploys the TypeScript CDK app that Guardian generated
// for an intent (spec.implementation=cdk) as one CloudFormation stack, reusing
// the CDK/CloudFormation backend. The generated source tree and its pusher
// manifest are read from the MonoFS store.
type GeneratedStackDriver struct{ baseDriver }

func (d *GeneratedStackDriver) Type() string { return assetdomain.TypeGeneratedStack }

func (d *GeneratedStackDriver) Validate(props map[string]any) error { return nil }

func (d *GeneratedStackDriver) loadManifest(ctx context.Context, in registry.AssetInput) (*cdkgen.PayloadManifest, error) {
	sourceDir := strings.TrimSpace(in.Asset.Payload["generated"])
	if sourceDir == "" {
		return nil, fmt.Errorf("asset %q payload.generated is required", in.Asset.Name)
	}
	if in.Store == nil {
		return nil, fmt.Errorf("store is required to read generated stack payload")
	}
	content, err := in.Store.ReadFile(ctx, path.Join(sourceDir, cdkgen.PayloadManifestFileName))
	if err != nil {
		return nil, fmt.Errorf("read generated stack manifest: %w", err)
	}
	var manifest cdkgen.PayloadManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("decode generated stack manifest: %w", err)
	}
	if manifest.StackName == "" || manifest.SourceDir == "" {
		return nil, fmt.Errorf("generated stack manifest is incomplete")
	}
	return &manifest, nil
}

func (d *GeneratedStackDriver) prepareRequest(ctx context.Context, in registry.AssetInput, withSource bool) (StackRequest, *cdkgen.PayloadManifest, func(), error) {
	if strings.TrimSpace(in.Target.Account) == "" {
		return StackRequest{}, nil, noopCleanup, fmt.Errorf("target.account is required for AWS CDK stacks")
	}
	if strings.TrimSpace(in.Target.Region) == "" {
		return StackRequest{}, nil, noopCleanup, fmt.Errorf("target.region is required for AWS CDK stacks")
	}
	manifest, err := d.loadManifest(ctx, in)
	if err != nil {
		return StackRequest{}, nil, noopCleanup, err
	}
	payload := stackPayload{
		SourceType:     "cdk-ts",
		SourceDir:      manifest.SourceDir,
		StackName:      manifest.StackName,
		StackID:        manifest.StackID,
		PackageManager: "npm",
	}
	req := StackRequest{
		PartitionName: in.PartitionName,
		IntentName:    in.IntentName,
		AssetName:     in.Asset.Name,
		Target:        in.Target,
		Manifest:      payload,
	}
	if !withSource {
		return req, manifest, noopCleanup, nil
	}
	workspaceDir, snapshots, cleanup, err := stageSourceTree(ctx, in.Store, manifest.SourceDir)
	if err != nil {
		return StackRequest{}, nil, noopCleanup, err
	}
	appCommand, appErr := resolveAppCommand(workspaceDir, payload)
	if appErr != nil {
		cleanup()
		return StackRequest{}, nil, noopCleanup, appErr
	}
	req.WorkspaceDir = workspaceDir
	req.AppCommand = appCommand
	req.DesiredHash = desiredHash(in, payload, nil, nil, snapshots)
	if arn := d.ensureExecutionRole(ctx, in, manifest.RequiredActions); arn != "" {
		req.ExecutionRoleARN = arn
	}
	req.Tags = driverLabels(in, req.DesiredHash)
	req.Tags["guardian.account"] = in.Target.Account
	req.Tags["guardian.region"] = in.Target.Region
	return req, manifest, cleanup, nil
}

func (d *GeneratedStackDriver) Check(ctx context.Context, in registry.AssetInput) error {
	req, _, cleanup, err := d.prepareRequest(ctx, in, true)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := d.backend.Synthesize(ctx, req); err != nil {
		return err
	}
	if err := d.backend.CheckEnvironment(ctx, req); err != nil {
		return err
	}
	current, ok, err := d.backend.GetStack(ctx, req)
	if err != nil {
		return err
	}
	if ok && strings.Contains(current.Status, "_IN_PROGRESS") {
		return fmt.Errorf("stack %s is already in progress with status %s", current.Name, current.Status)
	}
	return nil
}

func (d *GeneratedStackDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	req, manifest, cleanup, err := d.prepareRequest(ctx, in, true)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	defer cleanup()
	if err := d.backend.Synthesize(ctx, req); err != nil {
		return taskdomain.DriftReport{}, err
	}
	if err := d.backend.CheckEnvironment(ctx, req); err != nil {
		return taskdomain.DriftReport{}, err
	}
	current, ok, err := d.backend.GetStack(ctx, req)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	changed := func(summary string) taskdomain.DriftReport {
		return taskdomain.DriftReport{
			Status:        "Changed",
			Summary:       summary,
			ChangedAssets: append([]string(nil), manifest.GeneratedAssets...),
		}
	}
	if !ok {
		return changed("generated cloudformation stack is missing"), nil
	}
	if current.Tags["guardian.hash"] != req.DesiredHash {
		return changed("generated stack differs from desired hash"), nil
	}
	driftStatus, err := d.backend.DetectDrift(ctx, req)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if driftStatus != StackDriftInSync {
		return changed("generated stack drift detected"), nil
	}
	return taskdomain.DriftReport{
		Status:  "InSync",
		Summary: "generated stack is in sync",
	}, nil
}

func (d *GeneratedStackDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	req, manifest, cleanup, err := d.prepareRequest(ctx, in, true)
	if err != nil {
		return registry.AssetResult{}, err
	}
	defer cleanup()
	if err := d.backend.Synthesize(ctx, req); err != nil {
		return registry.AssetResult{}, err
	}
	if err := d.backend.CheckEnvironment(ctx, req); err != nil {
		return registry.AssetResult{}, err
	}
	stack, err := d.backend.DeployStack(ctx, req)
	if err != nil {
		return registry.AssetResult{}, err
	}
	outputs := map[string]string{
		"stackName": manifest.StackName,
		"region":    in.Target.Region,
		"account":   in.Target.Account,
	}
	for alias, outputKey := range manifest.OutputMap {
		if value := stack.Outputs[outputKey]; value != "" {
			outputs[alias] = value
		}
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *GeneratedStackDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	req, _, cleanup, err := d.prepareRequest(ctx, in, false)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := d.backend.DeleteStack(ctx, req); err != nil {
		return err
	}
	d.deleteExecutionRole(ctx, in)
	return nil
}
