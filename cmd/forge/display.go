package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/radryc/guardian/internal/cli/command"
	cliformat "github.com/radryc/guardian/internal/cli/format"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/forge/display"
)

func displayBindCommand() *command.Command {
	flags := flag.NewFlagSet("display bind", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "bound partition")
	from := flags.String("from", "", "canonical key, for example asset")
	to := flags.String("to", "", "display key, for example service")
	profile := flags.String("profile", "", "optional display profile id")
	return &command.Command{Description: "Bind a display-key alias on a repo-owned partition", Flags: flags, Run: func(_ context.Context, _ []string) error {
		if err := display.Bind(*root, *partition, *from, *to, *profile); err != nil {
			return err
		}
		fmt.Printf("bound %s -> %s on %s\n", *from, *to, *partition)
		return nil
	}}
}

func displayShowCommand() *command.Command {
	flags := flag.NewFlagSet("display show", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "bound partition")
	return &command.Command{Description: "Show repo-owned display aliases", Flags: flags, Run: func(_ context.Context, _ []string) error {
		keys, err := display.Show(*root, *partition)
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			fmt.Println("no aliases")
			return nil
		}
		names := make([]string, 0, len(keys))
		for name := range keys {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("%s -> %s\n", name, keys[name])
		}
		return nil
	}}
}

func displayLintCommand() *command.Command {
	flags := flag.NewFlagSet("display lint", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	format := flags.String("format", string(cliformat.FormatText), "output format: text|json")
	return &command.Command{Description: "Lint repo-owned display aliases", Flags: flags, Run: func(_ context.Context, _ []string) error {
		outFormat, err := parseFormat(*format)
		if err != nil {
			return err
		}
		list, err := display.Lint(*root)
		if err != nil {
			return err
		}
		printer := &output.Printer{Format: outFormat, Writer: os.Stdout}
		if outFormat == cliformat.FormatJSON {
			printer.PrintJSON(map[string]any{"success": !list.HasErrors(), "diagnostics": list})
		} else {
			for _, d := range list {
				printer.PrintText("%s %s %s: %s\n", d.Severity, d.Code, d.Partition, d.Message)
			}
			if list.HasErrors() {
				printer.PrintText("failed: %d error(s)\n", list.Errors())
			} else {
				printer.PrintText("ok: no display-key errors\n")
			}
		}
		if list.HasErrors() {
			return fmt.Errorf("%d display-key error(s)", list.Errors())
		}
		return nil
	}}
}
