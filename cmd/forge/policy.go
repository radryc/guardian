package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/radryc/guardian/internal/cli/command"
	cliformat "github.com/radryc/guardian/internal/cli/format"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/forge/policy"
	"github.com/radryc/guardian/internal/forge/workspace"
)

func policyCheckCommand() *command.Command {
	flags := flag.NewFlagSet("policy check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	file := flags.String("file", "", "guardrail policy file (default <root>/.forge/policy.yaml)")
	format := flags.String("format", string(cliformat.FormatText), "output format: text|json")
	return &command.Command{Description: "Evaluate the Forge guardrail policy", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		outFormat, err := parseFormat(*format)
		if err != nil {
			return err
		}
		refs, err := workspace.Discover(*root)
		if err != nil {
			return err
		}
		dirs := make([]string, 0, len(refs))
		for _, ref := range refs {
			dirs = append(dirs, ref.Dir)
		}
		policyPath := *file
		if policyPath == "" {
			policyPath = policy.PathFor(*root)
		}
		diagnostics, err := policy.Check(dirs, policyPath)
		if err != nil {
			return err
		}
		result := map[string]any{
			"success":     !diagnostics.HasErrors(),
			"policy":      policyPath,
			"errors":      diagnostics.Errors(),
			"warnings":    diagnostics.Warnings(),
			"diagnostics": diagnostics,
		}
		printer := &output.Printer{Format: outFormat, Writer: os.Stdout}
		if outFormat == cliformat.FormatJSON {
			printer.PrintJSON(result)
		} else {
			for _, d := range diagnostics {
				printer.PrintText("%s %s %s: %s\n", strings.ToUpper(string(d.Severity)), d.Code, d.Partition, d.Message)
			}
			if diagnostics.HasErrors() {
				printer.PrintText("failed: %d error(s)\n", diagnostics.Errors())
			} else {
				printer.PrintText("ok: no policy violations\n")
			}
		}
		if diagnostics.HasErrors() {
			return fmt.Errorf("%d policy violation(s)", diagnostics.Errors())
		}
		return nil
	}}
}
