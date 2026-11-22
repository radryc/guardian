package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/forge/author"
	quotacatalog "github.com/radryc/guardian/internal/forge/quota"
	policy "github.com/radryc/monofs/pkg/authz/policy"
	"gopkg.in/yaml.v3"
)

func quotaProfilesCommand() *command.Command {
	flags := flag.NewFlagSet("quota profiles", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	return &command.Command{Description: "List predefined quota profiles", Flags: flags, Run: func(_ context.Context, _ []string) error {
		for _, profile := range quotacatalog.Catalog(*root) {
			kind := "custom"
			if profile.Builtin {
				kind = "builtin"
			}
			fmt.Printf("%s\t%s\n", profile.Name, kind)
		}
		return nil
	}}
}

func quotaShowCommand() *command.Command {
	flags := flag.NewFlagSet("quota show", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	return &command.Command{Description: "Show the partition resource quota", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		part, _, err := author.ReadPartition(dir)
		if err != nil {
			return err
		}
		data, err := yaml.Marshal(part.Spec.Quota)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(data)
		return err
	}}
}

func quotaSetCommand() *command.Command {
	flags := flag.NewFlagSet("quota set", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	file := flags.String("file", "", "JSON or YAML quota object (use - for stdin)")
	inline := flags.String("json", "", "inline JSON or YAML quota object")
	profile := flags.String("profile", "", "quota profile name from the platform access policy")
	enforcement := flags.String("enforcement", "", "quota enforcement mode")
	clear := flags.Bool("clear", false, "remove spec.quota")
	return &command.Command{Description: "Set or clear the partition resource quota", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		if *clear {
			if *file != "" || *inline != "" || *profile != "" || *enforcement != "" {
				return fmt.Errorf("--clear cannot be combined with quota input")
			}
			path, err := author.SetQuota(dir, nil)
			if err != nil {
				return err
			}
			fmt.Printf("cleared quota in %s\n", path)
			return nil
		}
		if *file != "" && *inline != "" {
			return fmt.Errorf("pass either --file or --json, not both")
		}
		if strings.TrimSpace(*file) == "" && strings.TrimSpace(*inline) == "" && *profile == "" && *enforcement == "" {
			return fmt.Errorf("pass --file, --json, --profile, --enforcement, or --clear")
		}
		var quota policy.Quota
		switch {
		case *file != "":
			var data []byte
			if *file == "-" {
				data, err = io.ReadAll(os.Stdin)
			} else {
				data, err = os.ReadFile(*file)
			}
			if err != nil {
				return err
			}
			if err := yaml.Unmarshal(data, &quota); err != nil {
				return fmt.Errorf("parse quota: %w", err)
			}
		case *inline != "":
			if err := yaml.Unmarshal([]byte(*inline), &quota); err != nil {
				return fmt.Errorf("parse quota: %w", err)
			}
		}
		if *profile != "" {
			quota.Profile = *profile
		}
		if *enforcement != "" {
			quota.Enforcement = *enforcement
		}
		if err := quotacatalog.Validate(*root, &quota); err != nil {
			return err
		}
		path, err := author.SetQuota(dir, &quota)
		if err != nil {
			return err
		}
		fmt.Printf("updated %s\n", path)
		return nil
	}}
}
