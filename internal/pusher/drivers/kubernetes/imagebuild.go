package kubernetesdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(d.defaultRegistry) == "" {
		return fmt.Errorf("image build registry not configured: set GUARDIAN_IMAGE_BUILD_REGISTRY")
	}
	return nil
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
	log.Printf("[ImageBuild] drift asset=%s currentRef=%q desiredRef=%q", in.Asset.Name, currentRef, req.ImageRef)
	return changedDrift(in.Asset.Name, "kubernetes image build differs"), nil
}

func (d *ImageBuildDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	decoded, err := driverutil.DecodeAsset(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	spec, ok := decoded.(*assetdefs.ImageBuildSpec)
	if !ok {
		return registry.AssetResult{}, fmt.Errorf("asset %q is not an ImageBuild", in.Asset.Name)
	}
	req, cleanup, err := d.buildRequest(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	defer cleanup()

	outputs := registry.AssetResult{Outputs: map[string]string{
		"imageRef":   req.ImageRef,
		"repository": strings.TrimSpace(req.Repository),
		"registry":   strings.TrimSpace(req.Registry),
		"tag":        strings.TrimSpace(req.Tag),
	}}

	if exists, checkErr := d.backend.ImageExists(ctx, req.ImageRef); checkErr == nil && exists {
		outputs.Outputs["source"] = "registry"
		return outputs, nil
	}
	log.Printf("[ImageBuild] image=%s asset=%s not-in-registry: proceeding to build (tag=%s repo=%s)", req.ImageRef, in.Asset.Name, req.Tag, req.Repository)

	if spec.StampOnly {
		currentRef, err := currentImageRef(ctx, in)
		if err != nil {
			return registry.AssetResult{}, err
		}
		if currentRef == "" {
			return registry.AssetResult{}, fmt.Errorf("stampOnly: no current image ref for %s", in.Asset.Name)
		}
		if err := d.backend.StampImage(ctx, currentRef, req.ImageRef); err != nil {
			return registry.AssetResult{}, err
		}
		outputs.Outputs["source"] = "stamp"
	} else if req.LoadReq != nil {
		result, err := d.backend.LoadAndPush(ctx, *req.LoadReq)
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
		outputs.Logs = logs
		outputs.Outputs["source"] = "tar"
	} else {
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
		outputs.Logs = logs
		outputs.Outputs["source"] = "build"
	}
	return outputs, nil
}

func (d *ImageBuildDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

type preparedImageBuildRequest struct {
	ImageBuildRequest
	LoadReq    *ImageLoadRequest
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
	if strings.TrimSpace(spec.ImageTar) != "" {
		return d.buildTarRequest(ctx, in, spec)
	}
	return d.buildSourceRequest(ctx, in, spec)
}

func (d *ImageBuildDriver) buildTarRequest(ctx context.Context, in registry.AssetInput, spec *assetdefs.ImageBuildSpec) (preparedImageBuildRequest, func(), error) {
	tarPath, cleanup, err := imagebuildutil.StageTarFile(ctx, in.Store, spec.ImageTar)
	if err != nil {
		return preparedImageBuildRequest{}, func() {}, err
	}
	tarContent, err := in.Store.ReadFile(ctx, strings.TrimSpace(spec.ImageTar))
	if err != nil {
		cleanup()
		return preparedImageBuildRequest{}, func() {}, fmt.Errorf("read image tar for hash: %w", err)
	}
	registryHost := strings.TrimSpace(spec.Registry)
	if registryHost == "" {
		registryHost = strings.TrimSpace(d.defaultRegistry)
	}
	if registryHost == "" {
		cleanup()
		return preparedImageBuildRequest{}, func() {}, fmt.Errorf("image build asset %q requires registry or GUARDIAN_IMAGE_BUILD_REGISTRY", in.Asset.Name)
	}
	tag := "sha256-" + desiredImageTarHash(in, spec, tarContent)[:16]
	imageRef := registryHost + "/" + strings.TrimSpace(spec.Repository) + ":" + tag
	return preparedImageBuildRequest{
		ImageBuildRequest: ImageBuildRequest{ImageRef: imageRef},
		LoadReq: &ImageLoadRequest{
			TarPath:     tarPath,
			ImageRef:    imageRef,
			SourceImage: strings.TrimSpace(spec.SourceImage),
		},
		Repository: strings.TrimSpace(spec.Repository),
		Registry:   registryHost,
		Tag:        tag,
	}, cleanup, nil
}

func (d *ImageBuildDriver) buildSourceRequest(ctx context.Context, in registry.AssetInput, spec *assetdefs.ImageBuildSpec) (preparedImageBuildRequest, func(), error) {
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
		Base         string
		Spec         assetdefs.ImageBuildSpec
		Snapshots    []imagebuildutil.SourceFileSnapshot
		AssetVersion string
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
		Snapshots:    snapshots,
		AssetVersion: assetVersionFromInput(in),
	})
}

func desiredImageTarHash(in registry.AssetInput, spec *assetdefs.ImageBuildSpec, tarContent []byte) string {
	return digest.MustNormalizedHash(struct {
		Base         string
		Spec         assetdefs.ImageBuildSpec
		TarContent   string
		AssetVersion string
	}{
		Base: driverutil.AssetHash(in),
		Spec: assetdefs.ImageBuildSpec{
			Repository:  strings.TrimSpace(spec.Repository),
			Registry:    strings.TrimSpace(spec.Registry),
			ImageTar:    strings.TrimSpace(spec.ImageTar),
			SourceImage: strings.TrimSpace(spec.SourceImage),
		},
		TarContent:   string(tarContent),
		AssetVersion: assetVersionFromInput(in),
	})
}

func assetVersionFromInput(in registry.AssetInput) string {
	if in.AssetVersions == nil {
		return ""
	}
	return in.AssetVersions[in.Asset.Name]
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
