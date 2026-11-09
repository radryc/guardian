package gcpdriver

import (
	"context"
	"fmt"
	"strings"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/driverutil"
	"github.com/radryc/guardian/internal/pusher/registry"
)

// ObjectStoreDriver manages GCS buckets.
type ObjectStoreDriver struct{ baseDriver }

func (d *ObjectStoreDriver) Type() string { return assetdomain.TypeObjectStore }

func (d *ObjectStoreDriver) Validate(_ map[string]any) error { return nil }

func (d *ObjectStoreDriver) request(in registry.AssetInput, spec *assetdefs.ObjectStoreSpec) StackRequest {
	region := strings.TrimSpace(in.Target.Region)
	if override := strings.TrimSpace(spec.Region); override != "" {
		region = override
	}
	hash := driverutil.CompositeHash(in)
	return StackRequest{
		Partition:   in.PartitionName,
		Intent:      in.IntentName,
		Asset:       in.Asset.Name,
		Project:     in.Target.Account,
		Region:      region,
		Blueprint:   "objectstore",
		DesiredHash: hash,
		Variables: map[string]any{
			"bucket":        bucketName(in.PartitionName, in.IntentName, in.Asset.Name),
			"versioning":    driverutil.BoolValue(spec.Versioning),
			"force_destroy": false,
		},
	}
}

func (d *ObjectStoreDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeObjectStore(in)
	if err != nil {
		return err
	}
	if spec.Endpoint != "" {
		return nil
	}
	return d.backend.CheckEnvironment(ctx, d.request(in, spec))
}

func (d *ObjectStoreDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeObjectStore(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if spec.Endpoint != "" {
		return inSyncDrift(in.Asset.Name, "external object store endpoint is in sync"), nil
	}

	req := d.request(in, spec)
	state, err := d.backend.Get(ctx, req)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !state.Exists {
		return changedDrift(in.Asset.Name, "GCS bucket does not exist"), nil
	}
	if shortHash(req.DesiredHash) != state.Outputs["guardian_hash"] {
		return changedDrift(in.Asset.Name, "GCS bucket differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "GCS bucket is in sync"), nil
}

func (d *ObjectStoreDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeObjectStore(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	if spec.Endpoint != "" {
		return registry.AssetResult{Outputs: map[string]string{
			"endpoint": spec.Endpoint,
			"type":     "external",
		}}, nil
	}

	req := d.request(in, spec)
	outputs, err := d.backend.Apply(ctx, req)
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("apply GCS bucket: %w", err)
	}
	if bucket := outputs["bucket"]; bucket != "" {
		outputs["bucket"] = bucket
	}
	if outputs["type"] == "" {
		outputs["type"] = "gcs"
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *ObjectStoreDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeObjectStore(in)
	if err != nil {
		return err
	}
	if spec.Endpoint != "" {
		return nil
	}
	return d.backend.Destroy(ctx, d.request(in, spec))
}

func decodeObjectStore(in registry.AssetInput) (*assetdefs.ObjectStoreSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.ObjectStoreSpec)
	if !ok {
		return nil, fmt.Errorf("expected ObjectStoreSpec, got %T", typed)
	}
	return spec, nil
}
