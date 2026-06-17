package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rydzu/ainfra/guardian/internal/bootstrap"
	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
)

func releaseCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("release", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	partition := flags.String("partition", "", "partition name to release")
	allFlag := flags.Bool("all", false, "release all partitions")
	dirFlag := flags.String("dir", "", "path to partition directory")
	bump := flags.Bool("bump", false, "bump asset versions before releasing")
	waitFlag := flags.Bool("wait", false, "wait for partition convergence")
	reconcile := flags.Bool("reconcile", false, "run reconcile after push")
	skipBuild := flags.Bool("skip-build", false, "skip image build")
	skipPush := flags.Bool("skip-push", false, "skip image push")
	skipStamp := flags.Bool("skip-stamp", false, "skip image stamp")
	skipGuardian := flags.Bool("skip-guardian", false, "skip guardianctl push")
	dryRun := flags.Bool("dry-run", false, "print commands without executing")

	return &command.Command{
		Description: "Release a partition (build images -> stamp -> push -> reconcile)",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			if *allFlag {
				return releaseAll(ctx, printer, *bump, *waitFlag, *reconcile, *skipBuild, *skipPush, *skipStamp, *skipGuardian, *dryRun)
			}

			if *partition == "" && *dirFlag == "" {
				return fmt.Errorf("--partition or --dir is required (or use --all)")
			}

			partDir := *dirFlag
			partName := *partition
			if partDir == "" {
				partDir = findPartitionDir(partName)
			}
			if partName == "" {
				partName = partitionNameFromDir(partDir)
			}
			if partDir == "" {
				return fmt.Errorf("partition directory not found (set ST_ROOT or use --dir)")
			}

			return releaseOne(ctx, printer, partName, partDir, *bump, *waitFlag, *reconcile, *skipBuild, *skipPush, *skipStamp, *skipGuardian, *dryRun)
		},
	}
}

func imageCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("image", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	partitionFlag := flags.String("partition", "", "partition name")
	dirFlag := flags.String("dir", "", "path to partition directory")
	dryRun := flags.Bool("dry-run", false, "print commands without executing")

	return &command.Command{
		Description: "Build, push, and stamp container images for a partition",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			partDir := *dirFlag
			partName := *partitionFlag

			// Extract flags that may appear after positional args (e.g. "build --dir PATH")
			remaining := make([]string, 0, len(args))
			for i := 0; i < len(args); i++ {
				switch args[i] {
				case "--dir":
					if i+1 < len(args) {
						partDir = args[i+1]
						i++
					}
				case "--partition":
					if i+1 < len(args) {
						partName = args[i+1]
						i++
					}
				case "--dry-run":
					*dryRun = true
				default:
					remaining = append(remaining, args[i])
				}
			}
			args = remaining

			if partDir == "" && partName != "" {
				partDir = findPartitionDir(partName)
			}
			if partDir == "" {
				return fmt.Errorf("--dir or --partition is required")
			}

			for _, step := range args {
				switch step {
				case "build":
					return imageBuild(ctx, partDir, *dryRun)
				case "push":
					return imagePush(ctx, partDir, *dryRun)
				case "stamp":
					return imageStamp(ctx, partDir, *dryRun)
				default:
					return fmt.Errorf("unknown image step: %s (expected: build, push, stamp)", step)
				}
			}

			// Default: build + push + stamp
			if err := imageBuild(ctx, partDir, *dryRun); err != nil {
				return err
			}
			if err := imagePush(ctx, partDir, *dryRun); err != nil {
				return err
			}
			return imageStamp(ctx, partDir, *dryRun)
		},
	}
}

func releaseOne(ctx context.Context, printer *output.Printer, name, dir string, bump, wait, reconcile, skipBuild, skipPush, skipStamp, skipGuardian, dryRun bool) error {
	fmt.Fprintf(os.Stderr, "=== releasing partition %s ===\n", name)

	// Bump versions
	if bump {
		fmt.Fprintf(os.Stderr, "=== bumping versions ===\n")
		if err := bootstrap.Run(ctx, dryRun, "guardianctl", "partition", "tag", "--dir", dir); err != nil {
			return fmt.Errorf("bump: %w", err)
		}
	}

	// Build and stamp images
	if !skipBuild {
		fmt.Fprintf(os.Stderr, "=== building images ===\n")
		if err := imageBuild(ctx, dir, dryRun); err != nil {
			return fmt.Errorf("image build: %w", err)
		}
	}
	if !skipPush {
		if err := imagePush(ctx, dir, dryRun); err != nil {
			return fmt.Errorf("image push: %w", err)
		}
	}
	if !skipStamp {
		if err := imageStamp(ctx, dir, dryRun); err != nil {
			return fmt.Errorf("image stamp: %w", err)
		}
	}

	// Push to Guardian
	if !skipGuardian {
		fmt.Fprintf(os.Stderr, "=== pushing partition to Guardian ===\n")
		if err := bootstrap.Run(ctx, dryRun, "guardianctl", "partition", "push", "--dir", dir); err != nil {
			return fmt.Errorf("partition push: %w", err)
		}

		// Reconcile
		if reconcile {
			fmt.Fprintf(os.Stderr, "=== reconciling partition %s ===\n", name)
			if err := bootstrap.Run(ctx, dryRun, "guardianctl", "partition", "reconcile", "--partition", name); err != nil {
				return fmt.Errorf("partition reconcile: %w", err)
			}
		}

		// Wait
		if wait {
			fmt.Fprintf(os.Stderr, "=== waiting for partition %s ===\n", name)
			if err := bootstrap.Run(ctx, dryRun, "guardianctl", "partition", "wait", "--partition", name); err != nil {
				return fmt.Errorf("partition wait: %w", err)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "=== partition %s released ===\n", name)
	return nil
}

func releaseAll(ctx context.Context, printer *output.Printer, bump, wait, reconcile, skipBuild, skipPush, skipStamp, skipGuardian, dryRun bool) error {
	partitions := []string{
		"guardian-configs", "opentelemetry", "k8s-top",
		"doctor", "monitoring", "dev-workspace", "agent", "lb-agent", "lolipop",
	}

	for _, name := range partitions {
		dir := findPartitionDir(name)
		if dir == "" {
			fmt.Fprintf(os.Stderr, "skipping %s: partition dir not found\n", name)
			continue
		}
		if err := releaseOne(ctx, printer, name, dir, bump, wait, reconcile, skipBuild, skipPush, skipStamp, skipGuardian, dryRun); err != nil {
			return fmt.Errorf("partition %s: %w", name, err)
		}
		fmt.Fprintln(os.Stderr)
	}
	return nil
}

func imageBuild(ctx context.Context, partitionDir string, dryRun bool) error {
	root := stratatoolsRoot(partitionDir)
	if root == "" {
		return fmt.Errorf("cannot find stratatools root; set ST_ROOT or run from within the repo")
	}
	return bootstrap.Run(ctx, dryRun, "uv", "run", "--directory", root, "st-image", "build", "--partition", filepath.Base(partitionDir))
}

func imagePush(ctx context.Context, partitionDir string, dryRun bool) error {
	root := stratatoolsRoot(partitionDir)
	if root == "" {
		return fmt.Errorf("cannot find stratatools root; set ST_ROOT or run from within the repo")
	}
	return bootstrap.Run(ctx, dryRun, "uv", "run", "--directory", root, "st-image", "push", "--partition", filepath.Base(partitionDir))
}

func imageStamp(ctx context.Context, partitionDir string, dryRun bool) error {
	root := stratatoolsRoot(partitionDir)
	if root == "" {
		return fmt.Errorf("cannot find stratatools root; set ST_ROOT or run from within the repo")
	}
	return bootstrap.Run(ctx, dryRun, "uv", "run", "--directory", root, "st-image", "stamp", "--partition", filepath.Base(partitionDir))
}

func stratatoolsRoot(partitionDir string) string {
	if partitionDir != "" {
		// partitionDir = .../stratatools/partitions/<name>
		// Walk up two levels to find stratatools root
		partsDir := filepath.Dir(partitionDir)
		if filepath.Base(partsDir) == "partitions" {
			root := filepath.Dir(partsDir)
			if _, err := os.Stat(filepath.Join(root, "pyproject.toml")); err == nil {
				return root
			}
		}
	}
	return findRootDir()
}

func findPartitionDir(name string) string {
	root := findRootDir()
	if root != "." {
		dir := filepath.Join(root, "partitions", name)
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}
	// Fallback: walk up from CWD looking for partitions/<name>
	cwd, _ := os.Getwd()
	for dir := cwd; ; {
		for _, pattern := range []string{
			filepath.Join(dir, "partitions", name),
			filepath.Join(dir, "stratatools", "partitions", name),
			filepath.Join(dir, "strata", "stratatools", "partitions", name),
			filepath.Join(dir, "projects", "stratatools", "partitions", name),
			filepath.Join(dir, "src", "stratatools", "partitions", name),
		} {
			if _, err := os.Stat(pattern); err == nil {
				return pattern
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func partitionNameFromDir(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Base(dir)
}

// Ensure release/image commands are registered.
var _ = func() bool {
	// Registration happens in releaseCommands()
	return true
}()
