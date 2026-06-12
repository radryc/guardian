package historyquery

import (
	"time"

	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
)

type IntentSnapshot struct {
	Manifest        *intentdomain.Intent
	AssetVersions   map[string]string
	AssetVersionIDs map[string]string
	FallbackVersion string
	FallbackTime    time.Time
}

func DiffIntentSnapshots(current, previous *IntentSnapshot) []RolloutAsset {
	currentManifest := (*intentdomain.Intent)(nil)
	currentVersions := map[string]string{}
	currentVersionIDs := map[string]string{}
	currentFallbackVersion := ""
	currentFallbackTime := time.Time{}
	if current != nil {
		currentManifest = current.Manifest
		currentVersions = copyStringMap(current.AssetVersions)
		currentVersionIDs = copyStringMap(current.AssetVersionIDs)
		currentFallbackVersion = current.FallbackVersion
		currentFallbackTime = current.FallbackTime
	}

	previousManifest := (*intentdomain.Intent)(nil)
	previousVersions := map[string]string{}
	previousVersionIDs := map[string]string{}
	previousFallbackVersion := ""
	previousFallbackTime := time.Time{}
	if previous != nil {
		previousManifest = previous.Manifest
		previousVersions = copyStringMap(previous.AssetVersions)
		previousVersionIDs = copyStringMap(previous.AssetVersionIDs)
		previousFallbackVersion = previous.FallbackVersion
		previousFallbackTime = previous.FallbackTime
	}

	currentAssets := assetSpecMap(currentManifest)
	previousAssets := assetSpecMap(previousManifest)
	names := sortedStringSetKeys(currentAssets, previousAssets, currentVersionIDs, previousVersionIDs)
	rollouts := make([]RolloutAsset, 0, len(names))
	for _, name := range names {
		currentSpec, hasCurrentSpec := currentAssets[name]
		previousSpec, hasPreviousSpec := previousAssets[name]
		currentVersionID, hasCurrentVersion := currentVersionIDs[name]
		previousVersionID, hasPreviousVersion := previousVersionIDs[name]

		switch {
		case hasCurrentSpec || hasCurrentVersion:
			change := ""
			switch {
			case !hasPreviousSpec && !hasPreviousVersion:
				change = "added"
			case currentVersionID != previousVersionID || !assetSpecEqual(hasCurrentSpec, currentSpec, hasPreviousSpec, previousSpec):
				change = "updated"
			}
			if change == "" {
				continue
			}
			rollouts = append(rollouts, RolloutAsset{
				Name:    name,
				Type:    currentSpec.Type,
				Version: chooseAssetVersion(currentVersions[name], currentSpec.Version, currentVersionID, currentFallbackVersion, currentFallbackTime),
				Change:  change,
			})
		case hasPreviousSpec || hasPreviousVersion:
			rollouts = append(rollouts, RolloutAsset{
				Name:    name,
				Type:    previousSpec.Type,
				Version: chooseAssetVersion(previousVersions[name], previousSpec.Version, previousVersionID, previousFallbackVersion, previousFallbackTime),
				Change:  "removed",
			})
		}
	}
	return rollouts
}

func SummarizeIntentAssetDiff(newIntent bool, assets []RolloutAsset) string {
	return summarizeRollout(newIntent, false, assets)
}
