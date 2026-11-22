package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/forge/author"
	"github.com/radryc/guardian/internal/forge/publish"
	"github.com/radryc/guardian/internal/repoconfig"
)

func accessShowCommand() *command.Command {
	flags := flag.NewFlagSet("access show", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	return &command.Command{Description: "Show the partition access policy or its template", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		data, exists, err := author.ReadPartitionAccess(dir)
		if err != nil {
			return err
		}
		if !exists {
			data = []byte(author.PartitionAccessTemplate(*partition))
		}
		_, err = os.Stdout.Write(data)
		return err
	}}
}

func accessTemplateCommand() *command.Command {
	flags := flag.NewFlagSet("access template", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	partition := flags.String("partition", "", "partition name")
	return &command.Command{Description: "Print a starter partition access policy", Flags: flags, Run: func(_ context.Context, _ []string) error {
		if *partition == "" {
			return fmt.Errorf("--partition is required")
		}
		_, err := fmt.Print(author.PartitionAccessTemplate(*partition))
		return err
	}}
}

func accessSetCommand() *command.Command {
	flags := flag.NewFlagSet("access set", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	file := flags.String("file", "", "access policy file (use - for stdin)")
	return &command.Command{Description: "Validate and write partitions/<name>/access.yaml", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		if *file == "" {
			return fmt.Errorf("--file is required (use - for stdin)")
		}
		var data []byte
		if *file == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(*file)
		}
		if err != nil {
			return err
		}
		path, err := author.SavePartitionAccess(dir, *partition, repoDisplayPath(*root), data)
		if err != nil {
			return err
		}
		fmt.Printf("updated %s\n", path)
		return nil
	}}
}

func repoDisplayPath(root string) string {
	cfg, _, _, err := repoconfig.Load(root)
	if err != nil || cfg == nil {
		return ""
	}
	return publish.DisplayPathFromURL(cfg.Repo.Source)
}
