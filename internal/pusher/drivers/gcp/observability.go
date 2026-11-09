package gcpdriver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/driverutil"
	"github.com/radryc/guardian/internal/pusher/registry"
)

const (
	defaultCollectorImage = "ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib:0.108.0"
	defaultOTLPEndpoint   = "http://localhost:4317"
)

// ObservabilityDriver configures OTel collection for GCP. sidecar/env modes are
// configuration-only; gateway/collector/auto mode deploys an OTel Collector on
// Cloud Run.
type ObservabilityDriver struct{ baseDriver }

func (d *ObservabilityDriver) Type() string { return assetdomain.TypeObservability }

func (d *ObservabilityDriver) Validate(props map[string]any) error {
	return d.ValidateInContext(props, nil)
}

// ValidateInContext validates target/config/volume references against the
// intent graph.
func (d *ObservabilityDriver) ValidateInContext(props map[string]any, assets map[string]taskdomain.AbstractAsset) error {
	return assetdefs.Validate(
		assetdomain.Spec{Type: assetdomain.TypeObservability, Properties: props},
		assetdefs.ValidationContext{AssetTypes: assetTypeMap(assets)},
	)
}

func observabilityMode(spec *assetdefs.ObservabilitySpec) string {
	switch strings.ToLower(strings.TrimSpace(spec.Mode)) {
	case "gateway", "auto", "collector":
		return "gateway"
	case "env":
		return "env"
	case "sidecar":
		return "sidecar"
	}
	if strings.TrimSpace(spec.Endpoint) != "" {
		return "env"
	}
	return "sidecar"
}

func (d *ObservabilityDriver) gatewayRequest(ctx context.Context, in registry.AssetInput, spec *assetdefs.ObservabilitySpec) (StackRequest, error) {
	config, err := d.collectorConfig(ctx, in, spec)
	if err != nil {
		return StackRequest{}, err
	}
	network, subnet := "", ""
	if len(spec.Networks) > 0 {
		ref := spec.Networks[0]
		network = networkName(in.PartitionName, in.IntentName, ref)
		subnet = subnetName(in.PartitionName, in.IntentName, ref)
	}
	image := defaultCollectorImage
	if strings.TrimSpace(spec.Image) != "" {
		image = strings.TrimSpace(spec.Image)
	}
	return StackRequest{
		Partition:   in.PartitionName,
		Intent:      in.IntentName,
		Asset:       in.Asset.Name,
		Project:     in.Target.Account,
		Region:      in.Target.Region,
		Blueprint:   "observability",
		DesiredHash: driverutil.CompositeHash(in),
		Variables: map[string]any{
			"name":    otelGatewayName(in.PartitionName, in.IntentName, in.Asset.Name),
			"image":   image,
			"config":  config,
			"env":     map[string]string{},
			"port":    4318,
			"network": network,
			"subnet":  subnet,
		},
	}, nil
}

// collectorConfig resolves a referenced Config asset or generates a minimal
// googlecloud exporter configuration.
func (d *ObservabilityDriver) collectorConfig(ctx context.Context, in registry.AssetInput, spec *assetdefs.ObservabilitySpec) (string, error) {
	ref := strings.TrimSpace(spec.Config)
	if ref == "" {
		return generatedCollectorConfig(), nil
	}
	asset, ok := in.Assets[ref]
	if !ok {
		return "", fmt.Errorf("observability config %q not found", ref)
	}
	refIn := in
	refIn.Asset = asset
	typed, err := driverutil.DecodeAsset(refIn)
	if err != nil {
		return "", err
	}
	configSpec, ok := typed.(*assetdefs.ConfigSpec)
	if !ok {
		return "", fmt.Errorf("observability config %q is not a Config asset", ref)
	}
	files, err := driverutil.ResolveConfigFiles(ctx, refIn, []string{"gcp"}, configSpec)
	if err != nil {
		return "", err
	}
	if len(files.Files) == 0 {
		return "", fmt.Errorf("observability config %q is empty", ref)
	}
	names := make([]string, 0, len(files.Files))
	for name := range files.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	return files.Files[names[0]], nil
}

func generatedCollectorConfig() string {
	return `receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318
processors:
  batch: {}
exporters:
  googlecloud: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [googlecloud]
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [googlecloud]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [googlecloud]
`
}

func (d *ObservabilityDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeObservability(in)
	if err != nil {
		return err
	}
	if observabilityMode(spec) != "gateway" {
		return nil
	}
	req, err := d.gatewayRequest(ctx, in, spec)
	if err != nil {
		return err
	}
	return d.backend.CheckEnvironment(ctx, req)
}

func (d *ObservabilityDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeObservability(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if observabilityMode(spec) != "gateway" {
		return inSyncDrift(in.Asset.Name, "observability sidecar configuration is applied by compute assets"), nil
	}
	req, err := d.gatewayRequest(ctx, in, spec)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	state, err := d.backend.Get(ctx, req)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !state.Exists {
		return changedDrift(in.Asset.Name, "OTel gateway does not exist"), nil
	}
	if shortHash(req.DesiredHash) != state.Outputs["guardian_hash"] {
		return changedDrift(in.Asset.Name, "OTel gateway differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "OTel gateway is in sync"), nil
}

func (d *ObservabilityDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeObservability(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	mode := observabilityMode(spec)
	if mode != "gateway" {
		endpoint := strings.TrimSpace(spec.Endpoint)
		if endpoint == "" {
			endpoint = defaultOTLPEndpoint
		}
		return registry.AssetResult{Outputs: map[string]string{
			"endpoint": endpoint,
			"type":     mode,
		}}, nil
	}
	req, err := d.gatewayRequest(ctx, in, spec)
	if err != nil {
		return registry.AssetResult{}, err
	}
	outputs, err := d.backend.Apply(ctx, req)
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("apply OTel gateway: %w", err)
	}
	if outputs["type"] == "" {
		outputs["type"] = "gateway"
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *ObservabilityDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeObservability(in)
	if err != nil {
		return err
	}
	if observabilityMode(spec) != "gateway" {
		return nil
	}
	req, err := d.gatewayRequest(ctx, in, spec)
	if err != nil {
		return err
	}
	return d.backend.Destroy(ctx, req)
}

func decodeObservability(in registry.AssetInput) (*assetdefs.ObservabilitySpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.ObservabilitySpec)
	if !ok {
		return nil, fmt.Errorf("expected ObservabilitySpec, got %T", typed)
	}
	return spec, nil
}
