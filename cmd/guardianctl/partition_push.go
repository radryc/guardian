package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	bundlepkg "github.com/radryc/guardian/internal/bundle"
	"github.com/radryc/guardian/internal/cli/command"
	cliformat "github.com/radryc/guardian/internal/cli/format"
	"github.com/radryc/guardian/internal/cli/output"
	historydomain "github.com/radryc/guardian/internal/domain/history"
	"github.com/radryc/guardian/internal/orchestrator/dispatcher"
	"github.com/radryc/guardian/internal/orchestrator/reconciler"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/versioning/revisions"
	"github.com/radryc/guardian/pkg/guardianapi"
)

type partitionPushResult struct {
	Success             bool                   `json:"success"`
	Partition           string                 `json:"partition"`
	CorrelationID       string                 `json:"correlationId"`
	RemoveMissing       bool                   `json:"removeMissing"`
	WriteBatchRevision  string                 `json:"writeBatchRevisionId,omitempty"`
	DeleteBatchRevision string                 `json:"deleteBatchRevisionId,omitempty"`
	Written             []string               `json:"written"`
	Skipped             []string               `json:"skipped"`
	Deleted             []string               `json:"deleted"`
	Rollouts            []partitionRolloutDiff `json:"rollouts,omitempty"`
}

func partitionPushCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("partition push", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dir := flags.String("dir", "", "local partition directory")
	forceReconcile := flags.Bool("force", false, "trigger a force reconcile after pushing manifests")
	includeSecrets := flags.Bool("include-secrets", false, "include files under secrets/ in the managed bundle")
	removeMissing := flags.Bool("remove-missing", true, "remove managed files not present locally")
	breakGlass := flags.Bool("break-glass", false, "allow a direct write to a repo-managed partition (audited)")
	return &command.Command{Description: "Push a whole partition directory", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if *dir == "" {
			return fmt.Errorf("--dir is required")
		}
		bundle, err := loadLocalPartitionBundle(filepath.Clean(*dir), *includeSecrets)
		if err != nil {
			return err
		}
		if err := guardPartitionWrite(ctx, store, bundle.PartitionName, *breakGlass); err != nil {
			return err
		}
		result, err := pushPartitionBundle(ctx, store, bundle, *removeMissing)
		if err != nil {
			return err
		}
		if *forceReconcile {
			if err := reconcilePushed(ctx, store, bundle); err != nil {
				return err
			}
		}
		printPartitionPushResult(printer, result)
		return nil
	}}
}

// resolveIntentImageRefs applies ${intent.NAME.outputs.ASSET.FIELD} refs in-memory
// using the partition's image state so templates are preserved on disk.
func resolveIntentImageRefs(dir string, files []localIntentFile) error {
	return bundlepkg.ResolveImageRefs(dir, files)
}

func pushPartitionBundle(ctx context.Context, store guardianapi.Store, bundle *localPartitionBundle, removeMissing bool) (*partitionPushResult, error) {
	rollouts, err := planPartitionRolloutDiff(ctx, store, bundle, removeMissing)
	if err != nil {
		return nil, err
	}
	remoteFiles, err := walkFiles(ctx, store, paths.PartitionRoot(bundle.PartitionName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, os.ErrNotExist) {
		remoteFiles = nil
	}

	remoteSet := make(map[string]struct{}, len(remoteFiles))
	for _, logicalPath := range remoteFiles {
		remoteSet[logicalPath] = struct{}{}
	}

	writePaths := make([]string, 0)
	skippedPaths := make([]string, 0)
	writes := make([]guardianapi.PathWrite, 0)
	for _, logicalPath := range bundlepkg.SortedContentKeys(bundle.ManagedFiles) {
		content := bundle.ManagedFiles[logicalPath]
		if _, ok := remoteSet[logicalPath]; !ok {
			writes = append(writes, guardianapi.PathWrite{
				LogicalPath:       logicalPath,
				Content:           content,
				ExpectedVersionID: "absent",
			})
			writePaths = append(writePaths, logicalPath)
			continue
		}

		currentContent, err := store.ReadFile(ctx, logicalPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || isBlobUnavailableError(err) {
				writes = append(writes, guardianapi.PathWrite{
					LogicalPath:       logicalPath,
					Content:           content,
					ExpectedVersionID: "absent",
				})
				writePaths = append(writePaths, logicalPath)
				continue
			}
			return nil, err
		}
		if bytes.Equal(currentContent, content) {
			skippedPaths = append(skippedPaths, logicalPath)
			continue
		}
		info, err := store.Stat(ctx, logicalPath)
		if err != nil {
			return nil, err
		}
		writes = append(writes, guardianapi.PathWrite{
			LogicalPath:       logicalPath,
			Content:           content,
			ExpectedVersionID: info.VersionID,
		})
		writePaths = append(writePaths, logicalPath)
	}

	deletes := make([]guardianapi.PathDelete, 0)
	deletedPaths := make([]string, 0)
	if removeMissing {
		for _, logicalPath := range remoteFiles {
			if !bundlepkg.ManagedPartitionFile(bundle.PartitionName, logicalPath) {
				continue
			}
			if _, ok := bundle.ManagedFiles[logicalPath]; ok {
				continue
			}
			info, err := store.Stat(ctx, logicalPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, err
			}
			deletes = append(deletes, guardianapi.PathDelete{
				LogicalPath:       logicalPath,
				ExpectedVersionID: info.VersionID,
			})
			deletedPaths = append(deletedPaths, logicalPath)
		}
	}

	correlationID := revisions.NewCorrelationID()
	result := &partitionPushResult{
		Success:       true,
		Partition:     bundle.PartitionName,
		CorrelationID: correlationID,
		RemoveMissing: removeMissing,
		Written:       writePaths,
		Skipped:       skippedPaths,
		Deleted:       deletedPaths,
		Rollouts:      rollouts,
	}

	isNewPartition := len(remoteFiles) == 0
	if err := writePartitionPushEvent(ctx, store, bundle.PartitionName, correlationID, isNewPartition, len(writePaths), len(deletedPaths)); err != nil {
		return nil, err
	}

	if len(writes) > 0 {
		batch, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: writes,
			Context: guardianapi.MutationContext{
				PrincipalID:   "guardianctl",
				Reason:        "push partition bundle",
				CorrelationID: correlationID,
			},
		})
		if err != nil {
			return nil, err
		}
		result.WriteBatchRevision = batch.BatchRevisionID
	}
	if len(deletes) > 0 {
		batch, err := store.DeletePaths(ctx, guardianapi.DeleteBatch{
			Deletes: deletes,
			Context: guardianapi.MutationContext{
				PrincipalID:   "guardianctl",
				Reason:        "push partition bundle",
				CorrelationID: correlationID,
			},
		})
		if err != nil {
			return nil, err
		}
		result.DeleteBatchRevision = batch.BatchRevisionID
	}
	return result, nil
}

func printPartitionPushResult(printer *output.Printer, result *partitionPushResult) {
	if printer.Format == cliformat.FormatJSON {
		printer.PrintJSON(result)
		return
	}
	written := len(result.Written)
	skipped := len(result.Skipped)
	deleted := len(result.Deleted)

	fileWord := func(n int) string {
		if n == 1 {
			return "file"
		}
		return "files"
	}

	switch {
	case written == 0 && deleted == 0:
		printer.PrintText(
			"OK partition=%s no manifest changes (%d %s already up to date) correlation=%s\n",
			result.Partition, skipped, fileWord(skipped), result.CorrelationID,
		)
	case written > 0 && deleted == 0:
		printer.PrintText(
			"OK partition=%s wrote %d %s (%d unchanged) correlation=%s\n",
			result.Partition, written, fileWord(written), skipped, result.CorrelationID,
		)
	default:
		printer.PrintText(
			"OK partition=%s wrote %d %s, deleted %d, %d unchanged correlation=%s\n",
			result.Partition, written, fileWord(written), deleted, skipped, result.CorrelationID,
		)
	}

	// Note: rollout diff is computed against intent ASSET versions/contents,
	// not against the on-disk file count above. A change to partition metadata
	// (e.g. config.yaml endpoint label) will write a file but produce no asset
	// diff and therefore no reconcile work.
	printPartitionRolloutDiff(printer, result.Rollouts)
}

func isBlobUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "blob not found") ||
		strings.Contains(msg, "failed to read blob via fetcher")
}

func writePartitionPushEvent(ctx context.Context, store guardianapi.Store, partition, correlationID string, isNewPartition bool, written, deleted int) error {
	eventType := "partition.pushed"
	message := "partition updated"
	if isNewPartition {
		message = "partition created"
	}
	event := &historydomain.EventRecord{
		APIVersion:    "guardian/v1alpha1",
		Kind:          "EventRecord",
		EventID:       revisions.NewEventID(),
		Partition:     partition,
		Type:          eventType,
		Message:       message,
		CorrelationID: correlationID,
		CreatedAt:     time.Now().UTC(),
		Details: map[string]string{
			"is_new":  strconv.FormatBool(isNewPartition),
			"written": strconv.Itoa(written),
			"deleted": strconv.Itoa(deleted),
		},
	}
	content, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return err
	}
	_, err = store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: paths.EventState(partition, event.EventID), Content: content}},
		Context: guardianapi.MutationContext{
			PrincipalID:   "guardianctl",
			Reason:        "write partition push event",
			CorrelationID: correlationID,
		},
	})
	return err
}

func reconcilePushed(ctx context.Context, store guardianapi.Store, bundle *localPartitionBundle) error {
	if bundle == nil || bundle.PartitionName == "" {
		return fmt.Errorf("cannot reconcile: missing partition name")
	}
	disp := dispatcher.NewDispatcher(store, "guardianctl")
	recon := reconciler.NewReconciler(store, disp, time.Minute)
	return recon.ReconcilePartition(ctx, bundle.PartitionName, true)
}
