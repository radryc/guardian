package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	cliformat "github.com/rydzu/ainfra/guardian/internal/cli/format"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	"github.com/rydzu/ainfra/guardian/internal/historyquery"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/dispatcher"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type partitionTagRecord struct {
	Partition string    `json:"partition"`
	Tag       string    `json:"tag"`
	CreatedAt time.Time `json:"createdAt"`
	Current   bool      `json:"current,omitempty"`
	Intents   []string  `json:"intents,omitempty"`
}

type partitionRollbackIntentResult struct {
	Intent             string `json:"intent"`
	DeploymentRevision string `json:"deploymentRevision"`
}

type partitionRollbackResult struct {
	Success         bool                            `json:"success"`
	Partition       string                          `json:"partition"`
	Tag             string                          `json:"tag"`
	CorrelationID   string                          `json:"correlationId"`
	BatchRevisionID string                          `json:"batchRevisionId,omitempty"`
	RestoredIntents []partitionRollbackIntentResult `json:"restoredIntents,omitempty"`
}

func partitionTagsCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("partition tags", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	partitionName := flags.String("partition", "", "partition name")
	return &command.Command{Description: "List partition tags", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*partitionName) == "" {
			return fmt.Errorf("--partition is required")
		}
		tags, err := loadPartitionTags(ctx, store, strings.TrimSpace(*partitionName))
		if err != nil {
			return err
		}
		if printer.Format == cliformat.FormatJSON {
			printer.PrintJSON(tags)
			return nil
		}
		if len(tags) == 0 {
			printer.PrintText("no partition tags found for %s\n", *partitionName)
			return nil
		}
		rows := make([][]string, 0, len(tags))
		for _, tag := range tags {
			current := ""
			if tag.Current {
				current = "current"
			}
			rows = append(rows, []string{current, tag.Tag, tag.CreatedAt.Format(time.RFC3339), strings.Join(tag.Intents, ",")})
		}
		printer.PrintTable([]string{"CURRENT", "TAG", "CREATED", "INTENTS"}, rows)
		return nil
	}}
}

func partitionRollbackCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("partition rollback", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	partitionName := flags.String("partition", "", "partition name")
	tag := flags.String("tag", "", "partition tag/version to restore")
	previous := flags.Bool("previous", false, "restore the tag immediately before the current partition tag")
	return &command.Command{Description: "Restore all archived intent manifests for a selected partition tag", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*partitionName) == "" {
			return fmt.Errorf("--partition is required")
		}
		resolvedTag, err := resolvePartitionRollbackTag(ctx, store, strings.TrimSpace(*partitionName), strings.TrimSpace(*tag), *previous)
		if err != nil {
			return err
		}
		result, err := rollbackPartitionToTag(ctx, store, strings.TrimSpace(*partitionName), resolvedTag)
		if err != nil {
			return err
		}
		printPartitionRollbackResult(printer, result)
		return nil
	}}
}

func loadPartitionTags(ctx context.Context, store guardianapi.ReadStore, partitionName string) ([]partitionTagRecord, error) {
	rollouts, err := historyquery.LoadPartitionRollouts(ctx, store, partitionName, historyquery.DeploymentFilter{})
	if err != nil {
		return nil, err
	}
	byTag := map[string]*partitionTagRecord{}
	currentTags := map[string]struct{}{}
	for _, rollout := range rollouts {
		tag := partitionTagFromRolloutAssets(rollout.Assets)
		if tag == "" {
			continue
		}
		if rollout.Current {
			currentTags[tag] = struct{}{}
		}
		record, ok := byTag[tag]
		if !ok {
			record = &partitionTagRecord{Partition: partitionName, Tag: tag, CreatedAt: rollout.CreatedAt}
			byTag[tag] = record
		}
		if rollout.CreatedAt.After(record.CreatedAt) {
			record.CreatedAt = rollout.CreatedAt
		}
		if !containsString(record.Intents, rollout.Intent) {
			record.Intents = append(record.Intents, rollout.Intent)
			sort.Strings(record.Intents)
		}
	}
	tags := make([]partitionTagRecord, 0, len(byTag))
	for _, record := range byTag {
		if _, ok := currentTags[record.Tag]; ok {
			record.Current = true
		}
		tags = append(tags, *record)
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].CreatedAt.Equal(tags[j].CreatedAt) {
			return tags[i].Tag > tags[j].Tag
		}
		return tags[i].CreatedAt.After(tags[j].CreatedAt)
	})
	return tags, nil
}

func resolvePartitionRollbackTag(ctx context.Context, store guardianapi.ReadStore, partitionName, explicitTag string, previous bool) (string, error) {
	selectorCount := 0
	if explicitTag != "" {
		selectorCount++
	}
	if previous {
		selectorCount++
	}
	if selectorCount != 1 {
		return "", fmt.Errorf("exactly one of --tag or --previous is required")
	}
	if explicitTag != "" {
		return explicitTag, nil
	}

	tags, err := loadPartitionTags(ctx, store, partitionName)
	if err != nil {
		return "", err
	}
	currentTags := currentPartitionTags(tags)
	if len(currentTags) == 0 {
		return "", fmt.Errorf("no current partition tag found for %s; use --tag explicitly", partitionName)
	}
	if len(currentTags) > 1 {
		names := make([]string, 0, len(currentTags))
		for _, tag := range currentTags {
			names = append(names, tag.Tag)
		}
		sort.Strings(names)
		return "", fmt.Errorf("partition %s has multiple current tags (%s); use --tag explicitly", partitionName, strings.Join(names, ", "))
	}
	for _, tag := range tags {
		if tag.Tag == currentTags[0].Tag {
			continue
		}
		return tag.Tag, nil
	}
	return "", fmt.Errorf("no previous partition tag found for %s", partitionName)
}

func rollbackPartitionToTag(ctx context.Context, store guardianapi.Store, partitionName, tag string) (partitionRollbackResult, error) {
	targets, err := loadPartitionRollbackTargets(ctx, store, partitionName, tag)
	if err != nil {
		return partitionRollbackResult{}, err
	}
	if len(targets) == 0 {
		return partitionRollbackResult{}, fmt.Errorf("no archived manifests found for partition %s with tag %q", partitionName, tag)
	}

	correlationID := revisions.NewCorrelationID()
	intentNames := make([]string, 0, len(targets))
	for intentName := range targets {
		intentNames = append(intentNames, intentName)
	}
	sort.Strings(intentNames)

	writes := make([]guardianapi.PathWrite, 0, len(intentNames))
	result := partitionRollbackResult{Success: true, Partition: partitionName, Tag: tag, CorrelationID: correlationID, RestoredIntents: make([]partitionRollbackIntentResult, 0, len(intentNames))}
	records := make(map[string]historydomain.DeploymentRecord, len(intentNames))
	for _, intentName := range intentNames {
		deploymentRevision := targets[intentName]
		manifestContent, record, err := loadRollbackManifest(ctx, store, partitionName, intentName, deploymentRevision)
		if err != nil {
			return partitionRollbackResult{}, err
		}
		records[intentName] = *record
		writes = append(writes, guardianapi.PathWrite{LogicalPath: paths.IntentManifest(partitionName, intentName), Content: manifestContent})
		result.RestoredIntents = append(result.RestoredIntents, partitionRollbackIntentResult{Intent: intentName, DeploymentRevision: deploymentRevision})
	}

	batch, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: writes,
		Context: guardianapi.MutationContext{
			PrincipalID:   "guardianctl",
			Reason:        "partition rollback",
			CorrelationID: correlationID,
		},
	})
	if err != nil {
		return partitionRollbackResult{}, err
	}
	result.BatchRevisionID = batch.BatchRevisionID

	disp := dispatcher.NewDispatcher(store, "guardianctl")
	for _, restored := range result.RestoredIntents {
		record := records[restored.Intent]
		if err := disp.WriteEvent(ctx, &historydomain.EventRecord{
			Partition:          partitionName,
			Intent:             restored.Intent,
			Type:               "rollback.applied",
			Message:            fmt.Sprintf("restored archived manifest for partition tag %s", tag),
			DeploymentRevision: record.DeploymentRevision,
			CorrelationID:      correlationID,
		}); err != nil {
			return partitionRollbackResult{}, err
		}
	}

	return result, nil
}

func loadPartitionRollbackTargets(ctx context.Context, store guardianapi.ReadStore, partitionName, tag string) (map[string]string, error) {
	rollouts, err := historyquery.LoadPartitionRollouts(ctx, store, partitionName, historyquery.DeploymentFilter{})
	if err != nil {
		return nil, err
	}
	targets := make(map[string]string)
	for _, rollout := range rollouts {
		if partitionTagFromRolloutAssets(rollout.Assets) != tag {
			continue
		}
		if _, ok := targets[rollout.Intent]; ok {
			continue
		}
		targets[rollout.Intent] = rollout.DeploymentRevision
	}
	return targets, nil
}

func partitionTagFromRolloutAssets(assets []historyquery.RolloutAsset) string {
	tag := ""
	for _, asset := range assets {
		version := strings.TrimSpace(asset.Version)
		if version == "" {
			continue
		}
		if tag == "" {
			tag = version
			continue
		}
		if tag != version {
			return ""
		}
	}
	return tag
}

func printPartitionRollbackResult(printer *output.Printer, result partitionRollbackResult) {
	if printer.Format == cliformat.FormatJSON {
		printer.PrintJSON(result)
		return
	}
	printer.PrintText("rolled back partition %s to tag %s across %d intents\n", result.Partition, result.Tag, len(result.RestoredIntents))
	if len(result.RestoredIntents) == 0 {
		return
	}
	rows := make([][]string, 0, len(result.RestoredIntents))
	for _, restored := range result.RestoredIntents {
		rows = append(rows, []string{restored.Intent, restored.DeploymentRevision})
	}
	printer.PrintTable([]string{"INTENT", "DEPLOYMENT"}, rows)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func currentPartitionTags(tags []partitionTagRecord) []partitionTagRecord {
	current := make([]partitionTagRecord, 0, len(tags))
	for _, tag := range tags {
		if tag.Current {
			current = append(current, tag)
		}
	}
	return current
}
