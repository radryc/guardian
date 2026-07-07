package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
)

var dockerExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

func imageBuildCommand() *command.Command {
	flags := flag.NewFlagSet("image build", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dirFlag := flags.String("dir", "", "path to partition directory")
	dryRun := flags.Bool("dry-run", false, "print commands without executing")

	return &command.Command{
		Description: "Build Docker images defined in partition ImageBuild intents",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			if *dirFlag == "" {
				return fmt.Errorf("--dir is required")
			}
			dir := filepath.Clean(*dirFlag)

			assets, err := loadImageBuildAssets(dir)
			if err != nil {
				return err
			}
			if len(assets) == 0 {
				fmt.Fprintf(os.Stderr, "  no buildable ImageBuild assets in %s — skipping\n", dir)
				return nil
			}

			state := &imageState{Images: map[string]imageEntry{}}

			for _, asset := range assets {
				if asset.ImageFromUpstream != "" {
					tag := asset.Repository + ":latest"
					if asset.SourceImage != "" {
						tag = asset.SourceImage
					}

					if *dryRun {
						fmt.Fprintf(os.Stderr, "+ docker pull %s\n", asset.ImageFromUpstream)
						fmt.Fprintf(os.Stderr, "+ docker tag %s %s\n", asset.ImageFromUpstream, tag)
					} else {
						fmt.Fprintf(os.Stderr, "+ docker pull %s\n", asset.ImageFromUpstream)
						cmd := dockerExecCommand(ctx, "docker", "pull", asset.ImageFromUpstream)
						cmd.Stdout = os.Stdout
						cmd.Stderr = os.Stderr
						if err := cmd.Run(); err != nil {
							return fmt.Errorf("docker pull %s: %w", asset.ImageFromUpstream, err)
						}

						fmt.Fprintf(os.Stderr, "+ docker tag %s %s\n", asset.ImageFromUpstream, tag)
						cmd = dockerExecCommand(ctx, "docker", "tag", asset.ImageFromUpstream, tag)
						if err := cmd.Run(); err != nil {
							return fmt.Errorf("docker tag %s -> %s: %w", asset.ImageFromUpstream, tag, err)
						}
					}

					fmt.Fprintf(os.Stderr, "  pulled %s\n", tag)
					fmt.Fprintln(os.Stdout, tag)

					state.Images[asset.AssetName] = imageEntry{
						AssetName:   asset.AssetName,
						IntentFile:  asset.IntentFile,
						LocalTag:    tag,
						Repository:  asset.Repository,
						Registry:    asset.Registry,
						SourceImage: asset.SourceImage,
					}
					continue
				}
				if asset.BuildContext == "" {
					continue
				}
				buildContext := expandBuildContext(asset.BuildContext)
				dockerfile := resolveDockerfile(asset.Dockerfile, buildContext, dir)
				tag := asset.Repository + ":latest"
				if _, commit, _ := gitVersion(buildContext); commit != "" {
					tag = asset.Repository + ":" + commit
				}

				buildArgs := make(map[string]string)
				for k, v := range asset.BuildArgs {
					buildArgs[k] = v
				}
				version, commit, buildTime := gitVersion(buildContext)
				if version == "" {
					version = commit
				}
				if version != "" {
					buildArgs["VERSION"] = version
				}
				if commit != "" {
					buildArgs["COMMIT"] = commit
				}
				if buildTime != "" {
					buildArgs["BUILD_TIME"] = buildTime
				}

				buildArgs = resolveImageBuildArgs(buildArgs, state)
				for k, v := range buildArgs {
					if intentOutputRe.MatchString(v) {
						dep := intentOutputRe.FindStringSubmatch(v)[1]
						return fmt.Errorf("asset %q: build arg %q has unresolved placeholder %q — ensure asset %q is listed before this one in images.yaml",
							asset.AssetName, k, v, dep)
					}
				}

				dockerArgs := []string{"build", "-t", tag, "-f", dockerfile}
				if asset.Target != "" {
					dockerArgs = append(dockerArgs, "--target", asset.Target)
				}
				if asset.Platform != "" {
					dockerArgs = append(dockerArgs, "--platform", asset.Platform)
				}
				for _, key := range sortedKeys(buildArgs) {
					dockerArgs = append(dockerArgs, "--build-arg", key+"="+buildArgs[key])
				}
				for _, key := range sortedKeys(asset.BuildContexts) {
					ctxPath := expandBuildContext(asset.BuildContexts[key])
					dockerArgs = append(dockerArgs, "--build-context", key+"="+ctxPath)
				}
				dockerArgs = append(dockerArgs, buildContext)

				if *dryRun {
					fmt.Fprintf(os.Stderr, "+ docker %s\n", strings.Join(dockerArgs, " "))
				} else {
					fmt.Fprintf(os.Stderr, "+ docker %s\n", strings.Join(dockerArgs, " "))
					cmd := dockerExecCommand(ctx, "docker", dockerArgs...)
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					if err := cmd.Run(); err != nil {
						return fmt.Errorf("docker build %s: %w", tag, err)
					}
				}

				fmt.Fprintf(os.Stderr, "  built %s\n", tag)
				fmt.Fprintln(os.Stdout, tag)

				state.Images[asset.AssetName] = imageEntry{
					AssetName:     asset.AssetName,
					IntentFile:    asset.IntentFile,
					LocalTag:      tag,
					Repository:    asset.Repository,
					Registry:      asset.Registry,
					Dockerfile:    dockerfile,
					Context:       buildContext,
					BuildArgs:     buildArgs,
					Target:        asset.Target,
					Platform:      asset.Platform,
					BuildContexts: asset.BuildContexts,
					SourceImage:   asset.SourceImage,
				}
			}

			return saveImageState(dir, state)
		},
	}
}
