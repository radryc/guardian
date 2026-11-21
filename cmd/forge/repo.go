package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/forge/adopt"
	"github.com/radryc/guardian/internal/forge/repoinit"
	monofsstore "github.com/radryc/guardian/internal/store/monofs"
)

func repoInitCommand() *command.Command {
	flags := flag.NewFlagSet("repo init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	name := flags.String("name", "", "repository name")
	source := flags.String("source", "", "repository clone URL")
	branch := flags.String("branch", "main", "default branch")
	partition := flags.String("partition", "", "bind a partition to this repository")
	force := flags.Bool("force", false, "overwrite existing Forge files")
	return &command.Command{Description: "Prepare a repository for Forge and Guardian", Flags: flags, Run: func(_ context.Context, _ []string) error {
		if strings.TrimSpace(*name) == "" {
			return fmt.Errorf("--name is required")
		}
		result, err := repoinit.Init(repoinit.Options{
			Root: *root, Name: *name, Source: *source, Branch: *branch,
			Partition: *partition, Force: *force,
		})
		if err != nil {
			return err
		}
		for _, file := range result.Files {
			fmt.Printf("wrote %s\n", file)
		}
		return nil
	}}
}

func repoAdoptCommand() *command.Command {
	flags := flag.NewFlagSet("repo adopt", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "live partition to import")
	out := flags.String("out", "", "output directory (default <root>/partitions/<partition>)")
	force := flags.Bool("force", false, "overwrite existing files")
	routerAddr := flags.String("monofs-router", firstEnv("MONOFS_ROUTER", "127.0.0.1:9090"), "MonoFS router gRPC address")
	token := flags.String("monofs-token", os.Getenv("MONOFS_TOKEN"), "MonoFS token")
	principal := flags.String("monofs-principal-id", "forge", "MonoFS principal id")
	return &command.Command{Description: "Import a live partition bundle into the repository", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		if strings.TrimSpace(*token) == "" {
			return fmt.Errorf("MonoFS token is required: pass --monofs-token or set MONOFS_TOKEN")
		}
		store, _, err := monofsstore.Open(ctx, monofsstore.OpenConfig{
			RouterAddr: *routerAddr, Token: *token, PrincipalID: *principal,
			Role: "cli", ClientIDPrefix: "forge-adopt", Version: "forge",
			RPCTimeout: 30 * time.Second,
		})
		if err != nil {
			return fmt.Errorf("connect to MonoFS: %w", err)
		}
		result, err := adopt.Run(ctx, adopt.Options{
			Root: *root, Partition: *partition, OutDir: *out, Force: *force, Store: store,
		})
		if err != nil {
			return err
		}
		for _, file := range result.Files {
			fmt.Printf("wrote %s\n", file)
		}
		fmt.Printf("adopted %s into %s\n", result.Partition, result.OutDir)
		return nil
	}}
}
