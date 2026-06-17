package dockerdriver

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

type ImageBuildBackendAPI interface {
	BuildAndPublish(ctx context.Context, req ImageBuildRequest) (ImageBuildResult, error)
	LoadAndPush(ctx context.Context, req ImageLoadRequest) (ImageBuildResult, error)
	StampImage(ctx context.Context, currentRef, newRef string) error
	ImageExists(ctx context.Context, imageRef string) (bool, error)
}

type ImageBuildRequest struct {
	WorkspaceDir string
	Dockerfile   string
	ImageRef     string
	Target       string
	Platform     string
	BuildArgs    map[string]string
}

type ImageLoadRequest struct {
	TarPath     string
	ImageRef    string
	SourceImage string
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

func (b *ImageBuildBackend) LoadAndPush(ctx context.Context, req ImageLoadRequest) (ImageBuildResult, error) {
	if output, err := exec.CommandContext(ctx, "docker", "load", "-i", req.TarPath).CombinedOutput(); err != nil {
		return ImageBuildResult{}, fmt.Errorf("docker load failed: %w\n%s", err, string(output))
	}
	if output, err := exec.CommandContext(ctx, "docker", "tag", req.SourceImage, req.ImageRef).CombinedOutput(); err != nil {
		return ImageBuildResult{}, fmt.Errorf("docker tag %s -> %s failed: %w\n%s", req.SourceImage, req.ImageRef, err, string(output))
	}
	if output, err := exec.CommandContext(ctx, "docker", "push", req.ImageRef).CombinedOutput(); err != nil {
		return ImageBuildResult{}, fmt.Errorf("docker push %s failed: %w\n%s", req.ImageRef, err, string(output))
	}
	return ImageBuildResult{ImageRef: req.ImageRef}, nil
}

func (b *ImageBuildBackend) ImageExists(ctx context.Context, imageRef string) (bool, error) {
	if output, err := exec.CommandContext(ctx, "docker", "manifest", "inspect", imageRef).CombinedOutput(); err != nil {
		if strings.Contains(string(output), "no such manifest") || strings.Contains(string(output), "manifest unknown") || strings.Contains(string(output), "not found") {
			log.Printf("[ImageBuild] registry-check image=%s notFound (docker)", imageRef)
			return false, nil
		}
		log.Printf("[ImageBuild] registry-check image=%s dockerError: %v output=%s", imageRef, err, strings.TrimSpace(string(output)))
		return false, nil
	}
	return true, nil
}

func (b *ImageBuildBackend) StampImage(ctx context.Context, currentRef, newRef string) error {
	if output, err := exec.CommandContext(ctx, "docker", "tag", currentRef, newRef).CombinedOutput(); err != nil {
		return fmt.Errorf("docker tag %s -> %s failed: %w\n%s", currentRef, newRef, err, string(output))
	}
	if output, err := exec.CommandContext(ctx, "docker", "push", newRef).CombinedOutput(); err != nil {
		return fmt.Errorf("docker push %s failed: %w\n%s", newRef, err, string(output))
	}
	return nil
}
