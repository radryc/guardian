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
	"github.com/radryc/guardian/internal/versioning/digest"
)

// SecretDriver maps a Secret asset to Secret Manager.
type SecretDriver struct{ baseDriver }

func (d *SecretDriver) Type() string { return assetdomain.TypeSecret }

func (d *SecretDriver) Validate(props map[string]any) error {
	return assetdefs.Validate(assetdomain.Spec{Type: assetdomain.TypeSecret, Properties: props}, assetdefs.ValidationContext{})
}

func (d *SecretDriver) resolveValue(ctx context.Context, spec *assetdefs.SecretSpec) (string, error) {
	if strings.TrimSpace(spec.SecretRef) == "" {
		return spec.Value, nil
	}
	resolver := d.resolver
	if resolver == nil {
		resolver = secretsNoopResolver{}
	}
	return resolver.Resolve(ctx, spec.SecretRef)
}

func (d *SecretDriver) request(in registry.AssetInput, spec *assetdefs.SecretSpec, value string) StackRequest {
	hash := driverutil.CompositeHash(in, digest.ContentHash([]byte(value)), spec.SecretRef)
	return StackRequest{
		Partition:   in.PartitionName,
		Intent:      in.IntentName,
		Asset:       in.Asset.Name,
		Project:     in.Target.Account,
		Region:      in.Target.Region,
		Blueprint:   "secret",
		DesiredHash: hash,
		Variables: map[string]any{
			"secret_id":      secretName(in.PartitionName, in.IntentName, in.Asset.Name),
			"value":          value,
			"create_version": true,
		},
	}
}

func (d *SecretDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeSecret(in)
	if err != nil {
		return err
	}
	if strings.TrimSpace(spec.ExistingSecret) != "" {
		return nil
	}
	value, err := d.resolveValue(ctx, spec)
	if err != nil {
		return err
	}
	return d.backend.CheckEnvironment(ctx, d.request(in, spec, value))
}

func (d *SecretDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeSecret(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if strings.TrimSpace(spec.ExistingSecret) != "" {
		// Existence verification for adopted secrets arrives with the API
		// backend; the reference itself is treated as in sync.
		return inSyncDrift(in.Asset.Name, "adopted Secret Manager secret reference is in sync"), nil
	}
	value, err := d.resolveValue(ctx, spec)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	req := d.request(in, spec, value)
	state, err := d.backend.Get(ctx, req)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !state.Exists {
		return changedDrift(in.Asset.Name, "Secret Manager secret does not exist"), nil
	}
	if shortHash(req.DesiredHash) != state.Outputs["guardian_hash"] {
		return changedDrift(in.Asset.Name, "Secret Manager secret differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "Secret Manager secret is in sync"), nil
}

func (d *SecretDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeSecret(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	if existing := strings.TrimSpace(spec.ExistingSecret); existing != "" {
		return registry.AssetResult{Outputs: map[string]string{
			"secret_id": existing,
			"secret":    existing,
			"type":      "secretmanager",
			"adopted":   "true",
		}}, nil
	}
	value, err := d.resolveValue(ctx, spec)
	if err != nil {
		return registry.AssetResult{}, err
	}
	outputs, err := d.backend.Apply(ctx, d.request(in, spec, value))
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("apply Secret Manager secret: %w", err)
	}
	if outputs["type"] == "" {
		outputs["type"] = "secretmanager"
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *SecretDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeSecret(in)
	if err != nil {
		return err
	}
	if strings.TrimSpace(spec.ExistingSecret) != "" {
		return nil
	}
	value := spec.Value
	if strings.TrimSpace(spec.SecretRef) != "" {
		resolved, err := d.resolveValue(ctx, spec)
		if err != nil {
			return err
		}
		value = resolved
	}
	return d.backend.Destroy(ctx, d.request(in, spec, value))
}

func decodeSecret(in registry.AssetInput) (*assetdefs.SecretSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.SecretSpec)
	if !ok {
		return nil, fmt.Errorf("expected SecretSpec, got %T", typed)
	}
	return spec, nil
}

// secretsNoopResolver mirrors secrets.NoopResolver without adding a package
// dependency for the zero-value case.
type secretsNoopResolver struct{}

func (secretsNoopResolver) Resolve(_ context.Context, ref string) (string, error) { return ref, nil }
