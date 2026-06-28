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
	tarFlag := flags.Bool("tar", false, "export images as tar files instead of pushing to registry")
	dryRun := flags.Bool("dry-run", false, "print commands without executing")

	return &command.Command{
		Description: "Push built images to a registry or export as tar files",
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
				registry := entry.Registry
				if *registryFlag != "" {
					registry = *registryFlag
				}
				if registry == "" && !*tarFlag {
					return fmt.Errorf("no registry specified for %s (set --registry or add registry to asset)", name)
				}

				tag := computeImageTag(ctx, entry.LocalTag)
				entry.Tag = tag

				if *tarFlag {
					tarDir := filepath.Join(dir, "payloads", "images")
					if err := os.MkdirAll(tarDir, 0755); err != nil {
						return err
					}
					tarPath := filepath.Join(tarDir, entry.Repository+".tar")
					entry.TarPath = tarPath
					entry.SourceImage = entry.Repository + ":" + tag

					dockerArgs := []string{"save", "-o", tarPath, entry.LocalTag}
					if *dryRun {
						fmt.Fprintf(os.Stderr, "+ docker %s\n", strings.Join(dockerArgs, " "))
					} else {
						fmt.Fprintf(os.Stderr, "+ docker %s\n", strings.Join(dockerArgs, " "))
						cmd := dockerExecCommand(ctx, "docker", dockerArgs...)
						cmd.Stdout = os.Stdout
						cmd.Stderr = os.Stderr
						if err := cmd.Run(); err != nil {
							return fmt.Errorf("docker save %s: %w", entry.LocalTag, err)
						}
					}
					fmt.Fprintf(os.Stderr, "  exported %s\n", tarPath)
					fmt.Fprintln(os.Stdout, tarPath)
				} else {
					imageRef := registry + "/" + entry.Repository + ":" + tag
					entry.ImageRef = imageRef

					dockerTagArgs := []string{"tag", entry.LocalTag, imageRef}
					if *dryRun {
						fmt.Fprintf(os.Stderr, "+ docker %s\n", strings.Join(dockerTagArgs, " "))
					} else {
						fmt.Fprintf(os.Stderr, "+ docker %s\n", strings.Join(dockerTagArgs, " "))
						cmd := dockerExecCommand(ctx, "docker", dockerTagArgs...)
						cmd.Stderr = os.Stderr
						if err := cmd.Run(); err != nil {
							return fmt.Errorf("docker tag %s -> %s: %w", entry.LocalTag, imageRef, err)
						}
					}

					dockerPushArgs := []string{"push", imageRef}
					if *dryRun {
						fmt.Fprintf(os.Stderr, "+ docker %s\n", strings.Join(dockerPushArgs, " "))
					} else {
						fmt.Fprintf(os.Stderr, "+ docker %s\n", strings.Join(dockerPushArgs, " "))
						cmd := dockerExecCommand(ctx, "docker", dockerPushArgs...)
						cmd.Stdout = os.Stdout
						cmd.Stderr = os.Stderr
						if err := cmd.Run(); err != nil {
							return fmt.Errorf("docker push %s: %w", imageRef, err)
						}
					}
					fmt.Fprintf(os.Stderr, "  pushed %s\n", imageRef)
					fmt.Fprintln(os.Stdout, imageRef)
				}

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
