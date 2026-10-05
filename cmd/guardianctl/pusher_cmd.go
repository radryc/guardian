package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func pusherAddCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("pusher add", flag.ContinueOnError)
	name := flags.String("name", "", "pusher name (default aws-<account>)")
	account := flags.String("account", "", "AWS account id")
	region := flags.String("region", "", "AWS region")
	url := flags.String("url", "", "observed read API base URL")
	token := flags.String("token", "", "observed read API bearer token")
	tokenEnv := flags.String("token-env", "", "environment variable holding the bearer token")
	description := flags.String("description", "", "free-form description")
	return &command.Command{Description: "Register an AWS pusher and its observed read API URL", Flags: flags, Run: func(ctx context.Context, args []string) error {
		pusherName := strings.TrimSpace(*name)
		if pusherName == "" && strings.TrimSpace(*account) != "" {
			pusherName = "aws-" + strings.TrimSpace(*account)
		}
		pusher := &registry.Pusher{
			Name:           pusherName,
			Account:        *account,
			Region:         *region,
			ObservedAPIURL: *url,
			Token:          *token,
			TokenEnv:       *tokenEnv,
			Description:    *description,
		}
		if err := registry.New(store).PutPusher(ctx, pusher); err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(pusher)
			return nil
		}
		printer.PrintText("registered pusher %s (%s)\n", pusher.Name, pusher.ObservedAPIURL)
		return nil
	}}
}

func pusherListCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	return &command.Command{Description: "List registered pushers", Flags: flag.NewFlagSet("pusher list", flag.ContinueOnError), Run: func(ctx context.Context, args []string) error {
		pushers, err := registry.New(store).ListPushers(ctx)
		if err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(pushers)
			return nil
		}
		rows := make([][]string, 0, len(pushers))
		for _, pusher := range pushers {
			rows = append(rows, []string{pusher.Name, pusher.Account, pusher.Region, pusher.ObservedAPIURL, pusher.Description})
		}
		printer.PrintTable([]string{"NAME", "ACCOUNT", "REGION", "URL", "DESCRIPTION"}, rows)
		return nil
	}}
}

func pusherRemoveCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("pusher remove", flag.ContinueOnError)
	name := flags.String("name", "", "pusher name")
	return &command.Command{Description: "Remove a registered pusher", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*name) == "" {
			return fmt.Errorf("--name is required")
		}
		if err := registry.New(store).DeletePusher(ctx, strings.TrimSpace(*name)); err != nil {
			return err
		}
		printer.PrintText("removed pusher %s\n", strings.TrimSpace(*name))
		return nil
	}}
}
