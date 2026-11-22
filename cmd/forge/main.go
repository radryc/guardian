// Command forge prepares Guardian partition repositories: validate, plan, and
// submit changes for review. It never writes Guardian or MonoFS state; its only
// publish action is a proposal through MonoFS.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/radryc/guardian/internal/cli/command"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "help", "-h", "--help":
		usage()
		return
	case "validate":
		err = runCommand(ctx, "validate", validateCommand(), os.Args[2:])
	case "submit":
		err = runCommand(ctx, "submit", submitCommand(), os.Args[2:])
	case "status":
		err = runCommand(ctx, "status", statusCommand(), os.Args[2:])
	case "schema":
		err = runCommand(ctx, "schema", schemaCommand(), os.Args[2:])
	case "discover":
		err = runCommand(ctx, "discover", discoverCommand(), os.Args[2:])
	case "partition":
		err = runGroup(ctx, "partition", map[string]*command.Command{"new": partitionNewCommand()}, os.Args[2:])
	case "intent":
		err = runGroup(ctx, "intent", map[string]*command.Command{"new": intentNewCommand()}, os.Args[2:])
	case "quota":
		err = runGroup(ctx, "quota", map[string]*command.Command{
			"profiles": quotaProfilesCommand(), "show": quotaShowCommand(), "set": quotaSetCommand(),
		}, os.Args[2:])
	case "access":
		err = runGroup(ctx, "access", map[string]*command.Command{
			"show": accessShowCommand(), "set": accessSetCommand(), "template": accessTemplateCommand(),
		}, os.Args[2:])
	case "values":
		err = runGroup(ctx, "values", map[string]*command.Command{"show": valuesShowCommand(), "set": valuesSetCommand()}, os.Args[2:])
	case "asset":
		err = runGroup(ctx, "asset", map[string]*command.Command{"add": assetAddCommand(), "connect": assetConnectCommand()}, os.Args[2:])
	case "probe":
		err = runGroup(ctx, "probe", map[string]*command.Command{"add": probeAddCommand()}, os.Args[2:])
	case "secret":
		err = runGroup(ctx, "secret", map[string]*command.Command{"ref": secretRefCommand()}, os.Args[2:])
	case "alert":
		err = runGroup(ctx, "alert", map[string]*command.Command{"suggest": alertSuggestCommand()}, os.Args[2:])
	case "repo":
		err = runGroup(ctx, "repo", map[string]*command.Command{"init": repoInitCommand(), "adopt": repoAdoptCommand()}, os.Args[2:])
	case "policy":
		err = runGroup(ctx, "policy", map[string]*command.Command{"check": policyCheckCommand()}, os.Args[2:])
	case "pipeline":
		err = runGroup(ctx, "pipeline", map[string]*command.Command{"install": pipelineInstallCommand()}, os.Args[2:])
	case "doctor":
		err = runCommand(ctx, "doctor", doctorCommand(), os.Args[2:])
	case "ui":
		err = runCommand(ctx, "ui", uiCommand(), os.Args[2:])
	case "plan":
		err = runCommand(ctx, "plan", planCommand(), os.Args[2:])
	case "display":
		err = runGroup(ctx, "display", map[string]*command.Command{
			"bind": displayBindCommand(), "show": displayShowCommand(), "lint": displayLintCommand(),
		}, os.Args[2:])
	case "revert":
		err = runCommand(ctx, "revert", revertCommand(), os.Args[2:])
	default:
		usage()
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCommand(ctx context.Context, name string, cmd *command.Command, args []string) error {
	if cmd == nil {
		return fmt.Errorf("unknown command %q", name)
	}
	if err := cmd.Flags.Parse(args); err != nil {
		return err
	}
	if cmd.Run == nil {
		return nil
	}
	return cmd.Run(ctx, cmd.Flags.Args())
}

func runGroup(ctx context.Context, group string, commands map[string]*command.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected %s <command>", group)
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("unknown %s command %q", group, args[0])
	}
	return runCommand(ctx, group+" "+args[0], cmd, args[1:])
}

func usage() {
	fmt.Fprintln(os.Stderr, "forge — Guardian partition repository workbench")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  forge validate [--root PATH] [--strict] [--format text|json] [--include-secrets]")
	fmt.Fprintln(os.Stderr, "  forge submit   [--root PATH] [--partition NAME] [--message TEXT] [--dry-run]")
	fmt.Fprintln(os.Stderr, "  forge status   [--root PATH] [--partition NAME] [--live] [--format text|json]")
	fmt.Fprintln(os.Stderr, "  forge schema   [--out DIR] [--stdout]")
	fmt.Fprintln(os.Stderr, "  forge discover [--root PATH] [--out FILE] [--format text|json]")
	fmt.Fprintln(os.Stderr, "  forge partition new --name NAME")
	fmt.Fprintln(os.Stderr, "  forge intent new --partition P --name I --pusher PUSHER [--cluster C] [--namespace N]")
	fmt.Fprintln(os.Stderr, "  forge quota profiles")
	fmt.Fprintln(os.Stderr, "  forge quota show --partition P")
	fmt.Fprintln(os.Stderr, "  forge quota set --partition P [--file F | --json J | --profile NAME] [--clear]")
	fmt.Fprintln(os.Stderr, "  forge access show --partition P")
	fmt.Fprintln(os.Stderr, "  forge access set --partition P --file F")
	fmt.Fprintln(os.Stderr, "  forge access template --partition P")
	fmt.Fprintln(os.Stderr, "  forge values show --partition P [--intent I] [--asset A]")
	fmt.Fprintln(os.Stderr, "  forge values set --partition P [--intent I] [--asset A] --file F [--clear]")
	fmt.Fprintln(os.Stderr, "  forge asset add --partition P --intent I --type T --name N [--image IMG] [--properties JSON]")
	fmt.Fprintln(os.Stderr, "  forge asset connect --partition P --intent I --from A --to B")
	fmt.Fprintln(os.Stderr, "  forge probe add --partition P --intent I --asset A [--type T] [--steps JSON]")
	fmt.Fprintln(os.Stderr, "  forge secret ref --partition P --intent I --asset A --env KEY --ref REF")
	fmt.Fprintln(os.Stderr, "  forge alert suggest --partition P [--write]")
	fmt.Fprintln(os.Stderr, "  forge repo init --name NAME [--source URL] [--partition P]\n  forge repo adopt --partition P [--out DIR]")
	fmt.Fprintln(os.Stderr, "  forge policy check [--root PATH] [--file FILE]")
	fmt.Fprintln(os.Stderr, "  forge pipeline install [--file FILE] [--apply]")
	fmt.Fprintln(os.Stderr, "  forge doctor [--root PATH] [--strict]")
	fmt.Fprintln(os.Stderr, "  forge ui [--root PATH] [--listen 127.0.0.1:0] [--no-open]")
	fmt.Fprintln(os.Stderr, "  forge plan --partition P [--all] [--against disk|live] [--format text|json|markdown]")
	fmt.Fprintln(os.Stderr, "  forge display bind|show|lint ...")
	fmt.Fprintln(os.Stderr, "  forge revert --to SHA --partition P [--dry-run]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Forge never writes Guardian or MonoFS state; changes are proposed through MonoFS.")
}
