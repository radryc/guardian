package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/cli/output"
	displaypkg "github.com/radryc/guardian/internal/display"
	"github.com/radryc/guardian/pkg/guardianapi"
	"github.com/radryc/monofs/pkg/display"
)

type displayCommand struct {
	group string
	name  string
	cmd   *command.Command
}

func displayCommands(store guardianapi.Store, printer *output.Printer) []displayCommand {
	svc := displaypkg.NewService(store)
	subject := "guardianctl"
	return []displayCommand{
		{"display", "profiles", displayListCommand(svc, printer, subject)},
		{"display", "create", displayCreateCommand(svc, printer, subject)},
		{"display", "bind", displayBindCommand(svc, printer, subject)},
		{"display", "show", displayShowCommand(svc, printer, subject)},
		{"display", "use", displayUseCommand(svc, printer, subject)},
	}
}

func displayListCommand(svc *displaypkg.Service, printer *output.Printer, subject string) *command.Command {
	flags := flag.NewFlagSet("display profiles", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	admin := flags.Bool("admin", false, "include every profile")
	return &command.Command{Description: "List display profiles", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		profiles, err := svc.List(ctx, subject, *admin)
		if err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(profiles)
			return nil
		}
		for _, profile := range profiles {
			printer.PrintText("%s\t%s\t%s\n", profile.ID, profile.Scope, profile.Name)
		}
		return nil
	}}
}

func displayCreateCommand(svc *displaypkg.Service, printer *output.Printer, subject string) *command.Command {
	flags := flag.NewFlagSet("display create", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	id := flags.String("id", "", "profile id")
	name := flags.String("name", "", "display name")
	scope := flags.String("scope", "personal", "personal|team|global")
	var keys stringListFlag
	flags.Var(&keys, "key", "canonical=display key (repeatable)")
	return &command.Command{Description: "Create a display profile", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		if strings.TrimSpace(*id) == "" {
			return fmt.Errorf("--id is required")
		}
		profile := display.Profile{ID: *id, Name: *name, Scope: *scope, Keys: map[string]string{}}
		for _, pair := range keys {
			canonical, value, ok := strings.Cut(pair, "=")
			if !ok {
				return fmt.Errorf("invalid --key %q (want canonical=display)", pair)
			}
			profile.Keys[strings.TrimSpace(canonical)] = strings.TrimSpace(value)
		}
		created, err := svc.Create(ctx, subject, profile, *scope != "personal")
		if err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(created)
			return nil
		}
		printer.PrintText("created display profile %s\n", created.ID)
		return nil
	}}
}

func displayBindCommand(svc *displaypkg.Service, printer *output.Printer, subject string) *command.Command {
	flags := flag.NewFlagSet("display bind", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	profileID := flags.String("profile", "", "profile id")
	from := flags.String("from", "", "canonical key")
	to := flags.String("to", "", "display key")
	return &command.Command{Description: "Add or replace one key binding", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		if strings.TrimSpace(*profileID) == "" || strings.TrimSpace(*from) == "" || strings.TrimSpace(*to) == "" {
			return fmt.Errorf("--profile, --from, and --to are required")
		}
		profile, err := svc.Get(ctx, subject, *profileID, false)
		if err != nil {
			return err
		}
		if profile.Keys == nil {
			profile.Keys = map[string]string{}
		}
		profile.Keys[*from] = *to
		updated, err := svc.Update(ctx, subject, *profileID, profile, false)
		if err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(updated)
			return nil
		}
		printer.PrintText("bound %s -> %s on %s\n", *from, *to, updated.ID)
		return nil
	}}
}

func displayShowCommand(svc *displaypkg.Service, printer *output.Printer, subject string) *command.Command {
	flags := flag.NewFlagSet("display show", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	profileID := flags.String("profile", "", "profile id")
	return &command.Command{Description: "Show a display profile", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		profile, err := svc.Get(ctx, subject, *profileID, false)
		if err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(profile)
			return nil
		}
		for canonical, value := range profile.Keys {
			printer.PrintText("%s -> %s\n", canonical, value)
		}
		return nil
	}}
}

func displayUseCommand(svc *displaypkg.Service, printer *output.Printer, subject string) *command.Command {
	flags := flag.NewFlagSet("display use", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	profileID := flags.String("profile", "", "profile id")
	return &command.Command{Description: "Set the default display profile", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		if err := svc.SetDefault(ctx, subject, *profileID); err != nil {
			return err
		}
		printer.PrintText("default display profile: %s\n", *profileID)
		return nil
	}}
}
