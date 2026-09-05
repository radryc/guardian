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
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
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
	if req.ObserveExisting {
		exists, checkErr := d.backend.ImageExists(ctx, req.ImageRef)
		if checkErr != nil {
			return taskdomain.DriftReport{}, checkErr
		}
		if exists {
			return inSyncDrift(in.Asset.Name, "k8s image exists in registry"), nil
		}
		log.Printf("[ImageBuild] drift asset=%s observeExisting ref=%q not found", in.Asset.Name, req.ImageRef)
		return changedDrift(in.Asset.Name, "k8s image not found in registry"), nil
	}
	currentRef, err := currentImageRef(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if currentRef == req.ImageRef {
		return inSyncDrift(in.Asset.Name, "k8s image build is in sync"), nil
	}
	log.Printf("[ImageBuild] drift asset=%s currentRef=%q desiredRef=%q", in.Asset.Name, currentRef, req.ImageRef)
	return changedDrift(in.Asset.Name, "k8s image build differs"), nil
}

func (d *ImageBuildDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	currentRef, err := currentImageRef(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	if currentRef == "" {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "no prior image reference"}, &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "image is ready for first push"}, nil
	}
	exists, err := d.backend.ImageExists(ctx, currentRef)
	if err != nil {
		return nil, nil, err
	}
	if exists {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "k8s image exists in registry"}, &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "image is ready"}, nil
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "k8s image not found in registry"}, &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "image is missing"}, nil
}

func (d *ImageBuildDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	req, cleanup, err := d.buildRequest(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	defer cleanup()

	outputs := registry.AssetResult{Outputs: map[string]string{
		"imageRef":   req.ImageRef,
		"repository": req.Repository,
		"registry":   req.Registry,
		"tag":        req.Tag,
		"source":     "registry",
	}}

	if exists, checkErr := d.backend.ImageExists(ctx, req.ImageRef); checkErr == nil && exists {
		return outputs, nil
	}

	return registry.AssetResult{}, fmt.Errorf("image %q not found in registry — run 'guardianctl image release' first to build and push", req.ImageRef)
}

func (d *ImageBuildDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

type preparedImageBuildRequest struct {
	ImageRef        string
	Repository      string
	Registry        string
	Tag             string
	ObserveExisting bool
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

	registryHost := strings.TrimSpace(spec.Registry)
	if registryHost == "" {
		registryHost = strings.TrimSpace(d.defaultRegistry)
	}

	imageRef := strings.TrimSpace(spec.SourceImage)
	tag := ""

	if imageRef != "" {
		if !strings.Contains(imageRef, "/") && registryHost != "" {
			imageRef = registryHost + "/" + imageRef
		}
		if idx := strings.LastIndex(imageRef, ":"); idx >= 0 {
			tag = imageRef[idx+1:]
		}
	} else {
		tag = in.AssetVersions[in.Asset.Name]
		if tag == "" {
			return preparedImageBuildRequest{}, func() {}, fmt.Errorf("image build asset %q has no sourceImage and no version tag", in.Asset.Name)
		}
		imageRef = strings.TrimSpace(spec.Repository) + ":" + tag
		if registryHost != "" {
			imageRef = registryHost + "/" + imageRef
		}
	}

	return preparedImageBuildRequest{
		ImageRef:        imageRef,
		Repository:      strings.TrimSpace(spec.Repository),
		Registry:        registryHost,
		Tag:             tag,
		ObserveExisting: spec.ObserveExisting,
	}, func() {}, nil
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
