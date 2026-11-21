package kubernetesdriver

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/driverutil"
	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/pkg/guardianapi"
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
	if req.BuildSource != "" {
		exists, checkErr := d.backend.ImageExists(ctx, req.ImageRef)
		if checkErr != nil {
			return taskdomain.DriftReport{}, checkErr
		}
		if !exists {
			log.Printf("[ImageBuild] drift asset=%s source=%s ref=%q not built", in.Asset.Name, req.BuildSource, req.ImageRef)
			return changedDrift(in.Asset.Name, "source image is not built"), nil
		}
		currentRef, refErr := currentImageRef(ctx, in)
		if refErr != nil {
			return taskdomain.DriftReport{}, refErr
		}
		if currentRef != req.ImageRef {
			log.Printf("[ImageBuild] drift asset=%s source=%s currentRef=%q desiredRef=%q", in.Asset.Name, req.BuildSource, currentRef, req.ImageRef)
			return changedDrift(in.Asset.Name, "source revision changed"), nil
		}
		return inSyncDrift(in.Asset.Name, "source image is up to date"), nil
	}
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
	req, cleanup, err := d.buildRequest(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()
	currentRef, err := currentImageRef(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	if req.BuildSource != "" {
		currentRef = req.ImageRef
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

	source := "registry"
	if req.BuildSource != "" {
		source = "buildkit"
	}
	outputs := registry.AssetResult{Outputs: map[string]string{
		"imageRef":   req.ImageRef,
		"repository": req.Repository,
		"registry":   req.Registry,
		"tag":        req.Tag,
		"source":     source,
	}}

	if exists, checkErr := d.backend.ImageExists(ctx, req.ImageRef); checkErr == nil && exists {
		return outputs, nil
	}

	if req.BuildSource == "" {
		return registry.AssetResult{}, fmt.Errorf("image %q not found in registry — run 'guardianctl image release' first to build and push", req.ImageRef)
	}
	workspaceDir, workspaceCleanup, err := stageBuildContext(ctx, in.Store, req.BuildSource)
	if err != nil {
		return registry.AssetResult{}, err
	}
	defer workspaceCleanup()

	result, err := d.backend.BuildAndPublish(ctx, ImageBuildRequest{
		WorkspaceDir: workspaceDir,
		Dockerfile:   req.Dockerfile,
		ImageRef:     req.ImageRef,
		Target:       req.Target,
		Platform:     req.Platform,
		BuildArgs:    req.BuildArgs,
		Insecure:     req.Insecure,
	})
	if err != nil {
		return registry.AssetResult{}, err
	}
	if strings.TrimSpace(result.ImageRef) != "" {
		outputs.Outputs["imageRef"] = result.ImageRef
	}
	return outputs, nil
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
	BuildSource     string
	SourceCommit    string
	Dockerfile      string
	Target          string
	Platform        string
	BuildArgs       map[string]string
	Insecure        bool
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

	if strings.TrimSpace(spec.BuildSource) != "" {
		displayPath, err := imageBuildDisplayPath(spec.BuildSource)
		if err != nil {
			return preparedImageBuildRequest{}, func() {}, err
		}
		commit, err := repoRevision(ctx, in.Store, displayPath)
		if err != nil {
			return preparedImageBuildRequest{}, func() {}, fmt.Errorf("resolve source revision for %s: %w", displayPath, err)
		}
		tag := strings.TrimSpace(spec.BuildTag)
		if tag == "" {
			tag = shortImageTag(commit)
		}
		imageRef := strings.TrimSpace(spec.Repository) + ":" + tag
		if registryHost != "" {
			imageRef = registryHost + "/" + imageRef
		}
		insecure := spec.Insecure != nil && *spec.Insecure
		return preparedImageBuildRequest{
			ImageRef:        imageRef,
			Repository:      strings.TrimSpace(spec.Repository),
			Registry:        registryHost,
			Tag:             tag,
			ObserveExisting: spec.ObserveExisting,
			BuildSource:     displayPath,
			SourceCommit:    strings.TrimSpace(commit),
			Dockerfile:      firstNonEmpty(spec.Dockerfile, "Dockerfile"),
			Target:          strings.TrimSpace(spec.Target),
			Platform:        strings.TrimSpace(spec.Platform),
			BuildArgs:       cloneBuildArgs(spec.BuildArgs),
			Insecure:        insecure,
		}, func() {}, nil
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

func currentImageRef(_ context.Context, in registry.AssetInput) (string, error) {
	return strings.TrimSpace(in.Outputs[in.Asset.Name+".imageRef"]), nil
}

// repoRevisionReader is implemented by the MonoFS store adapter. It returns the
// commit hash currently projected at /.repos/<display-path>.
type repoRevisionReader interface {
	RepoCommit(ctx context.Context, displayPath string) (string, error)
}

func repoRevision(ctx context.Context, store guardianapi.ReadStore, displayPath string) (string, error) {
	if store == nil {
		return "", fmt.Errorf("build source requires a readable store")
	}
	reader, ok := store.(repoRevisionReader)
	if !ok {
		return "", fmt.Errorf("store does not expose repository revisions")
	}
	commit, err := reader.RepoCommit(ctx, displayPath)
	if err != nil {
		return "", err
	}
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return "", fmt.Errorf("repository %q has no commit hash", displayPath)
	}
	return commit, nil
}

func imageBuildDisplayPath(source string) (string, error) {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "/.repos/") {
		source = strings.TrimPrefix(source, "/.repos/")
	} else if strings.HasPrefix(source, ".repos/") {
		source = strings.TrimPrefix(source, ".repos/")
	} else {
		return "", fmt.Errorf("buildSource %q must be a /.repos/ path", source)
	}
	source = strings.Trim(source, "/")
	if source == "" || strings.Contains(source, "..") {
		return "", fmt.Errorf("buildSource %q is not a valid repository path", source)
	}
	return source, nil
}

func shortImageTag(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func cloneBuildArgs(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// stageBuildContext copies a MonoFS repository namespace into a local
// directory so the BuildKit backend can archive it.
func stageBuildContext(ctx context.Context, store guardianapi.ReadStore, source string) (string, func(), error) {
	if store == nil {
		return "", func() {}, fmt.Errorf("build source requires a readable store")
	}
	source = "/.repos/" + strings.Trim(source, "/")
	root, err := os.MkdirTemp("", "guardian-imagebuild-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create build context: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(root) }

	var walk func(logicalDir, relDir string) error
	walk = func(logicalDir, relDir string) error {
		entries, err := store.ListDir(ctx, logicalDir)
		if err != nil {
			return fmt.Errorf("list %s: %w", logicalDir, err)
		}
		for _, entry := range entries {
			name := strings.TrimSpace(entry.Name)
			if name == "" || name == ".git" {
				continue
			}
			logical := path.Join(logicalDir, name)
			rel := filepath.Join(relDir, name)
			target := filepath.Join(root, rel)
			if entry.IsDir {
				if err := os.MkdirAll(target, 0o755); err != nil {
					return fmt.Errorf("create %s: %w", target, err)
				}
				if err := walk(logical, rel); err != nil {
					return err
				}
				continue
			}
			content, err := store.ReadFile(ctx, logical)
			if err != nil {
				return fmt.Errorf("read %s: %w", logical, err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
			}
			mode := os.FileMode(0o644)
			if executableBuildFile(filepath.ToSlash(rel)) {
				mode = 0o755
			}
			if err := os.WriteFile(target, content, mode); err != nil {
				return fmt.Errorf("write %s: %w", target, err)
			}
		}
		return nil
	}
	if err := walk(source, "."); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return root, cleanup, nil
}

func executableBuildFile(rel string) bool {
	if strings.HasSuffix(rel, ".sh") {
		return true
	}
	for _, prefix := range []string{"scripts/", "bin/", "hooks/"} {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}
