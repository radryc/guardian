package awsdriver

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

// NetworkDriver maps a Network asset to an EC2 VPC. Scope and internal flags
// are recorded in the VPC hash/tags so drift is detected when they change.
type NetworkDriver struct{ baseDriver }

func (d *NetworkDriver) Type() string { return assetdomain.TypeNetwork }

func (d *NetworkDriver) Validate(props map[string]any) error {
	return assetdefs.Validate(assetdomain.Spec{Type: assetdomain.TypeNetwork, Properties: props}, assetdefs.ValidationContext{})
}

func (d *NetworkDriver) Check(ctx context.Context, in registry.AssetInput) error { return ctx.Err() }

func (d *NetworkDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeNetwork(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	vpcID := loadSavedOutput(ctx, in, in.Asset.Name+".vpcId")
	if vpcID == "" {
		return changedDrift(in.Asset.Name, "VPC not yet created"), nil
	}
	vpc, ok, err := d.backend.GetVPC(ctx, vpcID)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || vpc.Hash != networkVPCHash(in, spec) {
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
	hash := networkVPCHash(in, spec)
	cidr := strings.TrimSpace(spec.CIDR)
	if cidr == "" {
		cidr = defaultVPCCIDR
	}
	name := awsVPCName(in)
	vpcID, err := d.backend.UpsertVPC(ctx, VPC{
		Name:      name,
		Hash:      hash,
		Tags:      awsTags(in, hash),
		CIDRBlock: cidr,
		Internal:  spec.Internal,
	})
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("upsert VPC %s: %w", name, err)
	}
	return registry.AssetResult{Outputs: map[string]string{"vpcId": vpcID, "cidr": cidr, "name": name}}, nil
}

func (d *NetworkDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	vpcID := loadSavedOutput(ctx, in, in.Asset.Name+".vpcId")
	if vpcID == "" {
		return nil
	}
	return d.backend.DeleteVPC(ctx, vpcID)
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

func networkVPCHash(in registry.AssetInput, spec *assetdefs.NetworkSpec) string {
	return driverutil.CompositeHash(in, spec.CIDR, spec.Scope, spec.Driver, fmt.Sprintf("%t", spec.Internal))
}
