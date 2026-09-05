package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
)

type Command struct {
	Name        string
	Description string
	Flags       *flag.FlagSet
	Run         func(ctx context.Context, args []string) error
}

type Registry struct {
	commands map[string]*Command
	groups   map[string][]*Command
}

func New() *Registry {
	return &Registry{commands: map[string]*Command{}, groups: map[string][]*Command{}}
}

func (r *Registry) Register(group, name string, cmd *Command) {
	if cmd.Flags == nil {
		cmd.Flags = flag.NewFlagSet(group+" "+name, flag.ContinueOnError)
	}
	cmd.Name = name
	cmd.Flags.SetOutput(io.Discard)
	key := group + " " + name
	if r.commands == nil {
		r.commands = map[string]*Command{}
	}
	if r.groups == nil {
		r.groups = map[string][]*Command{}
	}
	r.commands[key] = cmd
	r.groups[group] = append(r.groups[group], cmd)
}

func (r *Registry) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		r.Usage()
		return fmt.Errorf("expected command")
	}
	if isHelpToken(args[0]) {
		return r.runHelp(args[1:])
	}
	if len(args) == 2 && isHelpToken(args[1]) {
		if _, ok := r.groups[args[0]]; ok {
			r.groupUsage(args[0])
			return nil
		}
	}
	if len(args) == 1 {
		key := args[0] + " run"
		if cmd, ok := r.commands[key]; ok {
			if err := cmd.Flags.Parse(nil); err != nil {
				return err
			}
			if cmd.Run == nil {
				return nil
			}
			return cmd.Run(ctx, cmd.Flags.Args())
		}
		r.Usage()
		return fmt.Errorf("expected <group> <command>")
	}
	key := args[0] + " " + args[1]
	cmd, ok := r.commands[key]
	if !ok {
		r.Usage()
		return fmt.Errorf("unknown command %q", key)
	}
	if hasExtendedHelpArg(args[2:]) {
		r.commandUsage(args[0], cmd, true)
		return nil
	}
	if err := cmd.Flags.Parse(args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			r.commandUsage(args[0], cmd, false)
			return nil
		}
		return err
	}
	if cmd.Run == nil {
		return nil
	}
	return cmd.Run(ctx, cmd.Flags.Args())
}
