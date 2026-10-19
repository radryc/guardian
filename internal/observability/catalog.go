package observability

import (
	"context"
	"os"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

// IdentityRecord is one entry in the observable identity catalog.
type IdentityRecord struct {
	Service   string `json:"service"`
	FullName  string `json:"full_name"`
	Partition string `json:"partition"`
	Intent    string `json:"intent"`
	Asset     string `json:"asset"`
	Pusher    string `json:"pusher,omitempty"`
	Cluster   string `json:"cluster,omitempty"`
	Truncated bool   `json:"truncated"`
}

// Identities derives the canonical telemetry identity for every asset declared
// in the Guardian store. It is intentionally read-only and deterministic so the
// API and CLI can be used to reconcile external telemetry catalogs.
func Identities(ctx context.Context, store guardianapi.ReadStore) ([]IdentityRecord, error) {
	entries, err := store.ListDir(ctx, paths.PartitionsRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return []IdentityRecord{}, nil
		}
		return nil, err
	}
	records := make([]IdentityRecord, 0)
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		partition := entry.Name
		defaultPusher := ""
		defaultCluster := ""
		if raw, err := store.ReadFile(ctx, paths.PartitionConfig(partition)); err == nil {
			if parsed, err := manifest.ParsePartition(raw); err == nil {
				defaultPusher = strings.TrimSpace(parsed.Spec.Defaults.TargetPusher)
				defaultCluster = strings.TrimSpace(parsed.Spec.Defaults.Target.Cluster)
			}
		}
		intentEntries, err := store.ListDir(ctx, paths.PartitionIntentsDir(partition))
		if err != nil {
			continue
		}
		for _, intentEntry := range intentEntries {
			if intentEntry.IsDir || !strings.HasSuffix(intentEntry.Name, ".yaml") {
				continue
			}
			intentName := strings.TrimSuffix(intentEntry.Name, ".yaml")
			raw, err := store.ReadFile(ctx, paths.IntentManifest(partition, intentName))
			if err != nil {
				continue
			}
			parsed, err := manifest.ParseIntent(raw)
			if err != nil {
				continue
			}
			pusher := strings.TrimSpace(parsed.Spec.TargetPusher)
			if pusher == "" {
				pusher = defaultPusher
			}
			cluster := strings.TrimSpace(parsed.Spec.Target.Cluster)
			if cluster == "" {
				cluster = defaultCluster
			}
			for _, asset := range parsed.Spec.Assets {
				identity := CanonicalServiceIdentity(partition, intentName, asset.Name)
				if identity.FullName == "" {
					continue
				}
				records = append(records, IdentityRecord{
					Service:   identity.Name,
					FullName:  identity.FullName,
					Partition: partition,
					Intent:    intentName,
					Asset:     asset.Name,
					Pusher:    pusher,
					Cluster:   cluster,
					Truncated: identity.Truncated,
				})
			}
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Partition != records[j].Partition {
			return records[i].Partition < records[j].Partition
		}
		if records[i].Intent != records[j].Intent {
			return records[i].Intent < records[j].Intent
		}
		return records[i].Asset < records[j].Asset
	})
	return records, nil
}
