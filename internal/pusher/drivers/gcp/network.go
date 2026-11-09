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

const defaultVPCCIDR = "10.0.0.0/16"

// NetworkDriver maps a Network asset to a GCP VPC with one regional subnet.
type NetworkDriver struct{ baseDriver }

func (d *NetworkDriver) Type() string { return assetdomain.TypeNetwork }

func (d *NetworkDriver) Validate(props map[string]any) error {
	return assetdefs.Validate(assetdomain.Spec{Type: assetdomain.TypeNetwork, Properties: props}, assetdefs.ValidationContext{})
}

func (d *NetworkDriver) request(in registry.AssetInput, spec *assetdefs.NetworkSpec) StackRequest {
	cidr := strings.TrimSpace(spec.CIDR)
	if cidr == "" {
		cidr = defaultVPCCIDR
	}
	hash := driverutil.CompositeHash(in, cidr, fmt.Sprintf("%t", spec.Internal), spec.Driver, spec.Scope)
	return StackRequest{
		Partition:   in.PartitionName,
		Intent:      in.IntentName,
		Asset:       in.Asset.Name,
		Project:     in.Target.Account,
		Region:      in.Target.Region,
		Blueprint:   "network",
		DesiredHash: hash,
		Variables: map[string]any{
			"name":     networkName(in.PartitionName, in.IntentName, in.Asset.Name),
			"subnet":   subnetName(in.PartitionName, in.IntentName, in.Asset.Name),
			"cidr":     cidr,
			"internal": spec.Internal,
		},
	}
}

func (d *NetworkDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeNetwork(in)
	if err != nil {
		return err
	}
	return d.backend.CheckEnvironment(ctx, d.request(in, spec))
}

func (d *NetworkDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeNetwork(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	req := d.request(in, spec)
	state, err := d.backend.Get(ctx, req)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !state.Exists {
		return changedDrift(in.Asset.Name, "VPC does not exist"), nil
	}
	if shortHash(req.DesiredHash) != state.Outputs["guardian_hash"] {
		return changedDrift(in.Asset.Name, "VPC differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "VPC is in sync"), nil
}

func (d *NetworkDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeNetwork(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	outputs, err := d.backend.Apply(ctx, d.request(in, spec))
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("apply VPC: %w", err)
	}
	if outputs["type"] == "" {
		outputs["type"] = "vpc"
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *NetworkDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeNetwork(in)
	if err != nil {
		return err
	}
	return d.backend.Destroy(ctx, d.request(in, spec))
}

func decodeNetwork(in registry.AssetInput) (*assetdefs.NetworkSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.NetworkSpec)
	if !ok {
		return nil, fmt.Errorf("expected NetworkSpec, got %T", typed)
	}
	return spec, nil
}
