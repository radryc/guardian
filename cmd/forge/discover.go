package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/radryc/guardian/internal/cli/command"
	cliformat "github.com/radryc/guardian/internal/cli/format"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/forge/discover"
)

func discoverCommand() *command.Command {
	flags := flag.NewFlagSet("discover", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	out := flags.String("out", filepath.Join(".forge", "profile.json"), "profile output path (- for stdout)")
	format := flags.String("format", string(cliformat.FormatText), "output format: text|json")
	maxFiles := flags.Int("max-files", 20000, "stop scanning after this many files")

	return &command.Command{
		Description: "Scan a repository for workloads Forge can turn into partition assets",
		Flags:       flags,
		Run: func(ctx context.Context, _ []string) error {
			outFormat, err := parseFormat(*format)
			if err != nil {
				return err
			}
			profile, err := discover.Run(ctx, discover.Options{Root: *root, MaxFiles: *maxFiles})
			if err != nil {
				return err
			}
			if *out == "-" {
				printer := &output.Printer{Format: cliformat.FormatJSON, Writer: os.Stdout}
				printer.PrintJSON(profile)
				return nil
			}
			data, err := marshalIndent(profile)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(*out, data, 0o644); err != nil {
				return err
			}
			printer := &output.Printer{Format: outFormat, Writer: os.Stdout}
			if outFormat == cliformat.FormatJSON {
				printer.PrintJSON(profile)
				return nil
			}
			printDiscoverResult(printer, profile, *out)
			return nil
		},
	}
}

func printDiscoverResult(printer *output.Printer, profile discover.Profile, out string) {
	printer.PrintText("scanned %s\n", profile.Root)
	if profile.Repository != nil {
		printer.PrintText("repository: %s (%s)\n", profile.Repository.Name, profile.Repository.Source)
	}
	if len(profile.Languages) > 0 {
		printer.PrintText("languages: %v\n", profile.Languages)
	}
	if len(profile.Workloads) == 0 {
		printer.PrintText("no workloads detected\n")
	} else {
		rows := make([][]string, 0, len(profile.Workloads))
		for _, workload := range profile.Workloads {
			rows = append(rows, []string{
				workload.Name,
				workload.Kind,
				workload.Runtime,
				fmt.Sprint(workload.Ports),
				workload.Source,
				workload.Confidence,
			})
		}
		printer.PrintTable([]string{"NAME", "KIND", "RUNTIME", "PORTS", "SOURCE", "CONFIDENCE"}, rows)
	}
	if len(profile.Suggestions) > 0 {
		printer.PrintText("\nsuggestions:\n")
		for _, suggestion := range profile.Suggestions {
			printer.PrintText("  - %s\n", suggestion)
		}
	}
	printer.PrintText("\nprofile written to %s\n", out)
}
