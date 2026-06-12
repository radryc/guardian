package dockerdriver

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
)

type ImageBuildBackendAPI interface {
	BuildAndPublish(ctx context.Context, req ImageBuildRequest) (ImageBuildResult, error)
}

type ImageBuildRequest struct {
	WorkspaceDir string
	Dockerfile   string
	ImageRef     string
	Target       string
	Platform     string
	BuildArgs    map[string]string
}

type ImageBuildResult struct {
	ImageRef string
}

type ImageBuildBackend struct{}

func NewImageBuildBackend() *ImageBuildBackend {
	return &ImageBuildBackend{}
}

func (b *ImageBuildBackend) BuildAndPublish(ctx context.Context, req ImageBuildRequest) (ImageBuildResult, error) {
	buildArgs := make([]string, 0, len(req.BuildArgs))
	keys := make([]string, 0, len(req.BuildArgs))
	for key := range req.BuildArgs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		buildArgs = append(buildArgs, "--build-arg", fmt.Sprintf("%s=%s", key, req.BuildArgs[key]))
	}
	args := []string{"build", "-t", req.ImageRef, "-f", req.Dockerfile}
	if req.Target != "" {
		args = append(args, "--target", req.Target)
	}
	if req.Platform != "" {
		args = append(args, "--platform", req.Platform)
	}
	args = append(args, buildArgs...)
	args = append(args, req.WorkspaceDir)
	if output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		return ImageBuildResult{}, fmt.Errorf("docker build %s failed: %w\n%s", req.ImageRef, err, string(output))
	}
	if output, err := exec.CommandContext(ctx, "docker", "push", req.ImageRef).CombinedOutput(); err != nil {
		return ImageBuildResult{}, fmt.Errorf("docker push %s failed: %w\n%s", req.ImageRef, err, string(output))
	}
	return ImageBuildResult{ImageRef: req.ImageRef}, nil
}
