package probes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func (r *Runner) writeSnapshots(ctx context.Context) {
	r.mu.Lock()
	partitions := make(map[string]struct{}, len(r.snapshotHashes))
	for partition := range r.snapshotHashes {
		partitions[partition] = struct{}{}
	}
	latest := make([]Result, 0, len(r.previous))
	for _, result := range r.previous {
		if result.ProbeID == "" {
			continue
		}
		partitions[result.Partition] = struct{}{}
		latest = append(latest, result)
	}
	r.mu.Unlock()

	SortResults(latest)
	byPartition := map[string][]Result{}
	for _, result := range latest {
		byPartition[result.Partition] = append(byPartition[result.Partition], result)
	}

	for partition := range partitions {
		partitionResults := byPartition[partition]
		if partitionResults == nil {
			partitionResults = []Result{}
		}
		snapshot := Snapshot{
			APIVersion: "guardian/v1alpha1",
			Kind:       "ProbeSnapshot",
			Pusher:     r.cfg.Pusher,
			Cluster:    r.cfg.Cluster,
			Partition:  partition,
			WorkerID:   r.cfg.WorkerID,
			ShardIndex: r.cfg.ShardIndex,
			ShardCount: r.cfg.ShardCount,
			UpdatedAt:  time.Now().UTC(),
			Probes:     partitionResults,
		}

		// UpdatedAt must not defeat content-hash suppression. Hash the probe
		// content, then only stamp/update the file when the content changed.
		hashSnapshot := snapshot
		hashSnapshot.UpdatedAt = time.Time{}
		hashPayload, err := json.Marshal(hashSnapshot)
		if err != nil {
			log.Printf("[Probes] encode snapshot %s: %v", partition, err)
			continue
		}
		sum := sha256.Sum256(hashPayload)
		hash := hex.EncodeToString(sum[:])

		r.mu.Lock()
		unchanged := r.snapshotHashes[partition] == hash
		r.mu.Unlock()
		if unchanged {
			continue
		}

		payload, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			log.Printf("[Probes] encode snapshot %s: %v", partition, err)
			continue
		}
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
			continue
		}
		r.mu.Lock()
		r.snapshotHashes[partition] = hash
		r.mu.Unlock()
	}
}
