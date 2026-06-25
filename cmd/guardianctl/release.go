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
	dryRun := flags.Bool("dry-run", false, "print commands without executing")
	monofsRouter := flags.String("monofs-router", os.Getenv("GUARDIAN_MONOFS_ROUTER"), "MonoFS router address for partition push")
	monofsToken := flags.String("monofs-token", os.Getenv("GUARDIAN_MONOFS_TOKEN"), "MonoFS token for partition push")

	return &command.Command{
		Description: "Release a partition: bump versions → push to Guardian → optional reconcile/wait",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			storeArgs := storeFlags(*monofsRouter, *monofsToken)
			if *allFlag {
				return releaseAll(ctx, printer, *bump, *waitFlag, *reconcile, *dryRun, storeArgs)
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

			return releaseOne(ctx, printer, partName, partDir, *bump, *waitFlag, *reconcile, *dryRun, storeArgs)
		},
	}
}

func releaseOne(ctx context.Context, printer *output.Printer, name, dir string, bump, wait, reconcile, dryRun bool, storeArgs []string) error {
	fmt.Fprintf(os.Stderr, "=== releasing partition %s ===\n", name)

	self := selfBinary()

	if bump {
		fmt.Fprintf(os.Stderr, "=== bumping versions ===\n")
		if err := bootstrap.Run(ctx, dryRun, self, "partition", "tag", "--dir", dir); err != nil {
			return fmt.Errorf("bump: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "=== pushing partition to Guardian ===\n")
	pushArgs := append([]string{self}, storeArgs...)
	pushArgs = append(pushArgs, "partition", "push", "--dir", dir)
	if err := bootstrap.Run(ctx, dryRun, pushArgs[0], pushArgs[1:]...); err != nil {
		return fmt.Errorf("partition push: %w", err)
	}

	if reconcile {
		fmt.Fprintf(os.Stderr, "=== reconciling partition %s ===\n", name)
		recArgs := append([]string{self}, storeArgs...)
		recArgs = append(recArgs, "partition", "reconcile", "--partition", name)
		if err := bootstrap.Run(ctx, dryRun, recArgs[0], recArgs[1:]...); err != nil {
			return fmt.Errorf("partition reconcile: %w", err)
		}
	}

	if wait {
		fmt.Fprintf(os.Stderr, "=== waiting for partition %s ===\n", name)
		waitArgs := append([]string{self}, storeArgs...)
		waitArgs = append(waitArgs, "partition", "wait", "--partition", name)
		if err := bootstrap.Run(ctx, dryRun, waitArgs[0], waitArgs[1:]...); err != nil {
			return fmt.Errorf("partition wait: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "=== partition %s released ===\n", name)
	return nil
}

func releaseAll(ctx context.Context, printer *output.Printer, bump, wait, reconcile, dryRun bool, storeArgs []string) error {
	partitions := []string{
		"guardian-configs", "opentelemetry", "k8s-top",
		"doctor", "monitoring", "dev-workspace", "agent", "lolipop",
	}

	for _, name := range partitions {
		dir := findPartitionDir(name)
		if dir == "" {
			fmt.Fprintf(os.Stderr, "skipping %s: partition dir not found\n", name)
			continue
		}
		if err := releaseOne(ctx, printer, name, dir, bump, wait, reconcile, dryRun, storeArgs); err != nil {
			return fmt.Errorf("partition %s: %w", name, err)
		}
		fmt.Fprintln(os.Stderr)
	}
	return nil
}

func findPartitionDir(name string) string {
	root := findRootDir()
	if root != "." {
		dir := filepath.Join(root, "partitions", name)
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}
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

func storeFlags(router, token string) []string {
	var args []string
	if router != "" {
		args = append(args, "--monofs-router", router)
	}
	if token != "" {
		args = append(args, "--monofs-token", token)
	}
	return args
}

func selfBinary() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "guardianctl"
}
