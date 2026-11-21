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
	"github.com/radryc/guardian/internal/forge/validate"
)

func validateCommand() *command.Command {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root, partitions directory, or one partition")
	strict := flags.Bool("strict", false, "treat warnings as errors")
	format := flags.String("format", string(cliformat.FormatText), "output format: text|json")
	includeSecrets := flags.Bool("include-secrets", false, "validate files under secrets/")
	return &command.Command{
		Description: "Validate partition manifests, assets, payloads, and alert rules",
		Flags:       flags,
		Run: func(ctx context.Context, _ []string) error {
			outFormat, err := parseFormat(*format)
			if err != nil {
				return err
			}
			result, runErr := validate.Run(ctx, validate.Options{
				Root:           strings.TrimSpace(*root),
				Strict:         *strict,
				IncludeSecrets: *includeSecrets,
			})
			printer := &output.Printer{Format: outFormat, Writer: os.Stdout}
			if outFormat == cliformat.FormatJSON {
				printer.PrintJSON(result)
			} else {
				printValidateResult(printer, result, *strict)
			}
			return runErr
		},
	}
}

func parseFormat(value string) (cliformat.Format, error) {
	switch cliformat.Format(strings.TrimSpace(value)) {
	case cliformat.FormatText:
		return cliformat.FormatText, nil
	case cliformat.FormatJSON:
		return cliformat.FormatJSON, nil
	default:
		return "", fmt.Errorf("unsupported --format %q (want text or json)", value)
	}
}

func printValidateResult(printer *output.Printer, result validate.Result, strict bool) {
	printer.PrintText("validated %d partition(s) under %s\n", result.PartitionsChecked, result.Root)
	for _, d := range result.Diagnostics {
		location := d.Partition
		if d.File != "" {
			if location != "" {
				location += " "
			}
			location += d.File
		}
		if location != "" {
			location = " " + location
		}
		printer.PrintText("%s %s%s: %s\n", strings.ToUpper(string(d.Severity)), d.Code, location, d.Message)
		if d.Hint != "" {
			printer.PrintText("  hint: %s\n", d.Hint)
		}
	}
	switch {
	case result.Success:
		if result.Warnings > 0 {
			printer.PrintText("ok: no errors (%d warning(s))\n", result.Warnings)
		} else {
			printer.PrintText("ok: no errors\n")
		}
	case strict && result.Warnings > 0 && result.Errors == 0:
		printer.PrintText("failed: %d warning(s) treated as errors by --strict\n", result.Warnings)
	default:
		printer.PrintText("failed: %d error(s), %d warning(s)\n", result.Errors, result.Warnings)
	}
}
