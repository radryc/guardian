package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	historydomain "github.com/radryc/guardian/internal/domain/history"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/versioning/revisions"
	"github.com/radryc/guardian/pkg/guardianapi"
)

// repoManagedPartition reports whether a partition is fed by a repository
// binding. Such partitions must change through a reviewed PR, not a direct
// control-plane write.
func repoManagedPartition(ctx context.Context, store guardianapi.Store, partition string) (bool, string, error) {
	if store == nil {
		return false, "", nil
	}
	repos, err := registry.New(store).ListRepos(ctx)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "", nil
		}
		return false, "", err
	}
	for _, repo := range repos {
		for _, binding := range repo.Bindings {
			if binding.Partition == partition {
				return true, repo.Name, nil
			}
		}
		if repo.Partition == partition {
			return true, repo.Name, nil
		}
	}
	return false, "", nil
}

func guardPartitionWrite(ctx context.Context, store guardianapi.Store, partition string, breakGlass bool) error {
	managed, repo, err := repoManagedPartition(ctx, store, partition)
	if err != nil {
		return err
	}
	if !managed {
		return nil
	}
	if !breakGlass {
		return fmt.Errorf("partition %q is managed by repository %q; submit a PR/MR through Forge, or pass --break-glass", partition, repo)
	}
	return writeBreakGlassEvent(ctx, store, partition, repo)
}

func writeBreakGlassEvent(ctx context.Context, store guardianapi.Store, partition, repo string) error {
	correlationID := revisions.NewCorrelationID()
	event := &historydomain.EventRecord{
		APIVersion:    "guardian/v1alpha1",
		Kind:          "EventRecord",
		EventID:       revisions.NewEventID(),
		Partition:     partition,
		Type:          "partition.break_glass",
		Message:       fmt.Sprintf("direct write to repo-managed partition (repository %s)", repo),
		CorrelationID: correlationID,
		CreatedAt:     time.Now().UTC(),
		Details:       map[string]string{"repository": repo},
	}
	content, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return err
	}
	_, err = store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: paths.EventState(partition, event.EventID), Content: content}},
		Context: guardianapi.MutationContext{
			PrincipalID:   "guardianctl",
			Reason:        "break-glass partition write",
			CorrelationID: correlationID,
		},
	})
	return err
}
