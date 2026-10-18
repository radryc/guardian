package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	setstore "github.com/rydzu/ainfra/guardian/internal/sets"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type stringListFlag []string

func (v *stringListFlag) String() string { return strings.Join(*v, ",") }
func (v *stringListFlag) Set(value string) error {
	*v = append(*v, strings.TrimSpace(value))
	return nil
}

func setsEnsureCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("sets ensure", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	name := flags.String("name", "", "set display name")
	description := flags.String("description", "", "set description")
	parent := flags.String("parent", "", "parent set id (optional)")
	var labels stringListFlag
	flags.Var(&labels, "label", "key=value label (repeatable)")
	var partitions stringListFlag
	flags.Var(&partitions, "partition", "partition to add (repeatable; missing partitions are skipped)")
	primary := flags.Bool("primary", false, "mark added partitions' primary set")
	allowMissing := flags.Bool("allow-missing", false, "add partitions even when no partition config exists yet")
	return &command.Command{Description: "Create or update a virtual set and add partitions idempotently", Flags: flags, Run: func(ctx context.Context, args []string) error {
		setName := strings.TrimSpace(*name)
		if setName == "" {
			return fmt.Errorf("--name is required")
		}
		sets := setstore.New(store)
		parentID, err := resolveParentID(ctx, store, *parent)
		if err != nil {
			return err
		}
		set, err := sets.FindByName(ctx, setName)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if os.IsNotExist(err) || set == nil {
			set = &setstore.Set{Name: setName, Description: *description, ParentID: parentID, Labels: labelMap(labels)}
			set, err = sets.Create(ctx, set)
			if err != nil {
				return err
			}
		} else {
			set.Description = *description
			if parentID != "" {
				set.ParentID = parentID
			}
			for key, value := range labelMap(labels) {
				set.Labels[key] = value
			}
			set, err = sets.Update(ctx, set)
			if err != nil {
				return err
			}
		}
		add := partitions
		if !*allowMissing {
			add = existingPartitions(ctx, store, partitions)
		}
		if len(add) > 0 {
			set, err = sets.AddPartitions(ctx, set.ID, add, nil, *primary)
			if err != nil {
				return err
			}
		}
		return printSet(printer, set, "ready")
	}}
}

func setsCreateCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("sets create", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	name := flags.String("name", "", "set display name")
	description := flags.String("description", "", "set description")
	parent := flags.String("parent", "", "parent set id (optional)")
	var labels stringListFlag
	flags.Var(&labels, "label", "key=value label (repeatable)")
	pinned := flags.Bool("pinned", false, "pin the set")
	return &command.Command{Description: "Create a virtual set", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*name) == "" {
			return fmt.Errorf("--name is required")
		}
		parentID, err := resolveParentID(ctx, store, *parent)
		if err != nil {
			return err
		}
		set, err := setstore.New(store).Create(ctx, &setstore.Set{Name: *name, Description: *description, ParentID: parentID, Labels: labelMap(labels), Pinned: *pinned})
		if err != nil {
			return err
		}
		return printSet(printer, set, "created")
	}}
}

func setsShowCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("sets show", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	setRef := flags.String("set", "", "set id or name")
	return &command.Command{Description: "Show a virtual set by id or name", Flags: flags, Run: func(ctx context.Context, args []string) error {
		set, err := resolveSet(ctx, store, *setRef)
		if err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(set)
			return nil
		}
		printer.PrintText("id:          %s\nname:        %s\nparent:      %s\ndescription: %s\npinned:      %t\npartitions:  %s\n",
			set.ID, set.Name, set.ParentID, set.Description, set.Pinned, strings.Join(set.Partitions, ", "))
		if len(set.Labels) > 0 {
			printer.PrintText("labels:      ")
			first := true
			for key, value := range set.Labels {
				if !first {
					printer.PrintText(", ")
				}
				printer.PrintText("%s=%s", key, value)
				first = false
			}
			printer.PrintText("\n")
		}
		return nil
	}}
}

func setsUpdateCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("sets update", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	setRef := flags.String("set", "", "set id or name")
	name := flags.String("name", "", "new display name")
	description := flags.String("description", "", "new description")
	clearDescription := flags.Bool("clear-description", false, "clear the description")
	parent := flags.String("parent", "", "new parent set id (empty leaves unchanged)")
	pinned := flags.String("pinned", "", "true|false (empty leaves unchanged)")
	clearLabels := flags.Bool("clear-labels", false, "remove all labels before applying --label")
	var labels stringListFlag
	flags.Var(&labels, "label", "key=value label to merge (repeatable)")
	var removeLabels stringListFlag
	flags.Var(&removeLabels, "remove-label", "label key to remove (repeatable)")
	return &command.Command{Description: "Update a virtual set by id or name", Flags: flags, Run: func(ctx context.Context, args []string) error {
		set, err := resolveSet(ctx, store, *setRef)
		if err != nil {
			return err
		}
		if strings.TrimSpace(*name) != "" {
			set.Name = *name
		}
		if *clearDescription {
			set.Description = ""
		} else if *description != "" {
			set.Description = *description
		}
		if strings.TrimSpace(*parent) != "" {
			parentID, err := resolveParentID(ctx, store, *parent)
			if err != nil {
				return err
			}
			set.ParentID = parentID
		}
		if strings.TrimSpace(*pinned) != "" {
			set.Pinned = strings.EqualFold(strings.TrimSpace(*pinned), "true")
		}
		if *clearLabels {
			set.Labels = map[string]string{}
		}
		if set.Labels == nil {
			set.Labels = map[string]string{}
		}
		for _, key := range removeLabels {
			delete(set.Labels, strings.TrimSpace(key))
		}
		for key, value := range labelMap(labels) {
			set.Labels[key] = value
		}
		set, err = setstore.New(store).Update(ctx, set)
		if err != nil {
			return err
		}
		return printSet(printer, set, "updated")
	}}
}

func setsMoveCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("sets move", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	setRef := flags.String("set", "", "set id or name")
	parent := flags.String("parent", "", "new parent set id (empty moves to root)")
	return &command.Command{Description: "Move a virtual set under a new parent", Flags: flags, Run: func(ctx context.Context, args []string) error {
		set, err := resolveSet(ctx, store, *setRef)
		if err != nil {
			return err
		}
		parentID, err := resolveParentID(ctx, store, *parent)
		if err != nil {
			return err
		}
		moved, err := setstore.New(store).Move(ctx, set.ID, parentID)
		if err != nil {
			return err
		}
		return printSet(printer, moved, "moved")
	}}
}

func setsDeleteCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("sets delete", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	setRef := flags.String("set", "", "set id or name")
	return &command.Command{Description: "Delete an empty virtual set by id or name", Flags: flags, Run: func(ctx context.Context, args []string) error {
		set, err := resolveSet(ctx, store, *setRef)
		if err != nil {
			return err
		}
		if err := setstore.New(store).Delete(ctx, set.ID); err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(map[string]string{"status": "deleted", "id": set.ID})
			return nil
		}
		printer.PrintText("deleted set %s\n", set.ID)
		return nil
	}}
}

func setsAssignCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("sets assign", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	setRef := flags.String("set", "", "set id or name")
	var add stringListFlag
	flags.Var(&add, "add", "partition to add (repeatable)")
	var remove stringListFlag
	flags.Var(&remove, "remove", "partition to remove (repeatable)")
	primary := flags.Bool("primary", false, "mark added partitions' primary set")
	return &command.Command{Description: "Add or remove partitions from a virtual set", Flags: flags, Run: func(ctx context.Context, args []string) error {
		set, err := resolveSet(ctx, store, *setRef)
		if err != nil {
			return err
		}
		for _, partition := range add {
			if _, statErr := store.Stat(ctx, paths.PartitionConfig(partition)); statErr != nil {
				return fmt.Errorf("partition %q not found", partition)
			}
		}
		updated, err := setstore.New(store).AddPartitions(ctx, set.ID, add, remove, *primary)
		if err != nil {
			return err
		}
		return printSet(printer, updated, "updated")
	}}
}

func setsListCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	return &command.Command{Description: "List virtual sets", Flags: flag.NewFlagSet("sets list", flag.ContinueOnError), Run: func(ctx context.Context, args []string) error {
		sets, err := setstore.New(store).List(ctx)
		if err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(sets)
			return nil
		}
		rows := make([][]string, 0, len(sets))
		for _, set := range sets {
			rows = append(rows, []string{set.ID, set.Name, set.ParentID, fmt.Sprintf("%d", len(set.Partitions)), strings.Join(set.Partitions, ",")})
		}
		printer.PrintTable([]string{"ID", "NAME", "PARENT", "PARTITIONS", "MEMBERS"}, rows)
		return nil
	}}
}

func resolveSet(ctx context.Context, store guardianapi.Store, ref string) (*setstore.Set, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("--set is required")
	}
	sets := setstore.New(store)
	if set, err := sets.Get(ctx, ref); err == nil {
		return set, nil
	}
	return sets.FindByName(ctx, ref)
}

func resolveParentID(ctx context.Context, store guardianapi.Store, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	parent, err := resolveSet(ctx, store, ref)
	if err != nil {
		return "", fmt.Errorf("parent set %q: %w", ref, err)
	}
	return parent.ID, nil
}

func existingPartitions(ctx context.Context, store guardianapi.Store, partitions []string) []string {
	out := make([]string, 0, len(partitions))
	for _, partition := range partitions {
		if _, err := store.Stat(ctx, paths.PartitionConfig(partition)); err != nil {
			fmt.Fprintf(os.Stderr, "warning: partition %s does not exist; skipping\n", partition)
			continue
		}
		out = append(out, partition)
	}
	return out
}

func labelMap(values []string) map[string]string {
	out := map[string]string{}
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key != "" && val != "" {
			out[key] = val
		}
	}
	return out
}

func printSet(printer *output.Printer, set *setstore.Set, verb string) error {
	if printer.Format == "json" {
		printer.PrintJSON(set)
		return nil
	}
	printer.PrintText("set %s %s (%d partitions)\n", set.ID, verb, len(set.Partitions))
	return nil
}
