package common

import (
	"context"
	"errors"
	"os"
	"sort"

	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

// LoadPartitionRuntime builds the partition runtime by reading the partition
// state file and scanning the authoritative per-intent state files. There is no
// persisted aggregate snapshot: writing one on every intent transition caused
// O(intents) write amplification, so per-intent files are the source of truth
// and the aggregate is derived on read.
func LoadPartitionRuntime(ctx context.Context, store guardianapi.ReadStore, partition string) (*statedomain.PartitionRuntime, error) {
	runtime, err := scanPartitionRuntime(ctx, store, partition)
	if err != nil {
		return nil, err
	}
	runtime = statedomain.NormalizePartitionRuntime(runtime)
	partitionRuntimeLoadsTotal.WithLabelValues("scan").Inc()
	partitionRuntimeIntentStatesTotal.WithLabelValues("scan").Add(float64(len(runtime.Intents)))
	return runtime, nil
}

func scanPartitionRuntime(ctx context.Context, store guardianapi.ReadStore, partition string) (*statedomain.PartitionRuntime, error) {
	runtime := statedomain.NewPartitionRuntime(partition)
	found := false

	partitionState, err := loadPartitionStateFile(ctx, store, partition)
	if err == nil {
		runtime.PartitionState = partitionState
		found = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	entries, err := store.ListDir(ctx, paths.StateIntentsDir(partition))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		for _, entry := range entries {
			if entry.IsDir || len(entry.Name) < 6 || entry.Name[len(entry.Name)-5:] != ".json" {
				continue
			}
			intentName := entry.Name[:len(entry.Name)-5]
			state, err := LoadIntentState(ctx, store, partition, intentName)
			if err != nil {
				return nil, err
			}
			runtime.Intents[intentName] = state
			found = true
		}
	}

	if !found {
		return nil, os.ErrNotExist
	}
	return runtime, nil
}
