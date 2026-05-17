package command

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Entry struct {
	Group       string
	Name        string
	Description string
}

func (r *Registry) Entries() []Entry {
	entries := make([]Entry, 0, len(r.commands))
	for key, cmd := range r.commands {
		group, name, ok := strings.Cut(key, " ")
		if !ok {
			continue
		}
		description := ""
		if cmd != nil {
			description = cmd.Description
		}
		entries = append(entries, Entry{
			Group:       group,
			Name:        name,
			Description: description,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Group != entries[j].Group {
			return entries[i].Group < entries[j].Group
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}

func isHelpToken(value string) bool {
	switch strings.TrimSpace(value) {
	case "-h", "--help", "help":
		return true
	default:
		return false
	}
}

func isExtendedHelpToken(value string) bool {
	switch strings.TrimSpace(value) {
	case "--help-full", "--help=full", "--extended-help", "help-full":
		return true
	default:
		return false
	}
}

func hasExtendedHelpArg(args []string) bool {
	for _, arg := range args {
		if isExtendedHelpToken(arg) {
			return true
		}
	}
	return false
}

func (r *Registry) runHelp(args []string) error {
	switch len(args) {
	case 0:
		r.Usage()
		return nil
	case 1:
		group := strings.TrimSpace(args[0])
		if _, ok := r.groups[group]; ok {
			r.groupUsage(group)
			return nil
		}
		return fmt.Errorf("unknown help topic %q", group)
	default:
		group := strings.TrimSpace(args[0])
		name := strings.TrimSpace(args[1])
		key := group + " " + name
		cmd, ok := r.commands[key]
		if !ok {
			return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
		}
		r.commandUsage(group, cmd, true)
		return nil
	}
}

func (r *Registry) commandUsage(group string, cmd *Command, extended bool) {
	if cmd == nil || cmd.Flags == nil {
		return
	}
	out := os.Stderr
	program := programName()
	fmt.Fprintf(out, "Usage: %s %s %s", program, group, cmd.Name)
	hasFlags := false
	cmd.Flags.VisitAll(func(*flag.Flag) {
		hasFlags = true
	})
	if hasFlags {
		fmt.Fprint(out, " [flags]")
	}
	fmt.Fprintln(out)
	if cmd.Description != "" {
		fmt.Fprintf(out, "\n%s\n", cmd.Description)
	}
	if !hasFlags {
		if extended {
			fmt.Fprintln(out, "\nExtended help:")
			fmt.Fprintln(out, "  This command has no command-specific flags.")
		}
		return
	}
	fmt.Fprintln(out, "\nFlags:")
	previousOutput := cmd.Flags.Output()
	cmd.Flags.SetOutput(out)
	defer cmd.Flags.SetOutput(previousOutput)
	cmd.Flags.PrintDefaults()
	if extended {
		printExtendedCommandHelp(out, program, group, cmd)
	}
}

func (r *Registry) groupUsage(group string) {
	cmds, ok := r.groups[group]
	if !ok {
		return
	}
	out := os.Stderr
	program := programName()
	fmt.Fprintf(out, "Usage: %s %s <command>\n", program, group)
	fmt.Fprintf(out, "\n%s commands:\n", strings.Title(group))
	sorted := append([]*Command(nil), cmds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, cmd := range sorted {
		fmt.Fprintf(out, "  %s\t%s\n", cmd.Name, cmd.Description)
	}
	fmt.Fprintf(out, "\nUse \"%s %s <command> --help\" for quick help.\n", program, group)
	fmt.Fprintf(out, "Use \"%s %s <command> --help-full\" or \"%s help %s <command>\" for extended help.\n", program, group, program, group)
}

func (r *Registry) Usage() {
	out := os.Stderr
	_, _ = fmt.Fprintln(out, "Commands:")
	for _, entry := range r.Entries() {
		_, _ = fmt.Fprintf(out, "  %s %s\t%s\n", entry.Group, entry.Name, entry.Description)
	}
}

func programName() string {
	program := filepath.Base(os.Args[0])
	if program == "" {
		return "command"
	}
	return program
}

func printExtendedCommandHelp(out io.Writer, program, group string, cmd *Command) {
	fmt.Fprintln(out, "\nExtended help:")
	fmt.Fprintf(out, "  Short form: %s %s %s --help\n", program, group, cmd.Name)
	fmt.Fprintf(out, "  Extended form: %s %s %s --help-full\n", program, group, cmd.Name)
	fmt.Fprintf(out, "  Help topic: %s help %s %s\n", program, group, cmd.Name)
	fmt.Fprintln(out, "\nDetailed flags:")
	cmd.Flags.VisitAll(func(flagDef *flag.Flag) {
		flagType := flagTypeName(flagDef)
		flagForm := "--" + flagDef.Name
		if flagType != "bool" {
			flagForm += " <" + flagType + ">"
		}
		fmt.Fprintf(out, "  %s\n", flagForm)
		fmt.Fprintf(out, "      %s\n", strings.TrimSpace(flagDef.Usage))
		fmt.Fprintf(out, "      default: %s\n", formatFlagDefault(flagDef))
		fmt.Fprintf(out, "      type: %s\n", flagType)
	})
}

func flagTypeName(flagDef *flag.Flag) string {
	getter, ok := flagDef.Value.(flag.Getter)
	if !ok {
		return "value"
	}
	switch getter.Get().(type) {
	case bool:
		return "bool"
	case timeDurationValue:
		return "duration"
	case string:
		return "string"
	case int:
		return "int"
	case int64:
		return "int64"
	case uint:
		return "uint"
	case uint64:
		return "uint64"
	case float64:
		return "float64"
	default:
		return "value"
	}
}

func formatFlagDefault(flagDef *flag.Flag) string {
	if flagDef.DefValue == "" {
		return "\"\""
	}
	return flagDef.DefValue
}

type timeDurationValue interface {
	String() string
}
