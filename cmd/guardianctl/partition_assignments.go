package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	manifestpkg "github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type partitionAssignmentRepo struct {
	Name        string `json:"name"`
	Source      string `json:"source,omitempty"`
	Branch      string `json:"branch,omitempty"`
	PayloadPath string `json:"payloadPath"`
	Selector    string `json:"selector,omitempty"`
	Pusher      string `json:"pusher"`
}

type partitionAssignmentPusher struct {
	Name       string   `json:"name"`
	Account    string   `json:"account,omitempty"`
	Region     string   `json:"region,omitempty"`
	Registered bool     `json:"registered"`
	Intents    []string `json:"intents,omitempty"`
}

type partitionAssignmentOutput struct {
	Partition string                      `json:"partition"`
	Repos     []partitionAssignmentRepo   `json:"repos,omitempty"`
	Pushers   []partitionAssignmentPusher `json:"pushers,omitempty"`
	Warnings  []string                    `json:"warnings,omitempty"`
}

func partitionAssignmentsCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("partition assignments", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	partitionName := flags.String("partition", "", "partition name")
	return &command.Command{Description: "Show repo and pusher assignments for a partition", Flags: flags, Run: func(ctx context.Context, args []string) error {
		partition := strings.TrimSpace(*partitionName)
		if partition == "" {
			return fmt.Errorf("--partition is required")
		}
		if _, err := store.Stat(ctx, paths.PartitionConfig(partition)); err != nil {
			return fmt.Errorf("partition %q not found", partition)
		}
		out := partitionAssignmentOutput{Partition: partition}
		repos, err := registry.New(store).ListRepos(ctx)
		if err != nil {
			return err
		}
		for _, repo := range repos {
			for _, binding := range repo.BindingList() {
				if strings.TrimSpace(binding.Partition) != partition {
					continue
				}
				out.Repos = append(out.Repos, partitionAssignmentRepo{
					Name: repo.Name, Source: repo.Source, Branch: repo.Branch,
					PayloadPath: binding.PayloadDir(), Selector: binding.Selector, Pusher: repo.Pusher,
				})
			}
		}
		if len(out.Repos) == 0 {
			out.Warnings = append(out.Warnings, "no repository assigned")
		}
		pushers, err := registry.New(store).ListPushers(ctx)
		if err != nil {
			return err
		}
		pusherByName := map[string]registry.Pusher{}
		for _, pusher := range pushers {
			pusherByName[pusher.Name] = pusher
		}
		intentsByPusher := map[string][]string{}
		entries, err := store.ListDir(ctx, paths.PartitionIntentsDir(partition))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir || !strings.HasSuffix(entry.Name, ".yaml") {
				continue
			}
			data, err := store.ReadFile(ctx, paths.PartitionIntentsDir(partition)+"/"+entry.Name)
			if err != nil {
				continue
			}
			parsed, err := manifestpkg.ParseIntent(data)
			if err != nil {
				continue
			}
			pusher := strings.TrimSpace(parsed.Spec.TargetPusher)
			if pusher == "" {
				continue
			}
			intentsByPusher[pusher] = append(intentsByPusher[pusher], parsed.Metadata.Name)
		}
		for name, intents := range intentsByPusher {
			sort.Strings(intents)
			pusher, ok := pusherByName[name]
			assignment := partitionAssignmentPusher{Name: name, Registered: ok, Intents: intents}
			if ok {
				assignment.Account = pusher.Account
				assignment.Region = pusher.Region
			} else {
				out.Warnings = append(out.Warnings, "target pusher "+name+" is not registered")
			}
			out.Pushers = append(out.Pushers, assignment)
		}
		sort.Slice(out.Repos, func(i, j int) bool { return out.Repos[i].Name < out.Repos[j].Name })
		sort.Slice(out.Pushers, func(i, j int) bool { return out.Pushers[i].Name < out.Pushers[j].Name })
		if printer.Format == "json" {
			printer.PrintJSON(out)
			return nil
		}
		rows := make([][]string, 0, len(out.Repos))
		for _, repo := range out.Repos {
			rows = append(rows, []string{repo.Name, repo.PayloadPath, repo.Selector, repo.Pusher})
		}
		printer.PrintTable([]string{"REPOSITORY", "PAYLOAD PATH", "SELECTOR", "PUSHER"}, rows)
		rows = rows[:0]
		for _, pusher := range out.Pushers {
			rows = append(rows, []string{pusher.Name, pusher.Account, pusher.Region, fmt.Sprintf("%t", pusher.Registered), strings.Join(pusher.Intents, ",")})
		}
		printer.PrintTable([]string{"PUSHER", "ACCOUNT", "REGION", "REGISTERED", "INTENTS"}, rows)
		for _, warning := range out.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
		}
		return nil
	}}
}
