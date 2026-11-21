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
	cliformat "github.com/radryc/guardian/internal/cli/format"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/forge/plan"
	monofsstore "github.com/radryc/guardian/internal/store/monofs"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func planCommand() *command.Command {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	all := flags.Bool("all", false, "plan every file instead of only changed files")
	base := flags.String("base", "", "git ref to diff against")
	against := flags.String("against", "disk", "disk|live")
	includeSecrets := flags.Bool("include-secrets", false, "include files under secrets/")
	format := flags.String("format", string(cliformat.FormatText), "output format: text|json|markdown")
	routerAddr := flags.String("monofs-router", firstEnv("MONOFS_ROUTER", "127.0.0.1:9090"), "MonoFS router gRPC address")
	token := flags.String("monofs-token", os.Getenv("MONOFS_TOKEN"), "MonoFS token")
	principal := flags.String("monofs-principal-id", "forge", "MonoFS principal id")

	return &command.Command{Description: "Build a read-only change plan for a partition", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		var store guardianapi.ReadStore
		switch strings.TrimSpace(*against) {
		case "", "disk":
		case "live":
			if strings.TrimSpace(*token) == "" {
				return fmt.Errorf("--against live requires a MonoFS token: pass --monofs-token or set MONOFS_TOKEN")
			}
			opened, _, err := monofsstore.Open(ctx, monofsstore.OpenConfig{
				RouterAddr: *routerAddr, Token: *token, PrincipalID: *principal,
				Role: "cli", ClientIDPrefix: "forge-plan", Version: "forge",
				RPCTimeout: 30 * time.Second,
			})
			if err != nil {
				return fmt.Errorf("connect to MonoFS: %w", err)
			}
			store = opened
		default:
			return fmt.Errorf("unsupported --against %q (want disk or live)", *against)
		}

		result, err := plan.Build(ctx, plan.Options{
			Root: *root, PartitionDir: dir, Partition: *partition,
			Changed: !*all, BaseRef: *base, IncludeSecrets: *includeSecrets, Store: store,
		})
		if err != nil {
			return err
		}
		switch strings.TrimSpace(*format) {
		case "json":
			(&output.Printer{Format: cliformat.FormatJSON, Writer: os.Stdout}).PrintJSON(result)
		case "markdown":
			fmt.Print(renderPlanMarkdown(result))
		case "", "text":
			printPlanText(&output.Printer{Format: cliformat.FormatText, Writer: os.Stdout}, result)
		default:
			return fmt.Errorf("unsupported --format %q (want text, json, or markdown)", *format)
		}
		return nil
	}}
}

func printPlanText(printer *output.Printer, result plan.Result) {
	printer.PrintText("plan for %s against %s: %d asset(s), %d intent(s)\n",
		result.Partition, result.Against, result.Assets, len(result.Intents))
	if len(result.Operations) == 0 {
		printer.PrintText("no changes\n")
		return
	}
	rows := make([][]string, 0, len(result.Operations))
	for _, op := range result.Operations {
		rows = append(rows, []string{op.Kind, op.Path})
	}
	printer.PrintTable([]string{"KIND", "PATH"}, rows)
}

func renderPlanMarkdown(result plan.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Forge plan: %s\n\n", result.Partition)
	fmt.Fprintf(&b, "%d asset(s) across %d intent(s), planned against **%s**.\n\n", result.Assets, len(result.Intents), result.Against)
	if len(result.Operations) == 0 {
		b.WriteString("No repository changes.\n")
		return b.String()
	}
	b.WriteString("| Kind | Path |\n|---|---|\n")
	for _, op := range result.Operations {
		fmt.Fprintf(&b, "| %s | `%s` |\n", op.Kind, op.Path)
	}
	b.WriteString("\nMerging deploys this change after the required checks pass.\n")
	return b.String()
}
