package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
)

func imageReleaseCommand() *command.Command {
	flags := flag.NewFlagSet("image release", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dirFlag := flags.String("dir", "", "path to partition directory")
	allFlag := flags.Bool("all", false, "release images for all partitions")
	registryFlag := flags.String("registry", "", "override target registry")
	tarFlag := flags.Bool("tar", false, "export images as tar files instead of pushing")
	dryRun := flags.Bool("dry-run", false, "print commands without executing")

	return &command.Command{
		Description: "Build, push, and stamp images for a partition (or all partitions)",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			if *allFlag {
				return imageReleaseAll(ctx, *registryFlag, *tarFlag, *dryRun)
			}

			if *dirFlag == "" {
				return fmt.Errorf("--dir is required (or use --all)")
			}

			dir := filepath.Clean(*dirFlag)
			return imageReleaseOne(ctx, dir, *registryFlag, *tarFlag, *dryRun)
		},
	}
}

func imageReleaseOne(ctx context.Context, dir, registry string, useTar, dryRun bool) error {
	fmt.Fprintf(os.Stderr, "=== building images in %s ===\n", dir)

	tag := resolveDevTag(ctx)
	if err := runImageBuildDir(ctx, dir, dryRun, tag); err != nil {
		return fmt.Errorf("build: %w", err)
	}

	fmt.Fprintf(os.Stderr, "=== pushing images ===\n")
	if err := runImagePushDir(ctx, dir, registry, dryRun); err != nil {
		return fmt.Errorf("push: %w", err)
	}

	fmt.Fprintf(os.Stderr, "=== stamping image refs ===\n")
	if err := runImageStampDir(ctx, dir, dryRun); err != nil {
		return fmt.Errorf("stamp: %w", err)
	}

	fmt.Fprintf(os.Stderr, "=== %s done ===\n", dir)
	return nil
}

func imageReleaseAll(ctx context.Context, registry string, useTar, dryRun bool) error {
	partitions := []string{
		"_bootstrap",
		"agent", "doctor", "guardian-configs", "k8s-top",
		"monitoring", "dev-workspace", "opentelemetry", "lb-agent", "lolipop",
	}

	for _, name := range partitions {
		dir := findPartitionDir(name)
		if dir == "" {
			fmt.Fprintf(os.Stderr, "skipping %s: partition dir not found\n", name)
			continue
		}
		assets, err := loadImageBuildAssets(dir)
		if err != nil || len(assets) == 0 {
			fmt.Fprintf(os.Stderr, "skipping %s: no ImageBuild assets (buildContext or imageFromUpstream)\n", name)
			continue
		}
		if err := imageReleaseOne(ctx, dir, registry, useTar, dryRun); err != nil {
			return fmt.Errorf("partition %s: %w", name, err)
		}
		fmt.Fprintln(os.Stderr)
	}
	return nil
}
