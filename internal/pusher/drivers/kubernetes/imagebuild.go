package kubernetesdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/drivers/imagebuildutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/versioning/digest"
)

type ImageBuildDriver struct {
	baseDriver
	backend         ImageBuildBackendAPI
	defaultRegistry string
}

func (d *ImageBuildDriver) Type() string                  { return "ImageBuild" }
func (d *ImageBuildDriver) Validate(map[string]any) error { return nil }
func (d *ImageBuildDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

func (d *ImageBuildDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	req, cleanup, err := d.buildRequest(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	defer cleanup()
	currentRef, err := currentImageRef(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if currentRef == req.ImageRef {
		return inSyncDrift(in.Asset.Name, "kubernetes image build is in sync"), nil
	}
	return changedDrift(in.Asset.Name, "kubernetes image build differs"), nil
}

func (d *ImageBuildDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	req, cleanup, err := d.buildRequest(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	defer cleanup()
	result, err := d.backend.BuildAndPublish(ctx, req.ImageBuildRequest)
	if err != nil {
		return registry.AssetResult{}, err
	}
	logs := make([]taskdomain.LogEntry, 0, len(result.Logs))
	for _, l := range result.Logs {
		logs = append(logs, taskdomain.LogEntry{
			Timestamp: l.Timestamp,
			Level:     l.Level,
			Asset:     in.Asset.Name,
			Message:   l.Message,
		})
	}
	return registry.AssetResult{
		Outputs: map[string]string{
			"imageRef":   result.ImageRef,
			"repository": strings.TrimSpace(req.Repository),
			"registry":   strings.TrimSpace(req.Registry),
			"tag":        strings.TrimSpace(req.Tag),
		},
		Logs: logs,
	}, nil
}

func (d *ImageBuildDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

type preparedImageBuildRequest struct {
	ImageBuildRequest
	Repository string
	Registry   string
	Tag        string
}

func (d *ImageBuildDriver) buildRequest(ctx context.Context, in registry.AssetInput) (preparedImageBuildRequest, func(), error) {
	decoded, err := driverutil.DecodeAsset(in)
	if err != nil {
		return preparedImageBuildRequest{}, func() {}, err
	}
	spec, ok := decoded.(*assetdefs.ImageBuildSpec)
	if !ok {
		return preparedImageBuildRequest{}, func() {}, fmt.Errorf("asset %q is not an ImageBuild", in.Asset.Name)
	}
	workspaceDir, snapshots, cleanup, err := imagebuildutil.StageSourceTree(ctx, in.Store, spec.SourceDir)
	if err != nil {
		return preparedImageBuildRequest{}, cleanup, err
	}
	dockerfile := strings.TrimSpace(spec.Dockerfile)
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	buildArgs := assetdefs.NormalizeBuildArgs(spec.BuildArgs)
	registryHost := strings.TrimSpace(spec.Registry)
	if registryHost == "" {
		registryHost = strings.TrimSpace(d.defaultRegistry)
	}
	if registryHost == "" {
		cleanup()
		return preparedImageBuildRequest{}, func() {}, fmt.Errorf("image build asset %q requires registry or GUARDIAN_IMAGE_BUILD_REGISTRY", in.Asset.Name)
	}
	tag := "sha256-" + desiredImageBuildHash(in, spec, snapshots)[:16]
	imageRef := registryHost + "/" + strings.TrimSpace(spec.Repository) + ":" + tag
	insecure := true
	if spec.Insecure != nil {
		insecure = *spec.Insecure
	}
	return preparedImageBuildRequest{
		ImageBuildRequest: ImageBuildRequest{
			WorkspaceDir: workspaceDir,
			Dockerfile:   dockerfile,
			ImageRef:     imageRef,
			Target:       strings.TrimSpace(spec.Target),
			Platform:     strings.TrimSpace(spec.Platform),
			BuildArgs:    buildArgs,
			Insecure:     insecure,
		},
		Repository: strings.TrimSpace(spec.Repository),
		Registry:   registryHost,
		Tag:        tag,
	}, cleanup, nil
}

func desiredImageBuildHash(in registry.AssetInput, spec *assetdefs.ImageBuildSpec, snapshots []imagebuildutil.SourceFileSnapshot) string {
	return digest.MustNormalizedHash(struct {
		Base      string
		Spec      assetdefs.ImageBuildSpec
		Snapshots []imagebuildutil.SourceFileSnapshot
	}{
		Base: driverutil.AssetHash(in),
		Spec: assetdefs.ImageBuildSpec{
			Repository: strings.TrimSpace(spec.Repository),
			Registry:   strings.TrimSpace(spec.Registry),
			SourceDir:  strings.TrimSpace(spec.SourceDir),
			Dockerfile: strings.TrimSpace(spec.Dockerfile),
			Target:     strings.TrimSpace(spec.Target),
			Platform:   strings.TrimSpace(spec.Platform),
			BuildArgs:  assetdefs.NormalizeBuildArgs(spec.BuildArgs),
			Insecure:   spec.Insecure,
		},
		Snapshots: snapshots,
	})
}

func currentImageRef(ctx context.Context, in registry.AssetInput) (string, error) {
	if in.Store == nil {
		return "", nil
	}
	raw, err := in.Store.ReadFile(ctx, paths.IntentState(in.PartitionName, in.IntentName))
	if err != nil {
		return "", nil
	}
	var state statedomain.IntentState
	if err := json.Unmarshal(raw, &state); err != nil {
		return "", fmt.Errorf("decode intent state for %s/%s: %w", in.PartitionName, in.IntentName, err)
	}
	if state.Outputs == nil {
		return "", nil
	}
	return strings.TrimSpace(state.Outputs[in.Asset.Name+".imageRef"]), nil
}
