package kubernetesdriver

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

// NetworkDriver maps a Network asset to a Kubernetes NetworkPolicy. Compute
// assets that reference the network are labelled with guardian.network.<name>
// and the policy selects exactly those pods.
type NetworkDriver struct{ baseDriver }

func (d *NetworkDriver) Type() string { return assetdomain.TypeNetwork }

func (d *NetworkDriver) Validate(props map[string]any) error {
	return assetdefs.Validate(assetdomain.Spec{Type: assetdomain.TypeNetwork, Properties: props}, assetdefs.ValidationContext{})
}

func (d *NetworkDriver) Check(ctx context.Context, in registry.AssetInput) error { return ctx.Err() }

func (d *NetworkDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "kubernetes network policy is ready"},
		&taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "kubernetes network policy is ready"}, nil
}

func (d *NetworkDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeNetwork(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	hash := networkPolicyHash(in, spec)
	policy, ok, err := d.backend.GetNetworkPolicy(namespace(in), networkPolicyName(in))
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || policy.Hash != hash {
		return changedDrift(in.Asset.Name, "kubernetes network policy differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "kubernetes network policy is in sync"), nil
}

func (d *NetworkDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeNetwork(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	hash := networkPolicyHash(in, spec)
	policy := NetworkPolicy{
		Namespace:    namespace(in),
		Name:         networkPolicyName(in),
		Hash:         hash,
		Labels:       driverutil.Labels("kubernetes", in, hash),
		PodSelector:  map[string]string{networkPodLabel(in.Asset.Name): "true"},
		AllowCluster: strings.EqualFold(strings.TrimSpace(spec.Scope), "cluster"),
		Internal:     spec.Internal,
	}
	if err := d.backend.UpsertNetworkPolicy(policy); err != nil {
		return registry.AssetResult{}, err
	}
	return registry.AssetResult{Outputs: map[string]string{
		"name":     policy.Name,
		"selector": networkPodLabel(in.Asset.Name) + "=true",
	}}, nil
}

func (d *NetworkDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.backend.DeleteNetworkPolicy(namespace(in), networkPolicyName(in))
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

func networkPolicyName(in registry.AssetInput) string {
	return workloadName(in, "net")
}

// networkPodLabel is the pod label that joins a workload to a network policy.
func networkPodLabel(assetName string) string {
	return "guardian.network." + assetName
}

func networkPolicyHash(in registry.AssetInput, spec *assetdefs.NetworkSpec) string {
	return driverutil.CompositeHash(in, spec.Driver, spec.Scope, fmt.Sprintf("%t", spec.Internal))
}
