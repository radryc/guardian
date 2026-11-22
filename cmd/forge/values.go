package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/forge/author"
	"gopkg.in/yaml.v3"
)

func valuesShowCommand() *command.Command {
	flags := flag.NewFlagSet("values show", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	intent := flags.String("intent", "", "intent name")
	asset := flags.String("asset", "", "asset name")
	return &command.Command{Description: "Show values at partition, intent, or asset level", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		vals, err := author.ReadValues(dir, *intent, *asset)
		if err != nil {
			return err
		}
		data, err := yaml.Marshal(vals)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(data)
		return err
	}}
}

func valuesSetCommand() *command.Command {
	flags := flag.NewFlagSet("values set", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	intent := flags.String("intent", "", "intent name")
	asset := flags.String("asset", "", "asset name")
	file := flags.String("file", "", "JSON or YAML values object (use - for stdin)")
	inline := flags.String("json", "", "inline JSON or YAML values object")
	clear := flags.Bool("clear", false, "remove the values block")
	return &command.Command{Description: "Set or clear partition, intent, or asset values", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		if *asset != "" && *intent == "" {
			return fmt.Errorf("--asset requires --intent")
		}
		if *clear {
			if *file != "" || *inline != "" {
				return fmt.Errorf("--clear cannot be combined with values input")
			}
			path, err := author.SetValues(dir, *intent, *asset, nil)
			if err != nil {
				return err
			}
			fmt.Printf("cleared values in %s\n", path)
			return nil
		}
		if *file != "" && *inline != "" {
			return fmt.Errorf("pass either --file or --json, not both")
		}
		if *file == "" && *inline == "" {
			return fmt.Errorf("pass --file, --json, or --clear")
		}
		var data []byte
		if *file == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else if *file != "" {
			data, err = os.ReadFile(*file)
		} else {
			data = []byte(*inline)
		}
		if err != nil {
			return err
		}
		var vals map[string]any
		if err := yaml.Unmarshal(data, &vals); err != nil {
			return fmt.Errorf("parse values: %w", err)
		}
		path, err := author.SetValues(dir, *intent, *asset, vals)
		if err != nil {
			return err
		}
		fmt.Printf("updated %s\n", path)
		return nil
	}}
}
