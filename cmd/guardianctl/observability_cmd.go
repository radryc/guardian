package main

import (
	"context"
	"flag"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/observability"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func observabilityIdentitiesCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("observability identities", flag.ContinueOnError)
	partition := flags.String("partition", "", "filter by partition")
	intent := flags.String("intent", "", "filter by intent")
	return &command.Command{Description: "List canonical telemetry identities", Flags: flags, Run: func(ctx context.Context, args []string) error {
		records, err := observability.Identities(ctx, store)
		if err != nil {
			return err
		}
		filtered := make([]observability.IdentityRecord, 0, len(records))
		for _, record := range records {
			if *partition != "" && record.Partition != *partition {
				continue
			}
			if *intent != "" && record.Intent != *intent {
				continue
			}
			filtered = append(filtered, record)
		}
		if printer.Format == "json" {
			printer.PrintJSON(filtered)
			return nil
		}
		rows := make([][]string, 0, len(filtered))
		for _, record := range filtered {
			rows = append(rows, []string{
				record.Service,
				record.Partition,
				record.Intent,
				record.Asset,
				record.Pusher,
				record.Cluster,
			})
		}
		printer.PrintTable([]string{"SERVICE", "PARTITION", "INTENT", "ASSET", "PUSHER", "CLUSTER"}, rows)
		return nil
	}}
}
