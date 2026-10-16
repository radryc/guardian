package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/awsgen"
	"github.com/rydzu/ainfra/guardian/internal/awsscan"
	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

// awsScanCommand runs an AWS account scan through a pusher, waits for the
// result, generates a partition bundle draft and optionally pushes it. It is
// the headless equivalent of the UI's Scan & Import flow.
func awsScanCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("aws scan", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	pusher := flags.String("pusher", "", "AWS pusher name (defaults to the only registered pusher)")
	account := flags.String("account", "", "AWS account id (defaults to the pusher registration)")
	partition := flags.String("partition", "", "target partition name for the generated bundle")
	regions := flags.String("regions", awsscan.AllRegions, "comma-separated regions or \"all\"")
	types := flags.String("types", "", "resource kinds to import: bucket,fileSystem,parameter,secret,service,loadBalancer")
	stacks := flags.String("stacks", "", "comma-separated CloudFormation stack name globs")
	grouping := flags.String("grouping", awsgen.GroupingStack, "bundle grouping: stack or resource")
	inventory := flags.Bool("inventory", false, "collect the full resource inventory")
	wait := flags.Bool("wait", true, "wait for the scan result")
	timeout := flags.Duration("timeout", 15*time.Minute, "scan wait timeout")
	interval := flags.Duration("interval", 3*time.Second, "scan poll interval")
	out := flags.String("out", "", "write the partition bundle to this directory")
	push := flags.Bool("push", false, "push the generated bundle to Guardian")
	reconcile := flags.Bool("reconcile", false, "reconcile the partition after pushing")
	return &command.Command{Description: "Run an AWS account scan and build a partition bundle", Flags: flags, Run: func(ctx context.Context, args []string) error {
		registryStore := registry.New(store)
		pusherName, err := resolveScanPusher(ctx, registryStore, *pusher)
		if err != nil {
			return err
		}
		scanAccount := strings.TrimSpace(*account)
		if scanAccount == "" {
			record, err := registryStore.GetPusher(ctx, pusherName)
			if err != nil {
				return fmt.Errorf("read pusher %s: %w", pusherName, err)
			}
			scanAccount = strings.TrimSpace(record.Account)
		}
		if scanAccount == "" {
			return fmt.Errorf("account is required (set --account or register the pusher with an account)")
		}

		request := awsscan.ScanRequest{
			APIVersion:  awsscan.APIVersion,
			Kind:        awsscan.RequestKind,
			ScanID:      "scan-" + revisions.NewCorrelationID(),
			Account:     scanAccount,
			Regions:     splitCSV(*regions),
			Inventory:   *inventory,
			CreatedAt:   time.Now().UTC(),
			RequestedBy: "guardianctl",
		}
		if request.Regions == nil {
			request.Regions = []string{awsscan.AllRegions}
		}
		if *inventory {
			request.InventoryDetail = awsscan.InventoryDetailSummary
		}
		content, err := json.MarshalIndent(request, "", "  ")
		if err != nil {
			return err
		}
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{
				LogicalPath: paths.ScanRequest(pusherName, request.ScanID),
				Content:     content,
			}},
			Context: guardianapi.MutationContext{
				PrincipalID:   "guardianctl",
				Reason:        "create aws scan from guardianctl",
				CorrelationID: request.ScanID,
			},
		}); err != nil {
			return fmt.Errorf("queue scan: %w", err)
		}
		printer.PrintText("queued scan %s on pusher %s (account %s)\n", request.ScanID, pusherName, scanAccount)
		if !*wait {
			return nil
		}

		result, err := waitForScanResult(ctx, store, pusherName, request.ScanID, *timeout, *interval)
		if err != nil {
			return err
		}
		if result.Status != awsscan.ScanStatusSucceeded {
			for _, scanErr := range result.Errors {
				printer.PrintText("  scan error: %s %s: %s\n", scanErr.Region, scanErr.ResourceType, scanErr.Message)
			}
			return fmt.Errorf("scan %s finished with status %s", request.ScanID, result.Status)
		}
		printer.PrintText("scan complete: %d bucket(s), %d filesystem(s), %d parameter(s), %d secret(s), %d service(s), %d load balancer(s), %d managed resource(s)\n",
			len(result.Buckets), len(result.FileSystems), len(result.Parameters), len(result.Secrets), len(result.Services), len(result.LoadBalancers), result.CountManaged())

		if strings.TrimSpace(*partition) == "" {
			printer.PrintText("no --partition set; skipping bundle generation\n")
			return nil
		}
		draft, err := awsgen.Generate(result, awsgen.Options{
			PartitionName: *partition,
			Grouping:      *grouping,
			TargetPusher:  pusherName,
			IncludeTypes:  splitCSV(*types),
			Stacks:        splitCSV(*stacks),
			Regions:       splitCSV(*regions),
		}, store)
		if err != nil {
			return fmt.Errorf("generate bundle: %w", err)
		}

		dir := strings.TrimSpace(*out)
		cleanup := func() {}
		if dir == "" {
			tempDir, err := os.MkdirTemp("", "guardian-scan-bundle-*")
			if err != nil {
				return err
			}
			dir = tempDir
			cleanup = func() { _ = os.RemoveAll(tempDir) }
		}
		defer cleanup()
		if err := writeDraftBundle(dir, draft); err != nil {
			return err
		}
		printer.PrintText("bundle: partition %s, %d intent(s), %d managed, %d unmapped -> %s\n",
			draft.Partition.Metadata.Name, len(draft.Intents), len(draft.Managed), len(draft.Unmapped), dir)
		for _, warning := range draft.Warnings {
			printer.PrintText("  warning: %s\n", warning)
		}

		if *push {
			bundle, err := loadLocalPartitionBundle(dir, false)
			if err != nil {
				return err
			}
			pushResult, err := pushPartitionBundle(ctx, store, bundle, false)
			if err != nil {
				return err
			}
			printPartitionPushResult(printer, pushResult)
			if *reconcile {
				if err := reconcilePushed(ctx, store, bundle); err != nil {
					return err
				}
				printer.PrintText("reconcile triggered for partition %s\n", draft.Partition.Metadata.Name)
			}
		}
		return nil
	}}
}

// resolveScanPusher picks the pusher: the flag value, or the only registered
// pusher.
func resolveScanPusher(ctx context.Context, registryStore *registry.Store, flagValue string) (string, error) {
	name := strings.TrimSpace(flagValue)
	if name != "" {
		return name, nil
	}
	pushers, err := registryStore.ListPushers(ctx)
	if err != nil {
		return "", err
	}
	if len(pushers) == 1 {
		return pushers[0].Name, nil
	}
	if len(pushers) == 0 {
		return "", fmt.Errorf("no pushers registered; set --pusher or register one with 'guardianctl pusher add'")
	}
	names := make([]string, 0, len(pushers))
	for _, pusher := range pushers {
		names = append(names, pusher.Name)
	}
	return "", fmt.Errorf("--pusher is required (registered pushers: %s)", strings.Join(names, ", "))
}

// waitForScanResult polls the scan result file until it appears or the
// timeout elapses.
func waitForScanResult(ctx context.Context, store guardianapi.ReadStore, pusher, scanID string, timeout, interval time.Duration) (*awsscan.ScanResult, error) {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		raw, err := store.ReadFile(ctx, paths.ScanResult(pusher, scanID))
		if err == nil {
			var result awsscan.ScanResult
			if err := json.Unmarshal(raw, &result); err != nil {
				return nil, fmt.Errorf("decode scan result: %w", err)
			}
			return &result, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("scan %s did not finish within %s", scanID, timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// writeDraftBundle writes a scan draft as a local partition bundle.
func writeDraftBundle(dir string, draft *awsgen.Draft) error {
	if err := os.MkdirAll(filepath.Join(dir, "intents"), 0o755); err != nil {
		return err
	}
	config, err := yaml.Marshal(draft.Partition)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), config, 0o644); err != nil {
		return err
	}
	for _, intent := range draft.Intents {
		content, err := yaml.Marshal(intent)
		if err != nil {
			return err
		}
		name := intent.Metadata.Name
		if strings.TrimSpace(name) == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "intents", name+".yaml"), content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func splitCSV(raw string) []string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	out := make([]string, 0, strings.Count(value, ",")+1)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
