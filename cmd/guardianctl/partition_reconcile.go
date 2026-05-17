package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	cliformat "github.com/rydzu/ainfra/guardian/internal/cli/format"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/dispatcher"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/reconciler"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type partitionReconcileResult struct {
	Success   bool                   `json:"success"`
	Partition string                 `json:"partition"`
	Waited    bool                   `json:"waited,omitempty"`
	Status    *partitionStatusResult `json:"status,omitempty"`
}

func partitionReconcileCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("partition reconcile", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	partitionName := flags.String("partition", "", "partition name")
	wait := flags.Bool("wait", false, "wait for the partition to reach the requested status after reconcile")
	waitStatus := flags.String("wait-status", "Healthy", "desired partition status when --wait is set")
	waitTimeout := flags.Duration("wait-timeout", defaultPartitionWaitTimeout, "maximum time to wait when --wait is set")
	waitInterval := flags.Duration("wait-interval", defaultPartitionWaitInterval, "poll interval when --wait is set")
	return &command.Command{Description: "Run one reconciliation cycle for a partition", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if *partitionName == "" {
			return fmt.Errorf("--partition is required")
		}
		if err := reconcilePartition(ctx, store, *partitionName); err != nil {
			return err
		}
		result := partitionReconcileResult{Success: true, Partition: *partitionName}
		if *wait {
			status, err := waitForPartitionStatus(ctx, store, *partitionName, *waitStatus, *waitTimeout, *waitInterval)
			if err != nil {
				return err
			}
			result.Waited = true
			result.Status = &status
		}
		printPartitionReconcileResult(printer, result)
		return nil
	}}
}

func reconcilePartition(ctx context.Context, store guardianapi.Store, partitionName string) error {
	disp := dispatcher.NewDispatcher(store, "guardianctl")
	recon := reconciler.NewReconciler(store, disp, time.Minute)
	return recon.ReconcilePartition(ctx, partitionName, true)
}

func printPartitionReconcileResult(printer *output.Printer, result partitionReconcileResult) {
	if printer.Format == cliformat.FormatJSON {
		printer.PrintJSON(result)
		return
	}
	printer.PrintText("reconciled partition %s\n", result.Partition)
	if result.Status == nil {
		return
	}
	printer.PrintText("\n")
	printPartitionStatus(printer, *result.Status)
}
