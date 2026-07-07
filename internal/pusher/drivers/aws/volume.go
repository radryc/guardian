package awsdriver

import (
	"context"
	"fmt"

	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	orchestratorcommon "github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
)

type VolumeDriver struct{ baseDriver }

func (d *VolumeDriver) Type() string                     { return "Volume" }
func (d *VolumeDriver) Validate(p map[string]any) error  { return nil }

func (d *VolumeDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

func (d *VolumeDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeVolume(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if driverutil.BoolValue(spec.Ephemeral) {
		return inSyncDrift(in.Asset.Name, "ephemeral storage is in sync"), nil
	}

	fsID := loadSavedOutput(in, in.Asset.Name+".efsId")
	if fsID == "" {
		return changedDrift(in.Asset.Name, "EFS filesystem not yet created"), nil
	}

	hash := driverutil.CompositeHash(in)
	fs, ok, err := d.backend.GetFileSystem(ctx, fsID)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || fs.Hash != hash {
		return changedDrift(in.Asset.Name, "EFS filesystem differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "EFS filesystem is in sync"), nil
}

func (d *VolumeDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeVolume(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	if driverutil.BoolValue(spec.Ephemeral) {
		return registry.AssetResult{Outputs: map[string]string{"type": "ephemeral"}}, nil
	}

	hash := driverutil.CompositeHash(in)
	creationToken := awsEFSName(in, in.Asset.Name)
	tags := awsTags(in, hash)

	fsID, err := d.backend.UpsertFileSystem(ctx, FileSystem{
		ID:        creationToken,
		Name:      creationToken,
		Hash:      hash,
		Tags:      tags,
		Encrypted: true,
	})
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("upsert EFS filesystem %s: %w", creationToken, err)
	}

	outputs := map[string]string{"efsId": fsID, "type": "efs"}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *VolumeDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fsID := loadSavedOutput(in, in.Asset.Name+".efsId")
	if fsID == "" {
		return nil
	}
	return d.backend.DeleteFileSystem(ctx, fsID)
}

func loadSavedOutput(in registry.AssetInput, key string) string {
	if in.Store == nil {
		return ""
	}
	state, err := orchestratorcommon.LoadIntentState(context.Background(), in.Store, in.PartitionName, in.IntentName)
	if err != nil {
		return ""
	}
	return state.Outputs[key]
}

func decodeVolume(in registry.AssetInput) (*assetdefs.VolumeSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.VolumeSpec)
	if !ok {
		return nil, fmt.Errorf("expected VolumeSpec, got %T", typed)
	}
	return spec, nil
}
