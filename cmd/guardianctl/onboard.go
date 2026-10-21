package main

import (
	"context"
	"flag"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/observed"
	"github.com/rydzu/ainfra/guardian/internal/onboard"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

// awsOnboardCommand materializes observed partitions from the onboarding
// descriptors published by customer CDK stacks. It is the headless equivalent
// of the onboarding controller.
func awsOnboardCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("aws onboard", flag.ContinueOnError)
	pusher := flags.String("pusher", "", "AWS pusher name (default: every registered pusher)")
	stack := flags.String("stack", "", "only onboard this CloudFormation stack")
	partition := flags.String("partition", "", "only onboard this partition")
	reconcile := flags.Bool("reconcile", true, "reconcile the affected partitions after writing")
	return &command.Command{Description: "Create observed partitions from customer onboarding descriptors", Flags: flags, Run: func(ctx context.Context, args []string) error {
		registryStore := registry.New(store)
		resolver := &observed.RegistryResolver{Registry: registryStore}
		service := &onboard.Service{
			Store:    store,
			Registry: registryStore,
			Resolver: resolver,
		}
		results, runErr := service.Run(ctx, onboard.Options{
			Pusher:    *pusher,
			Stack:     *stack,
			Partition: *partition,
			Reconcile: *reconcile,
		})
		if printer.Format == "json" {
			printer.PrintJSON(map[string]any{
				"results": results,
				"error":   errorString(runErr),
			})
			return runErr
		}
		for _, result := range results {
			if result.Error != "" {
				printer.PrintText("%s/%s -> %s/%s error: %s\n", result.Pusher, result.Stack, result.Partition, result.Intent, result.Error)
				continue
			}
			printer.PrintText("%s/%s -> %s/%s probes=%d\n", result.Pusher, result.Stack, result.Partition, result.Intent, result.Probes)
		}
		if runErr != nil {
			return runErr
		}
		if len(results) == 0 {
			printer.PrintText("no onboarding descriptors found\n")
		}
		return nil
	}}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
