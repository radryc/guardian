package historyquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"

	manifestpkg "github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type RolloutRecord struct {
	Partition          string                 `json:"partition"`
	Intent             string                 `json:"intent"`
	DeploymentRevision string                 `json:"deploymentRevision"`
	CreatedAt          time.Time              `json:"createdAt"`
	Target             targetdomain.Placement `json:"target,omitempty"`
	TaskIDs            []string               `json:"taskIDs,omitempty"`
	Current            bool                   `json:"current,omitempty"`
	NewIntent          bool                   `json:"newIntent,omitempty"`
	SelfHealing        bool                   `json:"selfHealing,omitempty"`
	Summary            string                 `json:"summary"`
	Assets             []RolloutAsset         `json:"assets"`
}

type RolloutAsset struct {
	Name    string `json:"name"`
	Type    string `json:"type,omitempty"`
	Version string `json:"version,omitempty"`
	Change  string `json:"change"`
}

type archivedRollout struct {
	record historydomain.DeploymentRecord
	intent *intentdomain.Intent
}

func LoadPartitionRollouts(ctx context.Context, store guardianapi.ReadStore, partitionName string, filter DeploymentFilter) ([]RolloutRecord, error) {
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	intentNames, err := listArchivedIntentNames(ctx, store, partitionName)
	if err != nil {
		return nil, err
	}
	rollouts := make([]RolloutRecord, 0)
	for _, intentName := range intentNames {
		archives, err := loadArchivedRollouts(ctx, store, partitionName, intentName)
		if err != nil {
			return nil, err
		}
		grouped := collapseEquivalentRollouts(archives)
		currentVisible := true
		for idx, archive := range grouped {
			if !filter.Match(archive.record.CreatedAt) {
				continue
			}
			var previous *archivedRollout
			if idx+1 < len(grouped) {
				previous = &grouped[idx+1]
			}
			rollout, err := buildRolloutRecord(archive, previous, currentVisible)
			if err != nil {
				return nil, err
			}
			rollouts = append(rollouts, rollout)
			currentVisible = false
		}
	}
	sort.Slice(rollouts, func(i, j int) bool {
		if rollouts[i].CreatedAt.Equal(rollouts[j].CreatedAt) {
			if rollouts[i].Partition == rollouts[j].Partition {
				if rollouts[i].Intent == rollouts[j].Intent {
					return rollouts[i].DeploymentRevision > rollouts[j].DeploymentRevision
				}
				return rollouts[i].Intent < rollouts[j].Intent
			}
			return rollouts[i].Partition < rollouts[j].Partition
		}
		return rollouts[i].CreatedAt.After(rollouts[j].CreatedAt)
	})
	if filter.Limit > 0 && len(rollouts) > filter.Limit {
		return append([]RolloutRecord(nil), rollouts[:filter.Limit]...), nil
	}
	return rollouts, nil
}

func loadArchivedRollouts(ctx context.Context, store guardianapi.ReadStore, partitionName, intentName string) ([]archivedRollout, error) {
	records, err := LoadDeploymentRecords(ctx, store, partitionName, intentName, DeploymentFilter{})
	if err != nil {
		return nil, err
	}
	archives := make([]archivedRollout, 0, len(records))
	for _, record := range records {
		currentIntent, err := loadArchivedIntentManifest(ctx, store, record)
		if err != nil {
			return nil, err
		}
		archives = append(archives, archivedRollout{record: record, intent: currentIntent})
	}
	return archives, nil
}

func collapseEquivalentRollouts(archives []archivedRollout) []archivedRollout {
	if len(archives) < 2 {
		return append([]archivedRollout(nil), archives...)
	}
	grouped := make([]archivedRollout, 0, len(archives))
	for _, archive := range archives {
		if len(grouped) > 0 && rolloutStateEqual(archive, grouped[len(grouped)-1]) && !hasChangedAssets(archive.record) {
			continue
		}
		grouped = append(grouped, archive)
	}
	return grouped
}

func buildRolloutRecord(current archivedRollout, previous *archivedRollout, currentRollout bool) (RolloutRecord, error) {
	selfHealing := current.record.SelfHealing
	if !selfHealing {
		selfHealing = previous != nil && rolloutStateEqual(current, *previous) && hasChangedAssets(current.record)
	}
	var previousSnapshot *IntentSnapshot
	if previous != nil && !selfHealing {
		previousSnapshot = &IntentSnapshot{
			Manifest:        previous.intent,
			AssetVersions:   previous.record.AssetVersions,
			AssetVersionIDs: previous.record.AssetVersionIDs,
			FallbackVersion: previous.record.DeploymentRevision,
			FallbackTime:    previous.record.CreatedAt,
		}
	}
	assets := DiffIntentSnapshots(&IntentSnapshot{
		Manifest:        current.intent,
		AssetVersions:   current.record.AssetVersions,
		AssetVersionIDs: current.record.AssetVersionIDs,
		FallbackVersion: current.record.DeploymentRevision,
		FallbackTime:    current.record.CreatedAt,
	}, previousSnapshot)
	if selfHealing || len(assets) == 0 {
		assets = fallbackRolloutAssets(current.record, current.intent, previous == nil, selfHealing)
	}
	return RolloutRecord{
		Partition:          current.record.Partition,
		Intent:             current.record.Intent,
		DeploymentRevision: current.record.DeploymentRevision,
		CreatedAt:          current.record.CreatedAt,
		Target:             current.record.Target,
		TaskIDs:            append([]string(nil), current.record.TaskIDs...),
		Current:            currentRollout,
		NewIntent:          previous == nil,
		SelfHealing:        selfHealing,
		Summary:            summarizeRollout(previous == nil, selfHealing, assets),
		Assets:             assets,
	}, nil
}

func hasChangedAssets(record historydomain.DeploymentRecord) bool {
	for _, name := range record.ChangedAssets {
		if strings.TrimSpace(name) != "" {
			return true
		}
	}
	return false
}

func loadArchivedIntentManifest(ctx context.Context, store guardianapi.ReadStore, record historydomain.DeploymentRecord) (*intentdomain.Intent, error) {
	content, err := store.ReadFile(ctx, paths.ArchiveManifest(record.Partition, record.Intent, record.DeploymentRevision))
	if err != nil {
		return nil, err
	}
	return manifestpkg.ParseIntent(content)
}

func rolloutStateEqual(current, previous archivedRollout) bool {
	if !reflect.DeepEqual(current.record.Target, previous.record.Target) {
		return false
	}
	if !stringMapEqual(current.record.AssetVersionIDs, previous.record.AssetVersionIDs) {
		return false
	}
	if !stringMapEqual(current.record.AssetVersions, previous.record.AssetVersions) {
		return false
	}
	currentAssets := assetSpecMap(current.intent)
	previousAssets := assetSpecMap(previous.intent)
	names := sortedStringSetKeys(currentAssets, previousAssets, nil, nil)
	for _, name := range names {
		currentSpec, hasCurrent := currentAssets[name]
		previousSpec, hasPrevious := previousAssets[name]
		if !assetSpecEqual(hasCurrent, currentSpec, hasPrevious, previousSpec) {
			return false
		}
	}
	return true
}

func fallbackRolloutAssets(record historydomain.DeploymentRecord, currentIntent *intentdomain.Intent, newIntent bool, selfHealing bool) []RolloutAsset {
	assets := assetSpecMap(currentIntent)
	names := append([]string(nil), record.ChangedAssets...)
	if len(names) == 0 {
		names = sortedStringSetKeys(assets, nil, record.AssetVersionIDs, nil)
	}
	seen := make(map[string]struct{}, len(names))
	rollouts := make([]RolloutAsset, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		spec := assets[name]
		change := "updated"
		if newIntent {
			change = "added"
		} else if selfHealing {
			change = "refreshed"
		}
		rollouts = append(rollouts, RolloutAsset{
			Name:    name,
			Type:    strings.TrimSpace(spec.Type),
			Version: chooseAssetVersion(record.AssetVersions[name], spec.Version, record.AssetVersionIDs[name], record.DeploymentRevision, record.CreatedAt),
			Change:  change,
		})
	}
	sort.Slice(rollouts, func(i, j int) bool {
		return rollouts[i].Name < rollouts[j].Name
	})
	return rollouts
}

func listArchivedIntentNames(ctx context.Context, store guardianapi.ReadStore, partitionName string) ([]string, error) {
	logicalDir := path.Join(paths.ArchiveRoot(), partitionName)
	entries, err := listArchivePartitionEntries(ctx, store, logicalDir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	return names, nil
}

func listArchivePartitionEntries(ctx context.Context, store guardianapi.ReadStore, logicalDir string) ([]guardianapi.DirEntry, error) {
	if paged, ok := store.(guardianapi.PagedDirLister); ok {
		out := make([]guardianapi.DirEntry, 0)
		offset := 0
		for {
			page, err := paged.ListDirPage(ctx, logicalDir, guardianapi.DirListOptions{Offset: offset, Limit: archiveScanPageSize})
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil, nil
				}
				return nil, err
			}
			out = append(out, page.Entries...)
			if !page.HasMore {
				return out, nil
			}
			offset = page.NextOffset
		}
	}
	entries, err := store.ListDir(ctx, logicalDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

func summarizeRollout(newIntent bool, selfHealing bool, assets []RolloutAsset) string {
	counts := map[string]int{}
	for _, asset := range assets {
		counts[asset.Change]++
	}
	parts := make([]string, 0, 4)
	if counts["added"] > 0 {
		parts = append(parts, summarizeChangeCount(counts["added"], "added"))
	}
	if counts["updated"] > 0 {
		parts = append(parts, summarizeChangeCount(counts["updated"], "updated"))
	}
	if counts["refreshed"] > 0 {
		parts = append(parts, summarizeChangeCount(counts["refreshed"], "refreshed"))
	}
	if counts["removed"] > 0 {
		parts = append(parts, summarizeChangeCount(counts["removed"], "removed"))
	}
	if len(parts) == 0 {
		if newIntent {
			return "Initial rollout archived"
		}
		if selfHealing {
			return "Self-heal archived"
		}
		return "Rollout archived"
	}
	prefix := "Rollout"
	if newIntent {
		prefix = "Initial rollout"
	} else if selfHealing {
		prefix = "Self-heal"
	}
	return fmt.Sprintf("%s: %s", prefix, strings.Join(parts, ", "))
}

func summarizeChangeCount(count int, action string) string {
	if count == 1 {
		return fmt.Sprintf("1 asset %s", action)
	}
	return fmt.Sprintf("%d assets %s", count, action)
}

func assetSpecMap(intent *intentdomain.Intent) map[string]assetdomain.Spec {
	if intent == nil {
		return map[string]assetdomain.Spec{}
	}
	assets := make(map[string]assetdomain.Spec, len(intent.Spec.Assets))
	for _, asset := range intent.Spec.Assets {
		assets[asset.Name] = asset
	}
	return assets
}

func chooseAssetVersion(recordVersion, specVersion, assetVersionID, deploymentRevision string, fallbackTime time.Time) string {
	for _, value := range []string{recordVersion, specVersion} {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	if derived := revisions.DerivedAssetVersionAt(assetVersionID, fallbackTime); derived != "" {
		return derived
	}
	return strings.TrimSpace(deploymentRevision)
}

func assetSpecEqual(hasCurrent bool, current assetdomain.Spec, hasPrevious bool, previous assetdomain.Spec) bool {
	if hasCurrent != hasPrevious {
		return false
	}
	if !hasCurrent {
		return true
	}
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return false
	}
	previousJSON, err := json.Marshal(previous)
	if err != nil {
		return false
	}
	return string(currentJSON) == string(previousJSON)
}

func sortedStringSetKeys(assetMaps ...any) []string {
	set := map[string]struct{}{}
	for _, value := range assetMaps {
		switch typed := value.(type) {
		case map[string]assetdomain.Spec:
			for key := range typed {
				set[key] = struct{}{}
			}
		case map[string]string:
			for key := range typed {
				set[key] = struct{}{}
			}
		case nil:
		default:
			panic(fmt.Sprintf("unsupported map type %T", value))
		}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringMapEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
