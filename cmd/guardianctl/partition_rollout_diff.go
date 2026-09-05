package main

import (
	"context"
	"errors"
	"os"
	"sort"

	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	"github.com/rydzu/ainfra/guardian/internal/historyquery"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type partitionRolloutDiff struct {
	Intent  string                      `json:"intent"`
	Summary string                      `json:"summary"`
	Assets  []historyquery.RolloutAsset `json:"assets"`
}

func planPartitionRolloutDiff(ctx context.Context, store guardianapi.Store, bundle *localPartitionBundle, removeMissing bool) ([]partitionRolloutDiff, error) {
	remoteIntents, err := loadRemoteIntentManifests(ctx, store, bundle.PartitionName)
	if err != nil {
		return nil, err
	}

	intentNames := make([]string, 0, len(bundle.IntentManifests)+len(remoteIntents))
	seen := make(map[string]struct{}, len(bundle.IntentManifests)+len(remoteIntents))
	for name := range bundle.IntentManifests {
		intentNames = append(intentNames, name)
		seen[name] = struct{}{}
	}
	if removeMissing {
		for name := range remoteIntents {
			if _, ok := seen[name]; ok {
				continue
			}
			intentNames = append(intentNames, name)
		}
	}
	sort.Strings(intentNames)

	rollouts := make([]partitionRolloutDiff, 0, len(intentNames))
	for _, name := range intentNames {
		currentManifest, hasCurrent := bundle.IntentManifests[name]
		previousManifest, hasPrevious := remoteIntents[name]
		if !hasCurrent && !hasPrevious {
			continue
		}
		if !hasCurrent && !removeMissing {
			continue
		}

		var currentSnapshot *historyquery.IntentSnapshot
		if hasCurrent {
			currentSnapshot = &historyquery.IntentSnapshot{Manifest: currentManifest}
		}
		var previousSnapshot *historyquery.IntentSnapshot
		if hasPrevious {
			previousSnapshot = &historyquery.IntentSnapshot{Manifest: previousManifest}
		}

		assets := historyquery.DiffIntentSnapshots(currentSnapshot, previousSnapshot)
		if len(assets) == 0 {
			continue
		}
		rollouts = append(rollouts, partitionRolloutDiff{
			Intent:  name,
			Summary: historyquery.SummarizeIntentAssetDiff(!hasPrevious, assets),
			Assets:  assets,
		})
	}
	return rollouts, nil
}

func loadRemoteIntentManifests(ctx context.Context, store guardianapi.Store, partitionName string) (map[string]*intentdomain.Intent, error) {
	intentNames, err := listIntentNames(ctx, store, partitionName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]*intentdomain.Intent{}, nil
		}
		return nil, err
	}
	intents := make(map[string]*intentdomain.Intent, len(intentNames))
	for _, intentName := range intentNames {
		manifest, err := loadIntentManifest(ctx, store, partitionName, intentName)
		if err != nil {
			return nil, err
		}
		intents[intentName] = manifest
	}
	return intents, nil
}

func formatPartitionRolloutAssets(assets []historyquery.RolloutAsset) string {
	return formatRolloutAssets(assets)
}

func printPartitionRolloutDiff(printer *output.Printer, rollouts []partitionRolloutDiff) {
	if len(rollouts) == 0 {
		printer.PrintText("rollout diff: no asset changes\n")
		return
	}
	rows := make([][]string, 0, len(rollouts))
	for _, rollout := range rollouts {
		rows = append(rows, []string{
			rollout.Intent,
			rollout.Summary,
			formatPartitionRolloutAssets(rollout.Assets),
		})
	}
	printer.PrintText("rollout diff:\n")
	printer.PrintTable([]string{"INTENT", "SUMMARY", "ASSETS"}, rows)
}
