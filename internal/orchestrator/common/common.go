package common

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/compiler/dag"
	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	"github.com/rydzu/ainfra/guardian/internal/compiler/resolver"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func ReadJSON(ctx context.Context, store guardianapi.ReadStore, logicalPath string, out any) error {
	data, err := store.ReadFile(ctx, logicalPath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s: %w", logicalPath, err)
	}
	return nil
}

func LoadPartitionState(ctx context.Context, store guardianapi.ReadStore, partition string) (*statedomain.PartitionState, error) {
	state, err := loadPartitionStateFile(ctx, store, partition)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	runtime, err := LoadPartitionRuntime(ctx, store, partition)
	if err != nil {
		return nil, err
	}
	if runtime.PartitionState == nil {
		return nil, os.ErrNotExist
	}
	return statedomain.ClonePartitionState(runtime.PartitionState), nil
}

func loadPartitionStateFile(ctx context.Context, store guardianapi.ReadStore, partition string) (*statedomain.PartitionState, error) {
	var state statedomain.PartitionState
	if err := ReadJSON(ctx, store, paths.PartitionState(partition), &state); err != nil {
		return nil, err
	}
	state.Partition = partition
	return &state, nil
}

func LoadIntentState(ctx context.Context, store guardianapi.ReadStore, partition, intent string) (*statedomain.IntentState, error) {
	var state statedomain.IntentState
	if err := ReadJSON(ctx, store, paths.IntentState(partition, intent), &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func LoadAllIntentStates(ctx context.Context, store guardianapi.ReadStore, partition string) (map[string]*statedomain.IntentState, error) {
	runtime, err := LoadPartitionRuntime(ctx, store, partition)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*statedomain.IntentState, len(runtime.Intents))
	for intentName, state := range runtime.Intents {
		out[intentName] = statedomain.CloneIntentState(state)
	}
	return out, nil
}

func IntentOutputs(states map[string]*statedomain.IntentState) map[string]map[string]string {
	outputs := make(map[string]map[string]string, len(states))
	keys := make([]string, 0, len(states))
	for key := range states {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if states[key] == nil || states[key].Outputs == nil {
			continue
		}
		copied := make(map[string]string, len(states[key].Outputs))
		for outKey, value := range states[key].Outputs {
			copied[outKey] = value
		}
		outputs[key] = copied
	}
	return outputs
}

func DependenciesHealthy(intentState *statedomain.IntentState, states map[string]*statedomain.IntentState) bool {
	for _, join := range intentState.Joins {
		joined := states[join]
		if joined == nil || joined.Status != statedomain.StatusHealthy {
			return false
		}
	}
	return true
}

func IsInFlight(status statedomain.IntentStatus) bool {
	switch status {
	case statedomain.StatusChecking, statedomain.StatusDiffing, statedomain.StatusApplying, statedomain.StatusDestroying:
		return true
	default:
		return false
	}
}

func PreserveStatusDuringRefresh(status statedomain.IntentStatus) bool {
	switch status {
	case statedomain.StatusHealthy,
		statedomain.StatusDrifted,
		statedomain.StatusDriftedLocked:
		return true
	default:
		return false
	}
}

func QueuedStatus(current statedomain.IntentStatus, op taskdomain.Operation) statedomain.IntentStatus {
	if PreserveStatusDuringRefresh(current) {
		return current
	}
	switch op {
	case taskdomain.OpCheck:
		return statedomain.StatusChecking
	case taskdomain.OpDiff:
		return statedomain.StatusDiffing
	case taskdomain.OpApply:
		return statedomain.StatusApplying
	case taskdomain.OpDestroy:
		return statedomain.StatusDestroying
	default:
		return current
	}
}

func HasActiveTask(ctx context.Context, store guardianapi.ReadStore, intentState *statedomain.IntentState) (bool, error) {
	if intentState == nil {
		return false, nil
	}
	if intentState.LastTaskID == "" || intentState.TargetPusher == "" {
		return IsInFlight(intentState.Status), nil
	}
	if exists, err := pathExists(ctx, store, paths.QueueTask(intentState.TargetPusher, intentState.LastTaskID)); err != nil {
		return false, err
	} else if exists {
		return true, nil
	}
	if !IsInFlight(intentState.Status) {
		return false, nil
	}
	if exists, err := pathExists(ctx, store, paths.QueueClaim(intentState.TargetPusher, intentState.LastTaskID)); err != nil {
		return false, err
	} else if exists {
		return true, nil
	}
	if exists, err := pathExists(ctx, store, paths.QueueResult(intentState.TargetPusher, intentState.LastTaskID)); err != nil {
		return false, err
	} else if exists {
		return true, nil
	}
	return false, nil
}

func pathExists(ctx context.Context, store guardianapi.ReadStore, logicalPath string) (bool, error) {
	if _, err := store.Stat(ctx, logicalPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func BuildTask(ctx context.Context, store guardianapi.ReadStore, intentState *statedomain.IntentState, op taskdomain.Operation, outputs map[string]map[string]string) (*taskdomain.Task, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	manifestContent, err := store.ReadFile(ctx, paths.IntentManifest(intentState.Partition, intentState.Intent))
	if err != nil {
		return nil, err
	}
	return BuildTaskFromManifest(intentState, manifestContent, op, outputs)
}

func BuildTaskFromManifest(intentState *statedomain.IntentState, manifestContent []byte, op taskdomain.Operation, outputs map[string]map[string]string) (*taskdomain.Task, error) {
	parsed, err := manifest.ParseIntent(manifestContent)
	if err != nil {
		return nil, err
	}
	graph := dag.New()
	assetByName := map[string]taskdomain.AbstractAsset{}
	for _, asset := range parsed.Spec.Assets {
		resolved, err := resolver.ResolveProperties(asset.Properties, outputs)
		if err != nil {
			return nil, err
		}
		graph.AddNode(asset.Name, asset.DependsOn)
		assetByName[asset.Name] = taskdomain.AbstractAsset{
			Type:       asset.Type,
			Name:       asset.Name,
			DependsOn:  append([]string(nil), asset.DependsOn...),
			Payload:    copyStringMap(asset.Payload),
			Properties: resolved,
		}
	}
	order, err := graph.TopologicalSort()
	if err != nil {
		return nil, err
	}
	assets := make([]taskdomain.AbstractAsset, 0, len(order))
	for _, name := range order {
		assets = append(assets, assetByName[name])
	}
	return &taskdomain.Task{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "Task",
		TaskID:            revisions.NewTaskID(),
		CorrelationID:     revisions.NewCorrelationID(),
		Partition:         intentState.Partition,
		Intent:            intentState.Intent,
		Op:                op,
		TargetPusher:      intentState.TargetPusher,
		Target:            intentState.Target,
		PartitionRevision: intentState.PartitionRevision,
		IntentVersionID:   intentState.IntentVersionID,
		IntentSpecHash:    intentState.IntentSpecHash,
		AssetVersionIDs:   copyStringMap(intentState.AssetVersionIDs),
		AssetVersions:     copyStringMap(intentState.AssetVersions),
		Assets:            assets,
		CreatedAt:         time.Now().UTC(),
	}, nil
}

func BuildTaskFromExisting(current *taskdomain.Task, op taskdomain.Operation) *taskdomain.Task {
	if current == nil {
		return nil
	}
	next := *current
	if next.CorrelationID == "" {
		next.CorrelationID = revisions.NewCorrelationID()
	}
	next.TaskID = revisions.NewTaskID()
	next.Op = op
	next.CreatedAt = time.Now().UTC()
	next.AssetVersionIDs = copyStringMap(current.AssetVersionIDs)
	next.AssetVersions = copyStringMap(current.AssetVersions)
	next.Assets = cloneAssets(current.Assets)
	return &next
}

func cloneAssets(in []taskdomain.AbstractAsset) []taskdomain.AbstractAsset {
	if len(in) == 0 {
		return nil
	}
	out := make([]taskdomain.AbstractAsset, 0, len(in))
	for _, asset := range in {
		out = append(out, taskdomain.AbstractAsset{
			Type:       asset.Type,
			Name:       asset.Name,
			DependsOn:  append([]string(nil), asset.DependsOn...),
			Payload:    copyStringMap(asset.Payload),
			Properties: copyAnyMap(asset.Properties),
		})
	}
	return out
}

func copyAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
