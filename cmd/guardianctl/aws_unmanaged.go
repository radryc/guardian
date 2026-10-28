package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/observed"
	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/unmanaged"
	"github.com/radryc/guardian/pkg/guardianapi"
)

// awsUnmanagedCommand lists CloudFormation stacks that need a repository and
// associates a repository with a stack (observed snapshot + review branch +
// MR/PR through MonoFS). It is the headless equivalent of the Unmanaged
// Stacks UI panel.
func awsUnmanagedCommand(store guardianapi.Store, publisher func(context.Context) unmanaged.Publisher, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("aws unmanaged", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	stack := flags.String("stack", "", "stack name (associate)")
	repo := flags.String("repo", "", "repository URL to associate (associate)")
	repoName := flags.String("repo-name", "", "Guardian repo record name (defaults to the partition name)")
	partition := flags.String("partition", "", "target partition (defaults to aws-<stack>)")
	branch := flags.String("branch", "", "base branch of the repository (defaults to its default branch)")
	pusher := flags.String("pusher", "", "pusher name (associate)")
	account := flags.String("account", "", "AWS account id override (associate)")
	region := flags.String("region", "", "AWS region override (associate)")
	templateFile := flags.String("template-file", "", "use a local CloudFormation template instead of fetching the live one")
	return &command.Command{Description: "List unmanaged AWS stacks and associate repositories with them", Flags: flags, Run: func(ctx context.Context, args []string) error {
		sub := "list"
		rest := args
		if len(args) > 0 {
			sub = strings.TrimSpace(args[0])
			rest = args[1:]
		}
		// Flags may follow the subcommand ("aws unmanaged associate --stack …");
		// the stdlib flag parser stops at the first positional, so parse the
		// remainder here as well.
		if err := flags.Parse(rest); err != nil && !errors.Is(err, flag.ErrHelp) {
			return err
		}
		service := &unmanaged.Service{
			Store:     store,
			Registry:  registry.New(store),
			Resolver:  &observed.RegistryResolver{Registry: registry.New(store)},
			Publisher: publisher(ctx),
		}
		switch sub {
		case "list":
			result, err := service.Detect(ctx)
			if err != nil {
				return err
			}
			if printer.Format == "json" {
				printer.PrintJSON(result)
				return nil
			}
			for _, warning := range result.Warnings {
				printer.PrintText("warning: %s\n", warning)
			}
			rows := make([][]string, 0, len(result.Stacks))
			for _, entry := range result.Stacks {
				rows = append(rows, []string{
					entry.State,
					entry.StackName,
					entry.Region,
					entry.Pusher,
					entry.StackStatus,
					entry.DriftStatus,
					entry.Partition,
				})
			}
			printer.PrintTable([]string{"STATE", "STACK", "REGION", "PUSHER", "STATUS", "DRIFT", "PARTITION"}, rows)
			return nil
		case "associate":
			if strings.TrimSpace(*stack) == "" {
				return fmt.Errorf("--stack is required")
			}
			if strings.TrimSpace(*repo) == "" {
				return fmt.Errorf("--repo is required")
			}
			opts := unmanaged.AssociateOptions{
				RepoName:   strings.TrimSpace(*repoName),
				RepoURL:    strings.TrimSpace(*repo),
				BaseBranch: strings.TrimSpace(*branch),
				Partition:  strings.TrimSpace(*partition),
				Pusher:     strings.TrimSpace(*pusher),
				Account:    strings.TrimSpace(*account),
				Region:     strings.TrimSpace(*region),
				StackName:  strings.TrimSpace(*stack),
			}
			if file := strings.TrimSpace(*templateFile); file != "" {
				raw, err := os.ReadFile(file)
				if err != nil {
					return fmt.Errorf("read template: %w", err)
				}
				opts.Template = string(raw)
			}
			association, err := service.Associate(ctx, opts)
			if err != nil {
				return err
			}
			if printer.Format == "json" {
				printer.PrintJSON(association)
				return nil
			}
			printer.PrintText("repo %s registered for partition %s\n", association.Repo.Name, association.Partition)
			if association.PublishError != "" {
				printer.PrintText("publish failed: %s\n", association.PublishError)
				printer.PrintText("the repo registration is kept; retry with 'guardianctl aws unmanaged associate'\n")
				return nil
			}
			if association.Publish != nil {
				repoResult := association.Publish.Repository
				printer.PrintText("published branch %s (status %s)\n", repoResult.TargetBranch, repoResult.Status)
				if repoResult.PullRequestURL != "" {
					if repoResult.PullRequestCreated {
						printer.PrintText("merge request created: %s\n", repoResult.PullRequestURL)
					} else {
						printer.PrintText("open merge request: %s\n", repoResult.PullRequestURL)
					}
				}
			} else {
				printer.PrintText("prepared branch %s (no MonoFS publisher configured)\n", association.Branch)
			}
			return nil
		default:
			return fmt.Errorf("unknown subcommand %q (use list or associate)", sub)
		}
	}}
}
