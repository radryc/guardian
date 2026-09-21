package awsdriver

import (
	"context"
	"fmt"
	"strings"

	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	orchestratorcommon "github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
)

type VolumeDriver struct{ baseDriver }

func (d *VolumeDriver) Type() string                    { return "Volume" }
func (d *VolumeDriver) Validate(p map[string]any) error { return nil }

func (d *VolumeDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

func volumeAdopted(spec *assetdefs.VolumeSpec) bool {
	return strings.TrimSpace(spec.ExistingID) != ""
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

	adopted := volumeAdopted(spec)
	fsID := loadSavedOutput(ctx, in, in.Asset.Name+".efsId")
	if adopted {
		fsID = strings.TrimSpace(spec.ExistingID)
	}
	if fsID == "" {
		if adopted {
			return changedDrift(in.Asset.Name, "adopted EFS filesystem is missing"), nil
		}
		return changedDrift(in.Asset.Name, "EFS filesystem not yet created"), nil
	}

	hash := driverutil.CompositeHash(in)
	fs, ok, err := d.backend.GetFileSystem(ctx, fsID)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok {
		if adopted {
			return changedDrift(in.Asset.Name, "adopted EFS filesystem is missing"), nil
		}
		return changedDrift(in.Asset.Name, "EFS filesystem differs"), nil
	}
	if !adopted && fs.Hash != hash {
		return changedDrift(in.Asset.Name, "EFS filesystem differs"), nil
	}
	if adopted {
		return inSyncDrift(in.Asset.Name, "adopted EFS filesystem is in sync"), nil
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

	if volumeAdopted(spec) {
		fsID := strings.TrimSpace(spec.ExistingID)
		if _, ok, err := d.backend.GetFileSystem(ctx, fsID); err != nil {
			return registry.AssetResult{}, err
		} else if !ok {
			return registry.AssetResult{}, fmt.Errorf("adopted EFS filesystem %s not found", fsID)
		}
		return registry.AssetResult{Outputs: map[string]string{"efsId": fsID, "type": "efs"}}, nil
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
	spec, err := decodeVolume(in)
	if err != nil {
		return err
	}
	if volumeAdopted(spec) {
		return nil
	}
	fsID := loadSavedOutput(ctx, in, in.Asset.Name+".efsId")
	if fsID == "" {
		return nil
	}
	return d.backend.DeleteFileSystem(ctx, fsID)
}

func loadSavedOutput(ctx context.Context, in registry.AssetInput, key string) string {
	if in.Store == nil {
		return ""
	}
	state, err := orchestratorcommon.LoadIntentState(ctx, in.Store, in.PartitionName, in.IntentName)
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
