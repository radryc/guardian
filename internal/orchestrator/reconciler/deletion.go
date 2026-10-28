package reconciler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/compiler/manifest"
	historydomain "github.com/radryc/guardian/internal/domain/history"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	statedomain "github.com/radryc/guardian/internal/domain/state"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/orchestrator/common"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/telemetry"
	"github.com/radryc/guardian/internal/versioning/revisions"
	"github.com/radryc/guardian/pkg/guardianapi"
)

// DeletionRequest is written to /partitions/<name>/.deletion.json to request a
// partition teardown. The reconciler destroys intents only when the partition
// config says deletionPolicy: destroy; otherwise states are orphaned. The
// partition subtree is purged once teardown completes.
type DeletionRequest struct {
	APIVersion  string    `json:"apiVersion"`
	Kind        string    `json:"kind"`
	Partition   string    `json:"partition"`
	RequestedAt time.Time `json:"requestedAt"`
	RequestedBy string    `json:"requestedBy,omitempty"`
}

// RequestPartitionDeletion writes the deletion marker for a configured
// partition. The partition's own deletionPolicy decides whether intents are
// destroyed or orphaned.
func RequestPartitionDeletion(ctx context.Context, store guardianapi.Store, partition, requestedBy string) (*DeletionRequest, error) {
	partition = strings.TrimSpace(partition)
	if partition == "" {
		return nil, fmt.Errorf("partition is required")
	}
	if _, err := store.ReadFile(ctx, paths.PartitionConfig(partition)); err != nil {
		return nil, err
	}
	req := &DeletionRequest{
		APIVersion:  "guardian/v1alpha1",
		Kind:        "PartitionDeletion",
		Partition:   partition,
		RequestedAt: time.Now().UTC(),
		RequestedBy: strings.TrimSpace(requestedBy),
	}
	content, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return nil, err
	}
	principal := strings.TrimSpace(requestedBy)
	if principal == "" {
		principal = "guardian"
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: paths.PartitionDeletion(partition), Content: content}},
		Context: guardianapi.MutationContext{
			PrincipalID:   principal,
			Reason:        "request partition deletion",
			CorrelationID: revisions.NewCorrelationID(),
		},
	}); err != nil {
		return nil, err
	}
	return req, nil
}

func loadDeletionRequest(ctx context.Context, store guardianapi.ReadStore, partition string) (*DeletionRequest, error) {
	data, err := store.ReadFile(ctx, paths.PartitionDeletion(partition))
	if err != nil {
		return nil, err
	}
	var req DeletionRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("parse partition deletion marker: %w", err)
	}
	return &req, nil
}

// reconcilePartitionDeletion drives a marked partition to teardown. Under the
// destroy policy every intent is queued as DESTROY and the partition files are
// purged once all intents report Destroyed. Under orphan the intent states are
// dropped and the partition is purged immediately.
func (r *Reconciler) reconcilePartitionDeletion(ctx context.Context, partitionName string, req *DeletionRequest, configContent []byte) error {
	// Never destroy unless the partition config explicitly says so. A config
	// that cannot be parsed is treated as orphan (purge-only) to avoid deleting
	// live resources on a malformed manifest.
	policy := ""
	if spec, err := manifest.ParsePartition(configContent); err == nil {
		policy = spec.Spec.DeletionPolicy
	}
	policy = normalizedDeletionPolicy(policy)

	states, err := common.LoadAllIntentStates(ctx, r.store, partitionName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if states == nil {
		states = map[string]*statedomain.IntentState{}
	}
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)

	if policy == "destroy" {
		pending := 0
		for _, name := range names {
			state := states[name]
			if state == nil {
				continue
			}
			if state.IntentType == intentdomain.TypeObserved {
				if err := r.dispatcher.DeleteIntentState(ctx, partitionName, name, revisions.NewCorrelationID(), "partition deletion: observed intent removed"); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				delete(states, name)
				continue
			}
			switch state.Status {
			case statedomain.StatusDestroyed:
				continue
			case statedomain.StatusDestroying:
				pending++
				continue
			}
			manifestContent, err := r.loadDeletedIntentManifest(ctx, partitionName, name, state)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					if delErr := r.dispatcher.DeleteIntentState(ctx, partitionName, name, revisions.NewCorrelationID(), "partition deletion: manifest unresolvable, orphaning state"); delErr != nil && !errors.Is(delErr, os.ErrNotExist) {
						return delErr
					}
					delete(states, name)
					continue
				}
				return err
			}
			next, err := common.BuildTaskFromManifest(state, manifestContent, taskdomain.OpDestroy, common.IntentOutputs(states), false)
			if err != nil {
				return err
			}
			correlationID := revisions.NewCorrelationID()
			state.Status = statedomain.StatusDestroying
			state.LastTaskID = next.TaskID
			state.LastError = nil
			state.Timestamps.LastQueuedAt = next.CreatedAt
			state.Timestamps.LastApplyAt = next.CreatedAt
			if err := r.dispatcher.QueueTask(ctx, next); err != nil {
				return err
			}
			if err := r.dispatcher.WriteIntentState(ctx, state); err != nil {
				return err
			}
			details := map[string]string{"deletionPolicy": "destroy"}
			if req != nil && strings.TrimSpace(req.RequestedBy) != "" {
				details["requestedBy"] = strings.TrimSpace(req.RequestedBy)
			}
			_ = r.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
				Partition:     partitionName,
				Intent:        name,
				Type:          "intent.destroying",
				Message:       "partition deletion requested; queued DESTROY",
				TaskID:        next.TaskID,
				CorrelationID: correlationID,
				Details:       details,
			})
			pending++
		}
		if pending > 0 {
			return nil
		}
	} else {
		for _, name := range names {
			if err := r.dispatcher.DeleteIntentState(ctx, partitionName, name, revisions.NewCorrelationID(), "partition deletion (orphan policy)"); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return r.purgePartition(ctx, partitionName)
}

// purgePartition removes every file under the partition root (config, intents,
// state, marker, observed snapshots), drops queued tasks still targeting the
// partition, and stops scheduling it.
func (r *Reconciler) purgePartition(ctx context.Context, partitionName string) error {
	r.purgeQueuedTasks(ctx, partitionName)
	correlationID := revisions.NewCorrelationID()
	files, err := r.walkPartitionFiles(ctx, paths.PartitionRoot(partitionName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(files) > 0 {
		deletes := make([]guardianapi.PathDelete, 0, len(files))
		for _, file := range files {
			deletes = append(deletes, guardianapi.PathDelete{LogicalPath: file})
		}
		if _, err := r.store.DeletePaths(ctx, guardianapi.DeleteBatch{
			Deletes: deletes,
			Context: guardianapi.MutationContext{
				PrincipalID:   "guardiand",
				Reason:        "purge deleted partition",
				CorrelationID: correlationID,
			},
		}); err != nil {
			return err
		}
	}
	r.removePartitionJob(partitionName)
	if r.dispatcher != nil {
		r.dispatcher.ForgetPartition(partitionName)
	}
	// Do not write a partition event here: event storage lives under the
	// partition being removed and would resurrect a partial subtree.
	log.Printf("reconciler: partition=%s purged (%d path(s), correlation=%s)", partitionName, len(files), correlationID)
	telemetry.EmitInfo(ctx, reconcilerScope, fmt.Sprintf("partition %s deleted (%d path(s) removed)", partitionName, len(files)))
	return nil
}

// purgeQueuedTasks drops any queued task (plus claim/result artifacts) that
// still targets the partition so a late pusher claim cannot recreate resources
// after the partition is gone.
func (r *Reconciler) purgeQueuedTasks(ctx context.Context, partitionName string) {
	pushers, err := r.store.ListDir(ctx, paths.QueueRoot())
	if err != nil {
		return
	}
	for _, pusher := range pushers {
		if !pusher.IsDir {
			continue
		}
		dir := paths.QueueDir(pusher.Name)
		entries, err := r.store.ListDir(ctx, dir)
		if err != nil {
			continue
		}
		var deletes []guardianapi.PathDelete
		for _, entry := range entries {
			if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
				continue
			}
			raw, err := r.store.ReadFile(ctx, dir+"/"+entry.Name)
			if err != nil {
				continue
			}
			var task taskdomain.Task
			if json.Unmarshal(raw, &task) != nil || task.Partition != partitionName {
				continue
			}
			taskID := strings.TrimSuffix(entry.Name, ".json")
			deletes = append(deletes,
				guardianapi.PathDelete{LogicalPath: paths.QueueTask(pusher.Name, taskID)},
				guardianapi.PathDelete{LogicalPath: paths.QueueClaim(pusher.Name, taskID)},
				guardianapi.PathDelete{LogicalPath: paths.QueueResult(pusher.Name, taskID)},
			)
		}
		if len(deletes) == 0 {
			continue
		}
		_, _ = r.store.DeletePaths(ctx, guardianapi.DeleteBatch{
			Deletes: deletes,
			Context: guardianapi.MutationContext{
				PrincipalID:   "guardiand",
				Reason:        "purge queued tasks for deleted partition",
				CorrelationID: revisions.NewCorrelationID(),
			},
		})
	}
}

func (r *Reconciler) walkPartitionFiles(ctx context.Context, logicalDir string) ([]string, error) {
	entries, err := r.store.ListDir(ctx, logicalDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		child := strings.TrimRight(logicalDir, "/") + "/" + entry.Name
		if entry.IsDir {
			nested, err := r.walkPartitionFiles(ctx, child)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			out = append(out, nested...)
			continue
		}
		out = append(out, child)
	}
	sort.Strings(out)
	return out, nil
}
