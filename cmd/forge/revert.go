package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/radryc/guardian/internal/cli/command"
	cliformat "github.com/radryc/guardian/internal/cli/format"
	"github.com/radryc/guardian/internal/cli/output"
)

func revertCommand() *command.Command {
	flags := flag.NewFlagSet("revert", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	to := flags.String("to", "", "merge commit to revert")
	message := flags.String("message", "", "commit message")
	repoURL := flags.String("repo-url", "", "repository clone URL")
	base := flags.String("base", "", "git ref to diff against")
	baseBranch := flags.String("base-branch", "", "target branch")
	workspaceID := flags.String("workspace", "forge", "MonoFS workspace id")
	force := flags.Bool("force", false, "revert onto a dirty working tree")
	dryRun := flags.Bool("dry-run", false, "revert locally and print operations without publishing")
	format := flags.String("format", string(cliformat.FormatText), "output format: text|json")
	routerAddr := flags.String("monofs-router", firstEnv("MONOFS_ROUTER", "127.0.0.1:9090"), "MonoFS router gRPC address")
	token := flags.String("monofs-token", os.Getenv("MONOFS_TOKEN"), "MonoFS token")
	principal := flags.String("monofs-principal-id", "forge", "MonoFS principal id")

	return &command.Command{Description: "Revert a merge through the same reviewed proposal path", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		outFormat, err := parseFormat(*format)
		if err != nil {
			return err
		}
		sha := strings.TrimSpace(*to)
		if sha == "" {
			return fmt.Errorf("--to is required")
		}
		if !*force {
			status, err := gitRun(ctx, *root, "status", "--porcelain")
			if err != nil {
				return err
			}
			if strings.TrimSpace(status) != "" {
				return fmt.Errorf("working tree is dirty; commit or stash first (or pass --force)")
			}
		}
		if _, err := gitRun(ctx, *root, "revert", "--no-commit", sha); err != nil {
			return err
		}
		commitMessage := strings.TrimSpace(*message)
		if commitMessage == "" {
			commitMessage = "revert: " + sha
		}
		result, submitErr := executeSubmit(ctx, submitOptions{
			Root: *root, Partition: *partition, Message: commitMessage,
			Base: *base, BaseBranch: *baseBranch, WorkspaceID: *workspaceID,
			RepoURL: *repoURL, DryRun: *dryRun, Format: *format,
			RouterAddr: *routerAddr, Token: *token, Principal: *principal,
		})
		printer := &output.Printer{Format: outFormat, Writer: os.Stdout}
		if outFormat == cliformat.FormatJSON {
			printer.PrintJSON(result)
		} else {
			printSubmitResult(printer, result)
		}
		if submitErr != nil {
			fmt.Fprintln(os.Stderr, "revert applied to the working tree; the proposal failed. Inspect the tree, then retry forge submit.")
			return submitErr
		}
		return nil
	}}
}

func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
