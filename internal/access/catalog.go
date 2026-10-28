package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/radryc/monofs/pkg/authz/policy"
	partitiondomain "github.com/radryc/guardian/internal/domain/partition"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

// LoadCatalog builds the policy catalog (partition names + labels) from the
// live store. Partitions without a config file are skipped; a malformed config
// fails the load so the last good policy is kept.
func LoadCatalog(ctx context.Context, store guardianapi.ReadStore) (*policy.Catalog, error) {
	if store == nil {
		return nil, fmt.Errorf("access policy: catalog requires a store")
	}
	entries, err := store.ListDir(ctx, paths.PartitionsRoot())
	if err != nil {
		return nil, fmt.Errorf("access policy: list partitions: %w", err)
	}
	catalog := &policy.Catalog{}
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		name := entry.Name
		raw, err := store.ReadFile(ctx, paths.PartitionConfig(name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("access policy: read partition %q config: %w", name, err)
		}
		var manifest partitiondomain.Partition
		if err := yaml.Unmarshal(raw, &manifest); err != nil {
			return nil, fmt.Errorf("access policy: parse partition %q config: %w", name, err)
		}
		labels := map[string]string{}
		for key, value := range manifest.Metadata.Labels {
			labels[key] = value
		}
		for key, value := range manifest.Spec.Labels {
			labels[key] = value
		}
		if len(labels) == 0 {
			labels = nil
		}
		catalog.Partitions = append(catalog.Partitions, policy.Partition{Name: name, Labels: labels})
	}
	sort.Slice(catalog.Partitions, func(i, j int) bool {
		return catalog.Partitions[i].Name < catalog.Partitions[j].Name
	})
	return catalog, nil
}

// catalogDigest returns a stable content hash of a catalog so the service can
// skip recompiles when neither the raw policy nor the catalog changed.
func catalogDigest(catalog *policy.Catalog) string {
	if catalog == nil {
		return ""
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
