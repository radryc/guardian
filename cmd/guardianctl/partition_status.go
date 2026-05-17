package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	cliformat "github.com/rydzu/ainfra/guardian/internal/cli/format"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

const (
	defaultPartitionWaitTimeout  = 10 * time.Minute
	defaultPartitionWaitInterval = 2 * time.Second
)

type partitionStatusResult struct {
	Partition        string                       `json:"partition"`
	Status           string                       `json:"status"`
	DisplayStatus    string                       `json:"displayStatus"`
	Summary          string                       `json:"summary,omitempty"`
	LastReconciledAt time.Time                    `json:"lastReconciledAt,omitempty"`
	Metrics          statedomain.PartitionStatusMetrics `json:"metrics,omitempty"`
	IntentStatuses   map[string]string            `json:"intentStatuses,omitempty"`
	Errors           []string                     `json:"errors,omitempty"`
}

func partitionStatusCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("partition status", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	partitionName := flags.String("partition", "", "partition name")
	return &command.Command{Description: "Show partition runtime state", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if *partitionName == "" {
			return fmt.Errorf("--partition is required")
		}
		status, err := loadPartitionStatus(ctx, store, *partitionName)
		if err != nil {
			return err
		}
		printPartitionStatus(printer, status)
		return nil
	}}
}

func partitionWaitCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("partition wait", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	partitionName := flags.String("partition", "", "partition name")
	wantStatus := flags.String("status", "Healthy", "desired partition status")
	timeout := flags.Duration("timeout", defaultPartitionWaitTimeout, "how long to wait")
	interval := flags.Duration("interval", defaultPartitionWaitInterval, "poll interval")
	return &command.Command{Description: "Wait until a partition reaches the requested status", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if *partitionName == "" {
			return fmt.Errorf("--partition is required")
		}
		status, err := waitForPartitionStatus(ctx, store, *partitionName, *wantStatus, *timeout, *interval)
		if err != nil {
			return err
		}
		printPartitionStatus(printer, status)
		return nil
	}}
}

func loadPartitionStatus(ctx context.Context, store guardianapi.Store, partition string) (partitionStatusResult, error) {
	result := partitionStatusResult{Partition: partition}
	if _, err := store.ReadFile(ctx, paths.PartitionConfig(partition)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, fmt.Errorf("partition %s not found", partition)
		}
		return result, err
	}

	state, err := common.LoadPartitionState(ctx, store, partition)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return result, err
		}
		result.Status = "Compiled"
		result.DisplayStatus = "Compiled"
		result.Summary = "Configuration present; waiting for first reconcile"
	} else {
		result.Status = strings.TrimSpace(state.Status)
		result.DisplayStatus = strings.TrimSpace(state.DisplayStatus)
		result.Summary = strings.TrimSpace(state.Summary)
		result.LastReconciledAt = state.LastReconciledAt
		result.Metrics = state.Metrics
		result.Errors = append([]string(nil), state.Errors...)
	}

	intentStates, err := common.LoadAllIntentStates(ctx, store, partition)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if len(intentStates) > 0 {
		result.IntentStatuses = make(map[string]string, len(intentStates))
		for name, state := range intentStates {
			status := string(state.Status)
			if strings.TrimSpace(status) == "" {
				status = string(statedomain.StatusReady)
			}
			result.IntentStatuses[name] = status
		}
	}

	if result.Status == "" {
		result.Status = "Compiled"
	}
	if result.DisplayStatus == "" {
		result.DisplayStatus = result.Status
	}
	if result.Summary == "" {
		result.Summary = result.DisplayStatus
	}
	return result, nil
}

func waitForPartitionStatus(ctx context.Context, store guardianapi.Store, partition, want string, timeout, interval time.Duration) (partitionStatusResult, error) {
	if strings.TrimSpace(want) == "" {
		want = "Healthy"
	}
	if timeout <= 0 {
		timeout = defaultPartitionWaitTimeout
	}
	if interval <= 0 {
		interval = defaultPartitionWaitInterval
	}
	want = strings.TrimSpace(want)
	deadline := time.Now().Add(timeout)
	var last partitionStatusResult
	for {
		status, err := loadPartitionStatus(ctx, store, partition)
		if err != nil {
			return last, err
		}
		last = status
		if strings.EqualFold(status.Status, want) {
			return status, nil
		}
		if strings.EqualFold(want, "Healthy") && (strings.EqualFold(status.Status, "Failing") || strings.EqualFold(status.Status, "Invalid")) {
			return status, fmt.Errorf("partition %s reached %s: %s", partition, status.Status, status.Summary)
		}
		if time.Now().After(deadline) {
			return status, fmt.Errorf("timed out waiting for partition %s to reach %s; last status=%s summary=%s", partition, want, status.Status, status.Summary)
		}
		select {
		case <-ctx.Done():
			return status, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func printPartitionStatus(printer *output.Printer, status partitionStatusResult) {
	if printer.Format == cliformat.FormatJSON {
		printer.PrintJSON(status)
		return
	}
	rows := [][]string{{status.Partition, status.Status, status.DisplayStatus, status.Summary}}
	printer.PrintTable([]string{"PARTITION", "STATUS", "DISPLAY", "SUMMARY"}, rows)
	if len(status.IntentStatuses) == 0 {
		return
	}
	printer.PrintText("\n")
	intentNames := make([]string, 0, len(status.IntentStatuses))
	for name := range status.IntentStatuses {
		intentNames = append(intentNames, name)
	}
	sort.Strings(intentNames)
	intentRows := make([][]string, 0, len(intentNames))
	for _, name := range intentNames {
		intentRows = append(intentRows, []string{name, status.IntentStatuses[name]})
	}
	printer.PrintTable([]string{"INTENT", "STATUS"}, intentRows)
}