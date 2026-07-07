package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
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
			dir := filepath.Clean(*dirFlag)

			state, err := loadImageState(dir)
			if err != nil {
				return fmt.Errorf("load image state: %w (did you run 'guardianctl image build' first?)", err)
			}

			for name, entry := range state.Images {
				reg := entry.Registry
				if *registryFlag != "" {
					reg = *registryFlag
				}
				if reg == "" {
					return fmt.Errorf("no registry specified for %s (set --registry or add registry to asset)", name)
				}

				tag := computeImageTag(ctx, entry.LocalTag)
				entry.Tag = tag
				imageRef := reg + "/" + entry.Repository + ":" + tag
				entry.ImageRef = imageRef

				cmd := dockerExecCommand(ctx, "docker", "tag", entry.LocalTag, imageRef)
				if *dryRun {
					fmt.Fprintf(os.Stderr, "+ docker tag %s %s\n", entry.LocalTag, imageRef)
				} else {
					fmt.Fprintf(os.Stderr, "+ docker tag %s %s\n", entry.LocalTag, imageRef)
					if err := cmd.Run(); err != nil {
						return fmt.Errorf("docker tag %s -> %s: %w", entry.LocalTag, imageRef, err)
					}
				}

				cmd = dockerExecCommand(ctx, "docker", "push", imageRef)
				if *dryRun {
					fmt.Fprintf(os.Stderr, "+ docker push %s\n", imageRef)
				} else {
					fmt.Fprintf(os.Stderr, "+ docker push %s\n", imageRef)
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					if err := cmd.Run(); err != nil {
						return fmt.Errorf("docker push %s: %w", imageRef, err)
					}
				}
				fmt.Fprintf(os.Stderr, "  pushed %s\n", imageRef)
				fmt.Fprintln(os.Stdout, imageRef)

				state.Images[name] = entry
			}

			return saveImageState(dir, state)
		},
	}
}

func computeImageTag(ctx context.Context, localTag string) string {
	cmd := dockerExecCommand(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", localTag)
	out, err := cmd.Output()
	if err != nil {
		cmd := dockerExecCommand(ctx, "docker", "image", "inspect", "--format", "{{index .RepoDigests 0}}", localTag)
		out, err = cmd.Output()
		if err != nil {
			return "latest"
		}
		ref := strings.TrimSpace(string(out))
		parts := strings.SplitN(ref, "@", 2)
		if len(parts) == 2 {
			digest := strings.TrimPrefix(parts[1], "sha256:")
			if len(digest) >= 16 {
				digest = digest[:16]
			}
			return "sha256-" + digest
		}
		parts = strings.SplitN(ref, ":", 2)
		if len(parts) == 2 {
			return parts[1]
		}
		return "latest"
	}
	imageID := strings.TrimSpace(string(out))
	imageID = strings.TrimPrefix(imageID, "sha256:")
	if len(imageID) >= 16 {
		imageID = imageID[:16]
	}
	return "sha256-" + imageID
}
