package main

import (
	"context"
	"encoding/json"
	"flag"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/probes"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func probeListCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("probe list", flag.ContinueOnError)
	service := flags.String("service", "", "filter by target service")
	pusher := flags.String("pusher", "", "filter by pusher")
	cluster := flags.String("cluster", "", "filter by cluster")
	partition := flags.String("partition", "", "filter by partition")
	status := flags.String("status", "", "filter by status")
	return &command.Command{Description: "List latest probe results", Flags: flags, Run: func(ctx context.Context, args []string) error {
		results := collectProbes(ctx, store)
		filtered := make([]probes.Result, 0, len(results))
		for _, result := range results {
			if *service != "" && result.Service != *service {
				continue
			}
			if *pusher != "" && result.Pusher != *pusher {
				continue
			}
			if *cluster != "" && result.Cluster != *cluster {
				continue
			}
			if *partition != "" && result.Partition != *partition {
				continue
			}
			if *status != "" && result.Status != *status {
				continue
			}
			filtered = append(filtered, result)
		}
		probes.SortResults(filtered)
		if printer.Format == "json" {
			printer.PrintJSON(filtered)
			return nil
		}
		rows := make([][]string, 0, len(filtered))
		for _, result := range filtered {
			rows = append(rows, []string{result.ProbeID, result.Service, result.Pusher, result.Partition, result.Name, result.Type, result.Status, result.Target})
		}
		printer.PrintTable([]string{"PROBE", "SERVICE", "PUSHER", "PARTITION", "NAME", "TYPE", "STATUS", "TARGET"}, rows)
		return nil
	}}
}

func probeShowCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("probe show", flag.ContinueOnError)
	id := flags.String("id", "", "probe id")
	return &command.Command{Description: "Show one probe result", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*id) == "" {
			return probeErr("--id is required")
		}
		for _, result := range collectProbes(ctx, store) {
			if result.ProbeID == *id {
				printer.PrintJSON(result)
				return nil
			}
		}
		return probeErr("probe not found")
	}}
}

func collectProbes(ctx context.Context, store guardianapi.Store) []probes.Result {
	entries, err := store.ListDir(ctx, paths.ProbesRoot())
	if err != nil {
		return nil
	}
	out := make([]probes.Result, 0)
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		snapshots, err := store.ListDir(ctx, paths.ProbesDir(entry.Name))
		if err != nil {
			continue
		}
		for _, snapshotEntry := range snapshots {
			if snapshotEntry.IsDir || !strings.HasSuffix(snapshotEntry.Name, ".json") {
				continue
			}
			partition := strings.TrimSuffix(snapshotEntry.Name, ".json")
			raw, err := store.ReadFile(ctx, paths.ProbeSnapshot(entry.Name, partition))
			if err != nil {
				continue
			}
			var snapshot probes.Snapshot
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				continue
			}
			out = append(out, snapshot.Probes...)
		}
	}
	return out
}

type probeErr string

func (e probeErr) Error() string { return string(e) }
