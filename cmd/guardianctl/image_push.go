package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/cli/command"
)

func imagePushCommand() *command.Command {
	flags := flag.NewFlagSet("image push", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dirFlag := flags.String("dir", "", "path to partition directory")
	registryFlag := flags.String("registry", "", "override target registry")
	dryRun := flags.Bool("dry-run", false, "print commands without executing")

	return &command.Command{
		Description: "Push built images to a registry",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			if *dirFlag == "" {
				return fmt.Errorf("--dir is required")
			}
			return pushImages(ctx, filepath.Clean(*dirFlag), *registryFlag, *dryRun, false)
		},
	}
}

// pushImages is the single image-push implementation used by both
// `guardianctl image push` and `image release`.
func pushImages(ctx context.Context, dir, registry string, dryRun, libraryMirror bool) error {
	state, err := loadImageState(dir)
	if err != nil {
		return fmt.Errorf("load image state: %w (did you run 'guardianctl image build' first?)", err)
	}

	for name, entry := range state.Images {
		reg := entry.Registry
		if registry != "" {
			reg = registry
		}
		if reg == "" {
			return fmt.Errorf("no registry specified for %s (set --registry or add registry to asset)", name)
		}

		tag, err := computeImageTag(ctx, entry.LocalTag)
		if err != nil && !dryRun {
			return err
		}
		if tag == "" {
			tag = "unknown"
		}
		entry.Tag = tag
		imageRef := reg + "/" + entry.Repository + ":" + tag
		entry.ImageRef = imageRef

		cmd := dockerExecCommand(ctx, "docker", "tag", entry.LocalTag, imageRef)
		if dryRun {
			fmt.Fprintf(os.Stderr, "+ docker tag %s %s\n", entry.LocalTag, imageRef)
		} else {
			fmt.Fprintf(os.Stderr, "+ docker tag %s %s\n", entry.LocalTag, imageRef)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("docker tag %s -> %s: %w", entry.LocalTag, imageRef, err)
			}
		}

		if err := runDockerPush(ctx, imageRef, dryRun, true); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "  pushed %s\n", imageRef)
		fmt.Fprintln(os.Stdout, imageRef)

		if libraryMirror {
			libraryImageRef := reg + "/library/" + entry.Repository + ":" + tag
			if dryRun {
				fmt.Fprintf(os.Stderr, "+ docker tag %s %s (for docker.io mirror)\n", entry.LocalTag, libraryImageRef)
				fmt.Fprintf(os.Stderr, "+ docker push %s\n", libraryImageRef)
			} else {
				cmd = dockerExecCommand(ctx, "docker", "tag", entry.LocalTag, libraryImageRef)
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("docker tag %s -> %s: %w", entry.LocalTag, libraryImageRef, err)
				}
				if err := runDockerPush(ctx, libraryImageRef, false, false); err != nil {
					fmt.Fprintf(os.Stderr, "  WARNING: docker push %s (library mirror) failed: %v\n", libraryImageRef, err)
				}
			}
		}

		state.Images[name] = entry
	}

	return saveImageState(dir, state)
}

func runDockerPush(ctx context.Context, imageRef string, dryRun, retry bool) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "+ docker push %s\n", imageRef)
		return nil
	}
	attempts := 1
	if retry {
		attempts = 5
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * 2 * time.Second
			fmt.Fprintf(os.Stderr, "  retrying push in %v (attempt %d/%d)...\n", wait, attempt+1, attempts)
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		cmd := dockerExecCommand(ctx, "docker", "push", imageRef)
		fmt.Fprintf(os.Stderr, "+ docker push %s\n", imageRef)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("docker push %s: %w", imageRef, lastErr)
	}
	return nil
}

// computeImageTag derives a content-addressed tag from the local image. It no
// longer silently falls back to "latest": a tag that cannot be resolved is an
// error, otherwise an immutable build could be overwritten.
func computeImageTag(ctx context.Context, localTag string) (string, error) {
	cmd := dockerExecCommand(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", localTag)
	out, err := cmd.Output()
	if err == nil {
		imageID := strings.TrimSpace(string(out))
		imageID = strings.TrimPrefix(imageID, "sha256:")
		if len(imageID) >= 16 {
			imageID = imageID[:16]
		}
		if imageID != "" {
			return "sha256-" + imageID, nil
		}
	}
	cmd = dockerExecCommand(ctx, "docker", "image", "inspect", "--format", "{{index .RepoDigests 0}}", localTag)
	out, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolve image tag for %s: %w", localTag, err)
	}
	ref := strings.TrimSpace(string(out))
	if parts := strings.SplitN(ref, "@", 2); len(parts) == 2 {
		digest := strings.TrimPrefix(parts[1], "sha256:")
		if len(digest) >= 16 {
			digest = digest[:16]
		}
		return "sha256-" + digest, nil
	}
	if parts := strings.SplitN(ref, ":", 2); len(parts) == 2 {
		return parts[1], nil
	}
	return "", fmt.Errorf("unable to derive a tag for image %s", localTag)
}
