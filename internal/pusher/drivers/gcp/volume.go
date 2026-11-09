package gcpdriver

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/driverutil"
	"github.com/radryc/guardian/internal/pusher/registry"
)

// VolumeDriver maps Volume assets to Filestore (or ephemeral/adopted volumes).
type VolumeDriver struct{ baseDriver }

func (d *VolumeDriver) Type() string { return assetdomain.TypeVolume }

func (d *VolumeDriver) Validate(props map[string]any) error {
	return assetdefs.Validate(assetdomain.Spec{Type: assetdomain.TypeVolume, Properties: props}, assetdefs.ValidationContext{})
}

func (d *VolumeDriver) ephemeral(spec *assetdefs.VolumeSpec) bool {
	return driverutil.BoolValue(spec.Ephemeral)
}

func (d *VolumeDriver) adopted(spec *assetdefs.VolumeSpec) bool {
	return strings.TrimSpace(spec.ExistingID) != ""
}

func filestoreTier(class string) string {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "premium", "ssd", "basic-ssd", "basic_ssd":
		return "BASIC_SSD"
	case "regional", "ha", "enterprise":
		return "REGIONAL"
	default:
		return "BASIC_HDD"
	}
}

// capacityGB converts a Kubernetes-style size (10Gi, 1Ti, 500Mi) into whole
// gigabytes, defaulting to 100 when unset.
func capacityGB(size string) int {
	size = strings.TrimSpace(size)
	if size == "" {
		return 100
	}
	multiplier := 1.0
	upper := strings.ToUpper(size)
	switch {
	case strings.HasSuffix(upper, "TI"):
		multiplier = 1024
		size = strings.TrimSpace(size[:len(size)-2])
	case strings.HasSuffix(upper, "GI"):
		size = strings.TrimSpace(size[:len(size)-2])
	case strings.HasSuffix(upper, "MI"):
		multiplier = 1.0 / 1024
		size = strings.TrimSpace(size[:len(size)-2])
	case strings.HasSuffix(upper, "T"):
		multiplier = 1024
		size = strings.TrimSpace(size[:len(size)-1])
	case strings.HasSuffix(upper, "G"):
		size = strings.TrimSpace(size[:len(size)-1])
	}
	value, err := strconv.ParseFloat(size, 64)
	if err != nil || value <= 0 {
		return 100
	}
	gb := int(math.Ceil(value * multiplier))
	if gb < 1 {
		return 1
	}
	return gb
}

func filestoreLocation(region, tier string) string {
	region = strings.TrimSpace(region)
	if tier == "REGIONAL" {
		return region
	}
	if region == "" {
		return ""
	}
	return region + "-a"
}

func (d *VolumeDriver) request(in registry.AssetInput, spec *assetdefs.VolumeSpec) StackRequest {
	tier := filestoreTier(spec.Class)
	return StackRequest{
		Partition:   in.PartitionName,
		Intent:      in.IntentName,
		Asset:       in.Asset.Name,
		Project:     in.Target.Account,
		Region:      in.Target.Region,
		Blueprint:   "volume",
		DesiredHash: driverutil.CompositeHash(in, spec.Size, tier),
		Variables: map[string]any{
			"name":        filestoreName(in.PartitionName, in.IntentName, in.Asset.Name),
			"capacity_gb": capacityGB(spec.Size),
			"tier":        tier,
			"location":    filestoreLocation(in.Target.Region, tier),
			"share":       databaseIdentifier(in.Asset.Name),
			"network":     "default",
		},
	}
}

func (d *VolumeDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeVolume(in)
	if err != nil {
		return err
	}
	if d.ephemeral(spec) || d.adopted(spec) {
		return nil
	}
	return d.backend.CheckEnvironment(ctx, d.request(in, spec))
}

func (d *VolumeDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeVolume(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if d.ephemeral(spec) {
		return inSyncDrift(in.Asset.Name, "ephemeral volume needs no GCP resource"), nil
	}
	if d.adopted(spec) {
		return inSyncDrift(in.Asset.Name, "adopted Filestore volume reference is in sync"), nil
	}
	req := d.request(in, spec)
	state, err := d.backend.Get(ctx, req)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !state.Exists {
		return changedDrift(in.Asset.Name, "Filestore instance does not exist"), nil
	}
	if shortHash(req.DesiredHash) != state.Outputs["guardian_hash"] {
		return changedDrift(in.Asset.Name, "Filestore instance differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "Filestore instance is in sync"), nil
}

func (d *VolumeDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeVolume(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	if d.ephemeral(spec) {
		return registry.AssetResult{Outputs: map[string]string{"type": "ephemeral"}}, nil
	}
	if existing := strings.TrimSpace(spec.ExistingID); existing != "" {
		return registry.AssetResult{Outputs: map[string]string{
			"volume_id": existing,
			"type":      "filestore",
			"adopted":   "true",
		}}, nil
	}
	outputs, err := d.backend.Apply(ctx, d.request(in, spec))
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("apply Filestore instance: %w", err)
	}
	if outputs["type"] == "" {
		outputs["type"] = "filestore"
	}
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
	if d.ephemeral(spec) || d.adopted(spec) {
		return nil
	}
	return d.backend.Destroy(ctx, d.request(in, spec))
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
