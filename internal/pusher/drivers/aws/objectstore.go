package awsdriver

import (
	"context"
	"fmt"
	"strings"

	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
)

type ObjectStoreDriver struct{ baseDriver }

func (d *ObjectStoreDriver) Type() string                    { return "ObjectStore" }
func (d *ObjectStoreDriver) Validate(p map[string]any) error { return nil }

func objectStoreTarget(spec *assetdefs.ObjectStoreSpec, in registry.AssetInput) (string, bool) {
	if name := strings.TrimSpace(spec.ExistingBucket); name != "" {
		return name, true
	}
	return awsS3BucketName(in), false
}

func (d *ObjectStoreDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
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
		return inSyncDrift(in.Asset.Name, "external S3 endpoint reference is in sync"), nil
	}

	bucketName, adopted := objectStoreTarget(spec, in)
	hash := driverutil.CompositeHash(in)

	bucket, ok, err := d.backend.GetBucket(ctx, bucketName)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok {
		if adopted {
			return changedDrift(in.Asset.Name, "adopted S3 bucket is missing"), nil
		}
		return changedDrift(in.Asset.Name, "S3 bucket differs"), nil
	}
	if bucket.Hash == hash {
		return inSyncDrift(in.Asset.Name, "S3 bucket is in sync"), nil
	}
	if adopted && bucket.Hash == "" {
		if bucket.Versioning != driverutil.BoolValue(spec.Versioning) {
			return changedDrift(in.Asset.Name, "adopted S3 bucket versioning differs"), nil
		}
		return inSyncDrift(in.Asset.Name, "adopted S3 bucket is in sync"), nil
	}
	return changedDrift(in.Asset.Name, "S3 bucket differs"), nil
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

	hash := driverutil.CompositeHash(in)
	tags := awsTags(in, hash)
	bucketName, _ := objectStoreTarget(spec, in)

	region := awsRegion(in)
	if name := strings.TrimSpace(spec.Region); name != "" {
		region = name
	}

	if err := d.backend.UpsertBucket(ctx, BucketSpec{
		Name:       bucketName,
		Hash:       hash,
		Tags:       tags,
		Versioning: driverutil.BoolValue(spec.Versioning),
		Region:     region,
	}); err != nil {
		return registry.AssetResult{}, fmt.Errorf("upsert S3 bucket %s: %w", bucketName, err)
	}

	outputs := map[string]string{
		"bucket": bucketName,
		"region": region,
		"type":   "s3",
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
	bucketName, _ := objectStoreTarget(spec, in)
	return d.backend.DeleteBucket(ctx, bucketName)
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
