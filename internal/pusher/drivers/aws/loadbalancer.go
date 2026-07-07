package awsdriver

import (
	"context"
	"fmt"

	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
)

type LoadBalancerDriver struct{ baseDriver }

func (d *LoadBalancerDriver) Type() string                { return "LoadBalancer" }
func (d *LoadBalancerDriver) Validate(p map[string]any) error { return nil }

func (d *LoadBalancerDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (d *LoadBalancerDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	hash := driverutil.CompositeHash(in)
	lbName := awsALBName(in)

	lb, ok, err := d.backend.GetLoadBalancer(ctx, lbName)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || lb.Hash != hash {
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

	hash := driverutil.CompositeHash(in)
	tags := awsTags(in, hash)

	lbName := awsALBName(in)
	scheme := "internet-facing"
	if spec.ServiceType == "ClusterIP" {
		scheme = "internal"
	}

	lbType := "application"
	isTCP := false
	for _, l := range spec.Listeners {
		if l.Protocol == "tcp" {
			isTCP = true
			break
		}
	}
	if isTCP {
		lbType = "network"
	}

	lbARN, err := d.backend.UpsertLoadBalancer(ctx, LoadBalancer{
		Name:   lbName,
		Hash:   hash,
		Tags:   tags,
		Type:   lbType,
		Scheme: scheme,
	})
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("create load balancer: %w", err)
	}

	outputs := map[string]string{"lb_arn": lbARN, "lb_name": lbName}

	for i, listener := range spec.Listeners {
		port := 80
		if listener.Port != nil {
			port = *listener.Port
		}

		proto := "HTTP"
		if listener.Protocol == "grpc" {
			proto = "HTTP"
		} else if listener.Protocol == "tcp" {
			proto = "TCP"
		}

		tgName := awsTargetGroupName(in, fmt.Sprintf("-%d", i))
		tgARN, tgErr := d.backend.UpsertTargetGroup(ctx, TargetGroup{
			Name:       tgName,
			Hash:       hash,
			Tags:       tags,
			Port:       port,
			Protocol:   proto,
			TargetType: "ip",
		})
		if tgErr != nil {
			return registry.AssetResult{}, fmt.Errorf("create target group for listener %d: %w", i, tgErr)
		}
		outputs[fmt.Sprintf("tg_%d_arn", i)] = tgARN

		_, lErr := d.backend.UpsertListener(ctx, Listener{
			LoadBalancerARN:  lbARN,
			Port:             port,
			Protocol:         proto,
			Hash:             hash,
			Tags:             tags,
			DefaultActionARN: tgARN,
		})
		if lErr != nil {
			return registry.AssetResult{}, fmt.Errorf("create listener for port %d: %w", port, lErr)
		}
	}

	lb, _, _ := d.backend.GetLoadBalancer(ctx, lbName)
	if lb.DNSName != "" {
		outputs["dns_name"] = lb.DNSName
	}

	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *LoadBalancerDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lb, ok, err := d.backend.GetLoadBalancer(ctx, awsALBName(in))
	if err != nil || !ok {
		return err
	}
	return d.backend.DeleteLoadBalancer(ctx, lb.ARN)
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
