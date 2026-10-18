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
	return &command.Command{Description: "Create or update a virtual set and add partitions idempotently", Flags: flags, Run: func(ctx context.Context, args []string) error {
		setName := strings.TrimSpace(*name)
		if setName == "" {
			return fmt.Errorf("--name is required")
		}
		sets := setstore.New(store)
		set, err := sets.FindByName(ctx, setName)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if os.IsNotExist(err) || set == nil {
			set = &setstore.Set{Name: setName, Description: *description, ParentID: *parent, Labels: labelMap(labels)}
			set, err = sets.Create(ctx, set)
			if err != nil {
				return err
			}
		} else {
			set.Description = *description
			if strings.TrimSpace(*parent) != "" {
				set.ParentID = *parent
			}
			for key, value := range labelMap(labels) {
				set.Labels[key] = value
			}
			set, err = sets.Update(ctx, set)
			if err != nil {
				return err
			}
		}
		var add []string
		for _, partition := range partitions {
			if _, statErr := store.Stat(ctx, paths.PartitionConfig(partition)); statErr != nil {
				fmt.Fprintf(os.Stderr, "warning: partition %s does not exist; skipping\n", partition)
				continue
			}
			add = append(add, partition)
		}
		if len(add) > 0 {
			set, err = sets.AddPartitions(ctx, set.ID, add, nil, *primary)
			if err != nil {
				return err
			}
		}
		if printer.Format == "json" {
			printer.PrintJSON(set)
			return nil
		}
		printer.PrintText("set %s ready (%d partitions)\n", set.ID, len(set.Partitions))
		return nil
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
