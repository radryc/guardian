package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	cliformat "github.com/rydzu/ainfra/guardian/internal/cli/format"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/reconciler"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type partitionDeleteResult struct {
	Success   bool   `json:"success"`
	Partition string `json:"partition"`
	Purged    bool   `json:"purged"`
	Message   string `json:"message,omitempty"`
}

func partitionDeleteCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("partition delete", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	partitionName := flags.String("partition", "", "partition name")
	force := flags.Bool("force", false, "purge all partition files immediately without destroying resources")
	wait := flags.Bool("wait", false, "wait until teardown completes")
	waitTimeout := flags.Duration("wait-timeout", defaultPartitionWaitTimeout, "maximum time to wait when --wait is set")
	timeoutAlias := flags.Duration("timeout", defaultPartitionWaitTimeout, "alias for --wait-timeout")
	waitInterval := flags.Duration("wait-interval", defaultPartitionWaitInterval, "poll interval when --wait is set")
	return &command.Command{Description: "Delete a partition after destroying its resources", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if *partitionName == "" {
			return fmt.Errorf("--partition is required")
		}
		if *timeoutAlias != defaultPartitionWaitTimeout && *waitTimeout == defaultPartitionWaitTimeout {
			*waitTimeout = *timeoutAlias
		}
		if *force {
			return forceDeletePartition(ctx, store, printer, *partitionName)
		}
		if _, err := reconciler.RequestPartitionDeletion(ctx, store, *partitionName, "guardianctl"); err != nil {
			return err
		}
		// Run one pass so DESTROY tasks are queued without waiting for the
		// partition's reconciliation schedule.
		if err := reconcilePartition(ctx, store, *partitionName, false); err != nil {
			return err
		}
		result := partitionDeleteResult{Success: true, Partition: *partitionName}
		result.Purged = partitionPurged(ctx, store, *partitionName)
		if result.Purged {
			printPartitionDeleteResult(printer, result)
			return nil
		}
		result.Message = "destroy tasks queued (deletionPolicy: destroy); the partition is removed once every intent reports Destroyed (use --force to purge without destroying)"
		if *wait {
			if err := waitForPartitionPurge(ctx, store, *partitionName, *waitTimeout, *waitInterval); err != nil {
				return err
			}
			result.Purged = true
			result.Message = ""
		}
		printPartitionDeleteResult(printer, result)
		return nil
	}}
}

func forceDeletePartition(ctx context.Context, store guardianapi.Store, printer *output.Printer, partitionName string) error {
	correlationID := revisions.NewCorrelationID()
	files, err := walkFiles(ctx, store, paths.PartitionRoot(partitionName))
	if err != nil {
		return err
	}
	if len(files) > 0 {
		deletes := make([]guardianapi.PathDelete, 0, len(files))
		for _, file := range files {
			deletes = append(deletes, guardianapi.PathDelete{LogicalPath: file})
		}
		if _, err := store.DeletePaths(ctx, guardianapi.DeleteBatch{
			Deletes: deletes,
			Context: guardianapi.MutationContext{PrincipalID: "guardianctl", Reason: "force delete partition files", CorrelationID: correlationID},
		}); err != nil {
			return err
		}
	}
	batch, err := store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: paths.PartitionRoot(partitionName)}},
		Context: guardianapi.MutationContext{PrincipalID: "guardianctl", Reason: "force delete partition", CorrelationID: correlationID},
	})
	if err != nil {
		return err
	}
	printMutation(printer, batch, paths.PartitionRoot(partitionName), correlationID)
	return nil
}

func partitionPurged(ctx context.Context, store guardianapi.Store, partitionName string) bool {
	_, err := store.Stat(ctx, paths.PartitionConfig(partitionName))
	return errors.Is(err, os.ErrNotExist)
}

func waitForPartitionPurge(ctx context.Context, store guardianapi.Store, partitionName string, timeout, interval time.Duration) error {
	if timeout <= 0 {
		timeout = defaultPartitionWaitTimeout
	}
	if interval <= 0 {
		interval = defaultPartitionWaitInterval
	}
	deadline := time.Now().Add(timeout)
	for {
		if partitionPurged(ctx, store, partitionName) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for partition %s teardown", timeout, partitionName)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

func printPartitionDeleteResult(printer *output.Printer, result partitionDeleteResult) {
	if printer.Format == cliformat.FormatJSON {
		printer.PrintJSON(result)
		return
	}
	if result.Purged {
		printer.PrintText("deleted partition %s\n", result.Partition)
		return
	}
	printer.PrintText("deletion requested for partition %s\n", result.Partition)
	if result.Message != "" {
		printer.PrintText("%s\n", result.Message)
	}
}
