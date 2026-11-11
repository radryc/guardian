package gcpdriver

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/driverutil"
	"github.com/radryc/guardian/internal/pusher/registry"
)

const defaultHTTPPort = 80

// LoadBalancerDriver maps a LoadBalancer asset to a global HTTP(S) load
// balancer in front of Cloud Run services. GCP v1 supports HTTP listeners only.
type LoadBalancerDriver struct{ baseDriver }

func (d *LoadBalancerDriver) Type() string { return assetdomain.TypeLoadBalancer }

func (d *LoadBalancerDriver) Validate(props map[string]any) error {
	return d.ValidateInContext(props, nil)
}

// ValidateInContext validates target references against the intent graph.
func (d *LoadBalancerDriver) ValidateInContext(props map[string]any, assets map[string]taskdomain.AbstractAsset) error {
	if err := assetdefs.Validate(
		assetdomain.Spec{Type: assetdomain.TypeLoadBalancer, Properties: props},
		assetdefs.ValidationContext{AssetTypes: assetTypeMap(assets)},
	); err != nil {
		return err
	}
	typed, _, err := assetdefs.Decode(assetdomain.Spec{Type: assetdomain.TypeLoadBalancer, Properties: props})
	if err != nil {
		return err
	}
	spec, ok := typed.(*assetdefs.LoadBalancerSpec)
	if !ok {
		return fmt.Errorf("expected LoadBalancerSpec, got %T", typed)
	}
	for idx, listener := range spec.Listeners {
		switch strings.ToUpper(strings.TrimSpace(listener.Protocol)) {
		case "", "HTTP":
		default:
			return fmt.Errorf("listeners[%d].protocol %q is not supported on GCP (HTTP only)", idx, listener.Protocol)
		}
	}
	return nil
}

func (d *LoadBalancerDriver) adopted(spec *assetdefs.LoadBalancerSpec) bool {
	return strings.TrimSpace(spec.ExistingName) != ""
}

func (d *LoadBalancerDriver) request(in registry.AssetInput, spec *assetdefs.LoadBalancerSpec) (StackRequest, error) {
	targets := make([]string, 0, len(spec.Targets))
	for _, name := range spec.Targets {
		asset, ok := in.Assets[name]
		if !ok {
			return StackRequest{}, fmt.Errorf("load balancer target %q not found", name)
		}
		if asset.Type != assetdomain.TypeCompute {
			return StackRequest{}, fmt.Errorf("load balancer target %q has type %q, want Compute", name, asset.Type)
		}
		targets = append(targets, cloudRunName(in.PartitionName, in.IntentName, name))
	}
	port := defaultHTTPPort
	if len(spec.Listeners) > 0 && spec.Listeners[0].Port != nil && *spec.Listeners[0].Port > 0 {
		port = *spec.Listeners[0].Port
	}
	return StackRequest{
		Partition:   in.PartitionName,
		Intent:      in.IntentName,
		Asset:       in.Asset.Name,
		Project:     in.Target.Account,
		Region:      in.Target.Region,
		Blueprint:   "loadbalancer",
		DesiredHash: driverutil.CompositeHash(in, strings.Join(targets, ","), strconv.Itoa(port)),
		Variables: map[string]any{
			"name":    lbName(in.PartitionName, in.IntentName, in.Asset.Name),
			"targets": targets,
			"port":    port,
		},
	}, nil
}

func (d *LoadBalancerDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeLoadBalancer(in)
	if err != nil {
		return err
	}
	if d.adopted(spec) {
		return nil
	}
	req, err := d.request(in, spec)
	if err != nil {
		return err
	}
	return d.backend.CheckEnvironment(ctx, req)
}

func (d *LoadBalancerDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeLoadBalancer(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if d.adopted(spec) {
		return inSyncDrift(in.Asset.Name, "adopted load balancer reference is in sync"), nil
	}
	req, err := d.request(in, spec)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	state, err := d.backend.Get(ctx, req)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !state.Exists {
		return changedDrift(in.Asset.Name, "load balancer does not exist"), nil
	}
	if shortHash(req.DesiredHash) != state.Outputs["guardian_hash"] {
		return changedDrift(in.Asset.Name, "load balancer differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "load balancer is in sync"), nil
}

func (d *LoadBalancerDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeLoadBalancer(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	if existing := strings.TrimSpace(spec.ExistingName); existing != "" {
		return registry.AssetResult{Outputs: map[string]string{
			"lb_name": existing,
			"type":    "http-lb",
			"adopted": "true",
		}}, nil
	}
	req, err := d.request(in, spec)
	if err != nil {
		return registry.AssetResult{}, err
	}
	outputs, err := d.backend.Apply(ctx, req)
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("apply load balancer: %w", err)
	}
	if outputs["type"] == "" {
		outputs["type"] = "http-lb"
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *LoadBalancerDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeLoadBalancer(in)
	if err != nil {
		return err
	}
	if d.adopted(spec) {
		return nil
	}
	req, err := d.request(in, spec)
	if err != nil {
		return err
	}
	return d.backend.Destroy(ctx, req)
}

func decodeLoadBalancer(in registry.AssetInput) (*assetdefs.LoadBalancerSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.LoadBalancerSpec)
	if !ok {
		return nil, fmt.Errorf("expected LoadBalancerSpec, got %T", typed)
	}
	return spec, nil
}
