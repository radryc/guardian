package awsdriver

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/pusher/secrets"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type baseDriver struct {
	backend  BackendAPI
	resolver secrets.Resolver
}

type CDKStackDriver struct{ baseDriver }

func Register(reg *registry.Registry, backend BackendAPI, resolver secrets.Resolver) {
	if reg == nil {
		return
	}
	if backend == nil {
		backend = NewBackend()
	}
	if resolver == nil {
		resolver = secrets.NoopResolver{}
	}
	reg.Register(&CDKStackDriver{baseDriver{backend: backend, resolver: resolver}})
}

func (d *CDKStackDriver) Type() string { return assetdomain.TypeCDKStack }

func (d *CDKStackDriver) Validate(props map[string]any) error {
	return assetdefs.Validate(assetdomain.Spec{Type: assetdomain.TypeCDKStack, Properties: props}, assetdefs.ValidationContext{})
}

func (d *CDKStackDriver) Check(ctx context.Context, in registry.AssetInput) error {
	req, cleanup, err := d.prepareRequest(ctx, in, true)
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

func (d *CDKStackDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	req, cleanup, err := d.prepareRequest(ctx, in, true)
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
	if !ok {
		return changedDrift(in.Asset.Name, "cloudformation stack is missing"), nil
	}
	if current.Tags["guardian.hash"] != req.DesiredHash {
		return changedDrift(in.Asset.Name, "cloudformation stack differs from desired hash"), nil
	}
	driftStatus, err := d.backend.DetectDrift(ctx, req)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if driftStatus != StackDriftInSync {
		return changedDrift(in.Asset.Name, "cloudformation stack drift detected"), nil
	}
	return inSyncDrift(in.Asset.Name, "cloudformation stack is in sync"), nil
}

func (d *CDKStackDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	req, cleanup, err := d.prepareRequest(ctx, in, true)
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
		"stackName": req.Manifest.StackName,
		"region":    req.Target.Region,
		"account":   req.Target.Account,
	}
	if stack.ID != "" {
		outputs["stackId"] = stack.ID
	}
	for key, value := range stack.Outputs {
		if _, exists := outputs[key]; !exists {
			outputs[key] = value
		}
	}
	for alias, outputKey := range req.Manifest.OutputMap {
		if value := stack.Outputs[outputKey]; value != "" {
			outputs[alias] = value
		}
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *CDKStackDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	req, cleanup, err := d.prepareRequest(ctx, in, false)
	if err != nil {
		return err
	}
	defer cleanup()
	return d.backend.DeleteStack(ctx, req)
}

func (d *CDKStackDriver) prepareRequest(ctx context.Context, in registry.AssetInput, withSource bool) (StackRequest, func(), error) {
	if strings.TrimSpace(in.Target.Account) == "" {
		return StackRequest{}, noopCleanup, fmt.Errorf("target.account is required for AWS CDK stacks")
	}
	if strings.TrimSpace(in.Target.Region) == "" {
		return StackRequest{}, noopCleanup, fmt.Errorf("target.region is required for AWS CDK stacks")
	}
	specAny, err := driverutil.DecodeAsset(in)
	if err != nil {
		return StackRequest{}, noopCleanup, err
	}
	spec, ok := specAny.(*assetdefs.CDKStackSpec)
	if !ok {
		return StackRequest{}, noopCleanup, fmt.Errorf("internal CDK stack spec type mismatch")
	}
	payload, err := loadStackPayload(ctx, in)
	if err != nil {
		return StackRequest{}, noopCleanup, err
	}
	env, err := driverutil.ResolveEnv(ctx, d.resolver, spec.Env)
	if err != nil {
		return StackRequest{}, noopCleanup, err
	}
	contextValues := normalizeStringMap(payload.Context)
	for key, value := range normalizeStringMap(spec.Context) {
		contextValues[key] = value
	}
	req := StackRequest{
		PartitionName: in.PartitionName,
		IntentName:    in.IntentName,
		AssetName:     in.Asset.Name,
		Target:        in.Target,
		Manifest:      payload,
		Context:       contextValues,
		Env:           env,
	}
	if !withSource {
		return req, noopCleanup, nil
	}
	var (
		workspaceDir string
		snapshots    []sourceFileSnapshot
		cleanup      func()
	)

	if payload.PrebuiltAssemblyDir != "" {
		workspaceDir, snapshots, cleanup, err = stageSourceTree(ctx, in.Store, payload.PrebuiltAssemblyDir)
		if err != nil {
			return StackRequest{}, noopCleanup, err
		}
		if !hasSnapshotPath(snapshots, "manifest.json") {
			cleanup()
			return StackRequest{}, noopCleanup, fmt.Errorf("prebuiltAssemblyDir %q must contain manifest.json", payload.PrebuiltAssemblyDir)
		}
		req.WorkspaceDir = workspaceDir
		req.AppCommand = workspaceDir
	} else {
		workspaceDir, snapshots, cleanup, err = stageSourceTree(ctx, in.Store, payload.SourceDir)
		if err != nil {
			return StackRequest{}, noopCleanup, err
		}
		appCommand, appErr := resolveAppCommand(workspaceDir, payload)
		if appErr != nil {
			cleanup()
			return StackRequest{}, noopCleanup, appErr
		}
		req.WorkspaceDir = workspaceDir
		req.AppCommand = appCommand
	}

	req.DesiredHash = desiredHash(in, payload, contextValues, env, snapshots)
	req.Tags = driverLabels(in, req.DesiredHash)
	req.Tags["guardian.account"] = in.Target.Account
	req.Tags["guardian.region"] = in.Target.Region
	return req, cleanup, nil
}

func hasSnapshotPath(snapshots []sourceFileSnapshot, relPath string) bool {
	needle := path.Clean(strings.TrimSpace(relPath))
	if needle == "" || needle == "." {
		return false
	}
	for _, snapshot := range snapshots {
		if path.Clean(snapshot.Path) == needle {
			return true
		}
	}
	return false
}

func driverLabels(in registry.AssetInput, hash string) map[string]string {
	labels := driverutil.Labels("aws", in, hash)
	if labels == nil {
		labels = map[string]string{}
	}
	labels["guardian.hash"] = hash
	return labels
}

func resolveAppCommand(workspaceDir string, payload stackPayload) (string, error) {
	if payload.AppCommand != "" {
		return payload.AppCommand, nil
	}
	entrypoint := payload.Entrypoint
	if entrypoint == "" {
		entrypoint = "bin/app.ts"
	}
	entrypoint = filepath.Clean(entrypoint)
	fullPath := filepath.Join(workspaceDir, entrypoint)
	info, err := os.Stat(fullPath)
	if err != nil {
		return "", fmt.Errorf("resolve entrypoint %q: %w", entrypoint, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("entrypoint %q must be a file", entrypoint)
	}
	return "npx ts-node --prefer-ts-exts " + entrypoint, nil
}

func stageSourceTree(ctx context.Context, store guardianapi.ReadStore, logicalDir string) (string, []sourceFileSnapshot, func(), error) {
	if store == nil {
		return "", nil, noopCleanup, fmt.Errorf("read store is required to stage CDK sources")
	}
	workspaceDir, err := os.MkdirTemp("", "guardian-aws-cdk-*")
	if err != nil {
		return "", nil, noopCleanup, fmt.Errorf("create temp workspace: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(workspaceDir)
	}
	files, err := walkStoreFiles(ctx, store, logicalDir)
	if err != nil {
		cleanup()
		return "", nil, noopCleanup, err
	}
	if len(files) == 0 {
		cleanup()
		return "", nil, noopCleanup, fmt.Errorf("sourceDir %q does not contain any files", logicalDir)
	}
	snapshots := make([]sourceFileSnapshot, 0, len(files))
	for _, logicalPath := range files {
		content, err := store.ReadFile(ctx, logicalPath)
		if err != nil {
			cleanup()
			return "", nil, noopCleanup, fmt.Errorf("read source file %s: %w", logicalPath, err)
		}
		relPath := strings.TrimPrefix(strings.TrimPrefix(logicalPath, path.Clean(logicalDir)), "/")
		if relPath == "" || relPath == logicalPath {
			relPath = path.Base(logicalPath)
		}
		destPath := filepath.Join(workspaceDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			cleanup()
			return "", nil, noopCleanup, fmt.Errorf("create workspace dir for %s: %w", logicalPath, err)
		}
		if err := os.WriteFile(destPath, content, 0o644); err != nil {
			cleanup()
			return "", nil, noopCleanup, fmt.Errorf("write workspace file %s: %w", destPath, err)
		}
		snapshots = append(snapshots, sourceFileSnapshot{Path: path.Clean(relPath), Content: string(content)})
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Path < snapshots[j].Path })
	return workspaceDir, snapshots, cleanup, nil
}

func walkStoreFiles(ctx context.Context, store guardianapi.ReadStore, logicalDir string) ([]string, error) {
	entries, err := store.ListDir(ctx, logicalDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0)
	for _, entry := range entries {
		child := path.Join(logicalDir, entry.Name)
		if entry.IsDir {
			nested, err := walkStoreFiles(ctx, store, child)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
			continue
		}
		out = append(out, child)
	}
	sort.Strings(out)
	return out, nil
}

func changedDrift(assetName, summary string) taskdomain.DriftReport {
	return taskdomain.DriftReport{
		Status:        "Changed",
		Summary:       summary,
		ChangedAssets: []string{assetName},
	}
}

func inSyncDrift(assetName, summary string) taskdomain.DriftReport {
	return taskdomain.DriftReport{
		Status:        "InSync",
		Summary:       summary,
		ChangedAssets: nil,
	}
}

func noopCleanup() {}
