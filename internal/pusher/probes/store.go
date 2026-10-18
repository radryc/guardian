package probes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"sort"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func (r *Runner) writeSnapshots(ctx context.Context, results []Result) {
	byPartition := map[string][]Result{}
	for _, result := range results {
		if result.ProbeID == "" {
			continue
		}
		byPartition[result.Partition] = append(byPartition[result.Partition], result)
	}
	for partition, partitionResults := range byPartition {
		sort.Slice(partitionResults, func(i, j int) bool { return partitionResults[i].ProbeID < partitionResults[j].ProbeID })
		snapshot := Snapshot{
			APIVersion: "guardian/v1alpha1",
			Kind:       "ProbeSnapshot",
			Pusher:     r.cfg.Pusher,
			Cluster:    r.cfg.Cluster,
			Partition:  partition,
			UpdatedAt:  time.Now().UTC(),
			Probes:     partitionResults,
		}
		payload, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			log.Printf("[Probes] encode snapshot %s: %v", partition, err)
			continue
		}
		sum := sha256.Sum256(payload)
		hash := hex.EncodeToString(sum[:])
		r.mu.Lock()
		if r.snapshotHashes[partition] == hash {
			r.mu.Unlock()
			continue
		}
		r.snapshotHashes[partition] = hash
		r.mu.Unlock()
		_, err = r.cfg.Store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{
				LogicalPath: paths.ProbeSnapshot(r.cfg.Pusher, partition),
				Content:     payload,
			}},
			Context: guardianapi.MutationContext{
				PrincipalID: "guardian-pusher-" + r.cfg.Pusher,
				Reason:      "write probe snapshot",
			},
		})
		if err != nil {
			log.Printf("[Probes] write snapshot %s: %v", partition, err)
		}
	}
}
