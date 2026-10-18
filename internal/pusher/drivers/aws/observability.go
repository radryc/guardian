package awsdriver

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/versioning/digest"
	"gopkg.in/yaml.v3"
)

// defaultOtelCollectorImage is the AWS Distro for OpenTelemetry (ADOT)
// collector used as the sidecar image. It reads its configuration from the
// AOT_CONFIG_CONTENT environment variable.
const defaultOtelCollectorImage = "public.ecr.aws/aws-observability/aws-otel-collector:v0.50.0"

type ObservabilityDriver struct{ baseDriver }

func (d *ObservabilityDriver) Type() string { return assetdomain.TypeObservability }

func (d *ObservabilityDriver) Validate(props map[string]any) error {
	return d.ValidateInContext(props, nil)
}

// ValidateInContext checks the spec against the intent's asset graph so
// targets/config/volume references can be resolved. Validate alone runs with an
// empty graph and would reject every reference.
func (d *ObservabilityDriver) ValidateInContext(props map[string]any, assets map[string]taskdomain.AbstractAsset) error {
	assetTypes := make(map[string]string, len(assets))
	for name, asset := range assets {
		assetTypes[name] = asset.Type
	}
	return assetdefs.Validate(assetdomain.Spec{Type: assetdomain.TypeObservability, Properties: props}, assetdefs.ValidationContext{AssetTypes: assetTypes})
}

func (d *ObservabilityDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

// Diff reports in-sync because sidecar-mode observability has no standalone
// resource: the collector is part of each target compute task definition and
// drift is detected through the target's composite hash.
func (d *ObservabilityDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeObservability(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	switch observabilityMode(spec) {
	case "gateway", "auto", "collector":
		svc, ok, err := d.backend.GetService(ctx, in.Target.Cluster, gatewayServiceName(in))
		if err != nil {
			return taskdomain.DriftReport{}, err
		}
		if !ok {
			return changedDrift(in.Asset.Name, "otel gateway is missing"), nil
		}
		if svc.Hash != driverutil.CompositeHash(in) {
			return changedDrift(in.Asset.Name, "otel gateway differs"), nil
		}
		return inSyncDrift(in.Asset.Name, "otel gateway is in sync"), nil
	default:
		return inSyncDrift(in.Asset.Name, "observability sidecars are applied by target compute assets"), nil
	}
}

func (d *ObservabilityDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeObservability(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	switch observabilityMode(spec) {
	case "gateway", "auto", "collector":
		return d.applyGateway(ctx, in, spec)
	case "env":
		endpoint := strings.TrimSpace(spec.Endpoint)
		return registry.AssetResult{Outputs: map[string]string{
			"endpoint":  endpoint,
			"namespace": observabilityNamespace(in),
			"logGroup":  observabilityLogGroup(in),
		}}, nil
	default:
		if len(spec.Targets) == 0 {
			return registry.AssetResult{}, fmt.Errorf("asset %s: targets requires at least one compute asset", in.Asset.Name)
		}
		return registry.AssetResult{Outputs: map[string]string{
			"endpoint":  "http://localhost:4317",
			"namespace": observabilityNamespace(in),
			"logGroup":  observabilityLogGroup(in),
		}}, nil
	}
}

func (d *ObservabilityDriver) applyGateway(ctx context.Context, in registry.AssetInput, spec *assetdefs.ObservabilitySpec) (registry.AssetResult, error) {
	config, err := otelCollectorConfig(in, spec)
	if err != nil {
		return registry.AssetResult{}, err
	}
	hash := driverutil.CompositeHash(in)
	logGroup := observabilityLogGroup(in)
	_ = d.backend.UpsertLogGroup(ctx, LogGroup{Name: logGroup, Hash: hash, Tags: awsTags(in, hash)})
	taskRole, err := d.backend.EnsureObservabilityTaskRole(ctx)
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("ensure observability task role: %w", err)
	}
	name := gatewayServiceName(in)
	namespace := serviceConnectNamespace()
	essential := true
	svc := ECSService{
		Name:           name,
		Cluster:        in.Target.Cluster,
		Hash:           hash,
		Tags:           awsTags(in, hash),
		DesiredCount:   1,
		LaunchType:     "FARGATE",
		TaskFamily:     awsTaskFamily("otel-gateway", in),
		TaskRole:       taskRole,
		AssignPublicIP: true,
		LogGroup:       logGroup,
		Container: ContainerDef{
			Name:    "otel-collector",
			Image:   defaultOtelCollectorImage,
			Command: []string{"--config=env:AOT_CONFIG_CONTENT"},
			Env: map[string]string{
				"AOT_CONFIG_CONTENT": config,
				"AWS_REGION":         awsRegion(in),
			},
			Ports: []PortDef{
				{Name: "otlp-grpc", ContainerPort: 4317, Protocol: "tcp"},
				{Name: "otlp-http", ContainerPort: 4318, Protocol: "tcp"},
			},
			Essential: &essential,
		},
		ServiceConnect: &ServiceConnectConfig{
			Namespace: namespace,
			Services: []ServiceConnectService{{
				PortMappingName: "otlp-grpc",
				DNSName:         name,
				Port:            4317,
			}},
		},
	}
	if err := d.backend.UpsertService(ctx, svc); err != nil {
		return registry.AssetResult{}, fmt.Errorf("upsert otel gateway: %w", err)
	}
	return registry.AssetResult{Outputs: map[string]string{
		"endpoint":  gatewayEndpoint(in),
		"namespace": observabilityNamespace(in),
		"logGroup":  logGroup,
		"service":   name,
	}}, nil
}

// Destroy removes a gateway service. Sidecar-mode observability has no
// standalone resource.
func (d *ObservabilityDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeObservability(in)
	if err != nil {
		return err
	}
	switch observabilityMode(spec) {
	case "gateway", "auto", "collector":
		return d.backend.DeleteService(ctx, in.Target.Cluster, gatewayServiceName(in))
	default:
		return nil
	}
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

func observabilityMode(spec *assetdefs.ObservabilitySpec) string {
	if spec == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(spec.Mode)) {
	case "":
		return "sidecar"
	default:
		return strings.ToLower(strings.TrimSpace(spec.Mode))
	}
}

func gatewayServiceName(in registry.AssetInput) string {
	return driverutil.ResourceName("otel-gateway", in.Target, in.PartitionName, in.IntentName, "")
}

func serviceConnectNamespace() string {
	if value := strings.TrimSpace(os.Getenv("GUARDIAN_SERVICE_CONNECT_NAMESPACE")); value != "" {
		return value
	}
	return "strata.local"
}

func gatewayEndpoint(in registry.AssetInput) string {
	return fmt.Sprintf("http://%s.%s:4317", gatewayServiceName(in), serviceConnectNamespace())
}

// observabilityAssetNames returns the sorted names of the intent's
// Observability assets that target the given compute asset.
func observabilityAssetNames(in registry.AssetInput) []string {
	names := make([]string, 0, 1)
	for name := range in.Assets {
		_, typed, err := driverutil.DecodeNamedAsset(in, name)
		if err != nil {
			continue
		}
		spec, ok := typed.(*assetdefs.ObservabilitySpec)
		if !ok {
			continue
		}
		if containsString(spec.Targets, in.Asset.Name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// computeHash is the composite hash of a compute asset plus every
// Observability asset that instruments it, so a collector change redeploys the
// ECS service even though the dependency edge points from Observability to
// Compute. The rendered collector config and sidecar image are folded in so
// driver-side config changes also trigger a redeploy.
func computeHash(in registry.AssetInput) string {
	base := driverutil.CompositeHash(in, observabilityAssetNames(in)...)
	sidecars, err := observabilitySidecars(in)
	if err != nil || len(sidecars) == 0 {
		return base
	}
	signatures := make([]string, 0, len(sidecars))
	for _, sidecar := range sidecars {
		signatures = append(signatures, sidecar.Image+"\n"+sidecar.Env["AOT_CONFIG_CONTENT"])
	}
	sort.Strings(signatures)
	return digest.MustNormalizedHash(struct {
		Base     string
		Sidecars []string
	}{Base: base, Sidecars: signatures})
}

// observabilitySidecars builds the collector sidecar containers for a compute
// asset. Only the first targeting Observability asset is used; naming more than
// one is rejected because a task can expose a single OTLP endpoint.
func observabilitySidecars(in registry.AssetInput) ([]ContainerDef, error) {
	names := observabilityAssetNames(in)
	if len(names) == 0 {
		return nil, nil
	}
	if len(names) > 1 {
		return nil, fmt.Errorf("compute asset %q is targeted by multiple observability assets (%s); only one is supported", in.Asset.Name, strings.Join(names, ", "))
	}
	_, typed, err := driverutil.DecodeNamedAsset(in, names[0])
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.ObservabilitySpec)
	if !ok {
		return nil, fmt.Errorf("expected ObservabilitySpec, got %T", typed)
	}
	if mode := observabilityMode(spec); mode != "" && mode != "sidecar" {
		return nil, nil
	}
	config, err := otelCollectorConfig(in, spec)
	if err != nil {
		return nil, err
	}
	essential := false
	name := "otel-collector"
	if in.Asset.Name == name {
		name = "otel-sidecar"
	}
	return []ContainerDef{{
		Name:    name,
		Image:   defaultOtelCollectorImage,
		Command: []string{"--config=env:AOT_CONFIG_CONTENT"},
		Env: map[string]string{
			"AOT_CONFIG_CONTENT": config,
			"AWS_REGION":         awsRegion(in),
		},
		Essential: &essential,
	}}, nil
}

// observabilityNamespace is the CloudWatch EMF namespace for the intent's
// application metrics.
func observabilityNamespace(in registry.AssetInput) string {
	namespace := fmt.Sprintf("guardian/%s/%s", in.PartitionName, in.IntentName)
	return strings.Trim(namespace, "/")
}

// observabilityLogGroup is the CloudWatch Logs group the EMF exporter writes
// to; CloudWatch derives metrics from these events.
func observabilityLogGroup(in registry.AssetInput) string {
	return awsLogGroupName(in) + "/otel"
}

// otelCollectorConfig renders the ADOT collector configuration. A Config asset
// referenced by the Observability spec takes precedence; otherwise a default
// OTLP -> CloudWatch EMF pipeline is generated, with optional OTLP HTTP
// forwarding and X-Ray trace export.
func otelCollectorConfig(in registry.AssetInput, spec *assetdefs.ObservabilitySpec) (string, error) {
	if spec.Config != "" {
		if content, ok := observabilityConfigContent(in, spec.Config); ok {
			return content, nil
		}
	}
	cloudwatchEnabled := spec.CloudWatch == nil || *spec.CloudWatch
	mode := observabilityMode(spec)
	exporters := map[string]any{
		"awsemf": map[string]any{
			"namespace":               observabilityNamespace(in),
			"region":                  awsRegion(in),
			"log_group_name":          observabilityLogGroup(in),
			"log_stream_name":         in.Asset.Name,
			"dimension_rollup_option": "NoDimensionRollup",
			"resource_to_telemetry_conversion": map[string]any{
				"enabled": true,
			},
		},
	}
	metricExporters := []string{"awsemf"}
	if endpoint := strings.TrimSpace(spec.Endpoint); endpoint != "" {
		protocol := strings.ToLower(strings.TrimSpace(spec.Protocol))
		if protocol == "http" || protocol == "http/protobuf" || strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
			exporters["otlphttp"] = map[string]any{"endpoint": endpoint}
			metricExporters = append(metricExporters, "otlphttp")
		} else {
			exporters["otlp"] = map[string]any{"endpoint": endpoint}
			metricExporters = append(metricExporters, "otlp")
		}
	}
	pipelines := map[string]any{
		"metrics": map[string]any{
			"receivers":  []string{"otlp"},
			"processors": []string{"batch"},
			"exporters":  metricExporters,
		},
	}
	processors := map[string]any{"batch": map[string]any{}}
	if cloudwatchEnabled {
		exporters["awscloudwatchlogs"] = map[string]any{
			"region":          awsRegion(in),
			"log_group_name":  observabilityLogGroup(in),
			"log_stream_name": in.Asset.Name,
		}
		pipelines["logs"] = map[string]any{
			"receivers":  []string{"otlp"},
			"processors": []string{"batch"},
			"exporters":  []string{"awscloudwatchlogs"},
		}
	}
	if cloudwatchEnabled && (mode == "gateway" || mode == "auto" || mode == "collector") || containsStringFold(spec.Exporters, "xray", "awsxray") {
		exporters["awsxray"] = map[string]any{"region": awsRegion(in)}
		// The X-Ray exporter derives the service segment name from peer.service
		// before falling back to the resource service.name, and telemetrygen (as
		// well as some SDKs) sets peer.service to the span role instead of the
		// service, which makes traces show up under the wrong service. Drop it so
		// segments are named from service.name.
		processors["attributes/xray"] = map[string]any{
			"actions": []any{
				map[string]any{"key": "peer.service", "action": "delete"},
			},
		}
		pipelines["traces"] = map[string]any{
			"receivers":  []string{"otlp"},
			"processors": []string{"attributes/xray", "batch"},
			"exporters":  []string{"awsxray"},
		}
	}
	config := map[string]any{
		"receivers": map[string]any{
			"otlp": map[string]any{
				"protocols": map[string]any{
					"grpc": map[string]any{"endpoint": "0.0.0.0:4317"},
					"http": map[string]any{"endpoint": "0.0.0.0:4318"},
				},
			},
		},
		"processors": processors,
		"exporters":  exporters,
		"service":    map[string]any{"pipelines": pipelines},
	}
	content, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("render collector config: %w", err)
	}
	return string(content), nil
}

// observabilityConfigContent resolves a Config asset reference to a single
// collector config file. Multi-file configs are unsupported for the sidecar
// because AOT_CONFIG_CONTENT takes exactly one document.
func observabilityConfigContent(in registry.AssetInput, ref string) (string, bool) {
	_, typed, err := driverutil.DecodeNamedAsset(in, ref)
	if err != nil {
		return "", false
	}
	spec, ok := typed.(*assetdefs.ConfigSpec)
	if !ok {
		return "", false
	}
	_, content, ok := driverutil.SingleConfigFile(spec)
	return content, ok
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsStringFold(values []string, targets ...string) bool {
	for _, value := range values {
		for _, target := range targets {
			if strings.EqualFold(strings.TrimSpace(value), target) {
				return true
			}
		}
	}
	return false
}
