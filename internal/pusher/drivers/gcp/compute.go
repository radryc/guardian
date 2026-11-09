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

const defaultContainerPort = 8080

// ComputeDriver maps a Compute asset to a Cloud Run v2 service.
type ComputeDriver struct{ baseDriver }

func (d *ComputeDriver) Type() string { return assetdomain.TypeCompute }

func (d *ComputeDriver) Validate(props map[string]any) error {
	if err := assetdefs.Validate(assetdomain.Spec{Type: assetdomain.TypeCompute, Properties: props}, assetdefs.ValidationContext{}); err != nil {
		return err
	}
	typed, _, err := assetdefs.Decode(assetdomain.Spec{Type: assetdomain.TypeCompute, Properties: props})
	if err != nil {
		return err
	}
	spec, ok := typed.(*assetdefs.ComputeSpec)
	if !ok {
		return fmt.Errorf("expected ComputeSpec, got %T", typed)
	}
	if spec.Privileged != nil && *spec.Privileged {
		return fmt.Errorf("compute.privileged is not supported on GCP Cloud Run")
	}
	if len(spec.VolumeMounts) > 0 {
		return fmt.Errorf("compute.volumeMounts is not supported on GCP Cloud Run")
	}
	if len(spec.ConfigMounts) > 0 {
		return fmt.Errorf("compute.configMounts is not supported on GCP Cloud Run")
	}
	if len(spec.HostBindMounts) > 0 {
		return fmt.Errorf("compute.hostBindMounts is not supported on GCP Cloud Run")
	}
	if strings.TrimSpace(spec.ShmSize) != "" {
		return fmt.Errorf("compute.shmSize is not supported on GCP Cloud Run")
	}
	if strings.TrimSpace(spec.GPUs) != "" {
		return fmt.Errorf("compute.gpus is not supported on GCP Cloud Run")
	}
	if len(spec.Capabilities) > 0 {
		return fmt.Errorf("compute.capabilities is not supported on GCP Cloud Run")
	}
	if spec.HostUsers != nil {
		return fmt.Errorf("compute.hostUsers is not supported on GCP Cloud Run")
	}
	if spec.ImagePullPolicy == "Never" {
		return fmt.Errorf("compute.imagePullPolicy=Never is not supported on GCP Cloud Run")
	}
	if strings.TrimSpace(spec.Cluster) != "" {
		return fmt.Errorf("compute.cluster is not supported on GCP Cloud Run")
	}
	return nil
}

func (d *ComputeDriver) adopted(spec *assetdefs.ComputeSpec) bool {
	return spec.ObserveExisting && strings.TrimSpace(spec.ExistingServiceName) != ""
}

func (d *ComputeDriver) request(in registry.AssetInput, spec *assetdefs.ComputeSpec) StackRequest {
	replicas := 0
	if spec.Replicas != nil {
		replicas = *spec.Replicas
	}
	env := make(map[string]string, len(spec.Env))
	for key, value := range spec.Env {
		if value == nil {
			continue
		}
		env[key] = fmt.Sprint(value)
	}
	port := defaultContainerPort
	if len(spec.Ports) > 0 {
		switch {
		case spec.Ports[0].ContainerPort != nil:
			port = *spec.Ports[0].ContainerPort
		case spec.Ports[0].Port != nil:
			port = *spec.Ports[0].Port
		}
	}
	network, subnet := "", ""
	if len(spec.Networks) > 0 {
		ref := spec.Networks[0]
		network = networkName(in.PartitionName, in.IntentName, ref)
		subnet = subnetName(in.PartitionName, in.IntentName, ref)
	}
	cpu, memory := "", ""
	if spec.Resources != nil {
		cpu = strings.TrimSpace(spec.Resources.Limits.CPU)
		memory = strings.TrimSpace(spec.Resources.Limits.Memory)
	}

	return StackRequest{
		Partition:   in.PartitionName,
		Intent:      in.IntentName,
		Asset:       in.Asset.Name,
		Project:     in.Target.Account,
		Region:      in.Target.Region,
		Blueprint:   "compute",
		DesiredHash: driverutil.CompositeHash(in),
		Variables: map[string]any{
			"name":     cloudRunName(in.PartitionName, in.IntentName, in.Asset.Name),
			"image":    spec.Image,
			"replicas": replicas,
			"env":      env,
			"args":     []string(spec.Args),
			"command":  []string(spec.Command),
			"port":     port,
			"cpu":      cpu,
			"memory":   memory,
			"network":  network,
			"subnet":   subnet,
		},
	}
}

func (d *ComputeDriver) checkNetworkRefs(in registry.AssetInput, spec *assetdefs.ComputeSpec) error {
	for _, name := range spec.Networks {
		asset, ok := in.Assets[name]
		if !ok {
			return fmt.Errorf("compute network reference %q not found", name)
		}
		if asset.Type != assetdomain.TypeNetwork {
			return fmt.Errorf("compute network reference %q has type %q, want Network", name, asset.Type)
		}
	}
	return nil
}

func (d *ComputeDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return err
	}
	if err := d.checkNetworkRefs(in, spec); err != nil {
		return err
	}
	if d.adopted(spec) {
		return nil
	}
	return d.backend.CheckEnvironment(ctx, d.request(in, spec))
}

func (d *ComputeDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if err := d.checkNetworkRefs(in, spec); err != nil {
		return taskdomain.DriftReport{}, err
	}
	if d.adopted(spec) {
		return inSyncDrift(in.Asset.Name, "adopted Cloud Run service reference is in sync"), nil
	}
	req := d.request(in, spec)
	state, err := d.backend.Get(ctx, req)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !state.Exists {
		return changedDrift(in.Asset.Name, "Cloud Run service does not exist"), nil
	}
	if shortHash(req.DesiredHash) != state.Outputs["guardian_hash"] {
		return changedDrift(in.Asset.Name, "Cloud Run service differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "Cloud Run service is in sync"), nil
}

func (d *ComputeDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	if err := d.checkNetworkRefs(in, spec); err != nil {
		return registry.AssetResult{}, err
	}
	if d.adopted(spec) {
		return registry.AssetResult{Outputs: map[string]string{
			"service": spec.ExistingServiceName,
			"type":    "cloudrun",
			"adopted": "true",
		}}, nil
	}
	outputs, err := d.backend.Apply(ctx, d.request(in, spec))
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("apply Cloud Run service: %w", err)
	}
	if outputs["type"] == "" {
		outputs["type"] = "cloudrun"
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *ComputeDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return err
	}
	if d.adopted(spec) {
		return nil
	}
	return d.backend.Destroy(ctx, d.request(in, spec))
}

func decodeCompute(in registry.AssetInput) (*assetdefs.ComputeSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.ComputeSpec)
	if !ok {
		return nil, fmt.Errorf("expected ComputeSpec, got %T", typed)
	}
	return spec, nil
}
