package driverutil

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	"github.com/radryc/guardian/internal/pusher/registry"
)

func AutoInstrumentDisabled(labels map[string]string) bool {
	for key, value := range labels {
		if strings.EqualFold(strings.TrimSpace(key), "guardian.observability/auto-instrument") {
			return strings.EqualFold(strings.TrimSpace(value), "false")
		}
	}
	return false
}

// ApplyOtelEnv injects the canonical identity and topology attributes into the
// workload environment. Explicit OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES
// values are preserved; generated values only fill gaps.

func ApplyOtelEnv(env map[string]string, in registry.AssetInput, provider string) map[string]string {
	return ApplyOtelEnvWithServiceName(env, in, provider, "")
}

// ApplyOtelEnvWithServiceName is ApplyOtelEnv with an optional partition-level
// service-name override. Explicit workload values still win.

func ApplyOtelEnvWithServiceName(env map[string]string, in registry.AssetInput, provider, serviceNameOverride string) map[string]string {
	if env == nil {
		env = map[string]string{}
	}
	identity := CanonicalServiceIdentity(in.PartitionName, in.IntentName, in.Asset.Name)
	attrs := parseOtelResourceAttrs(env["OTEL_RESOURCE_ATTRIBUTES"])

	// Canonical identity is authoritative. An explicit service name is kept as
	// service.alias so dashboards and external consumers can still find the
	// legacy name, but it no longer replaces partition.intent.asset.
	explicitServiceName := strings.TrimSpace(env["OTEL_SERVICE_NAME"])
	existingServiceName := strings.TrimSpace(attrs["service.name"])
	serviceName := identity.Name
	if serviceName == "" {
		serviceName = explicitServiceName
	}
	if serviceName == "" {
		serviceName = existingServiceName
	}
	if serviceName == "" {
		serviceName = strings.TrimSpace(serviceNameOverride)
	}
	if serviceName != "" {
		env["OTEL_SERVICE_NAME"] = serviceName
		attrs["service.name"] = serviceName
	}

	setGenerated := func(key, value string) {
		if _, exists := attrs[key]; exists || value == "" {
			return
		}
		attrs[key] = value
	}
	if identity.Name != "" {
		if explicitServiceName != "" && explicitServiceName != identity.Name {
			setGenerated("service.alias", explicitServiceName)
		}
		if existingServiceName != "" && existingServiceName != identity.Name {
			setGenerated("service.alias", existingServiceName)
		}
		if override := strings.TrimSpace(serviceNameOverride); override != "" && override != identity.Name {
			setGenerated("service.alias", override)
		}
	}
	setGenerated("service.namespace", in.PartitionName)
	setGenerated("guardian.partition", in.PartitionName)
	setGenerated("guardian.intent", in.IntentName)
	setGenerated("guardian.asset", in.Asset.Name)
	setGenerated("guardian.asset.type", string(in.Asset.Type))
	setGenerated("guardian.identity.kind", "managed")
	setGenerated("guardian.pusher", in.PusherName)
	setGenerated("guardian.provider", provider)
	if strings.EqualFold(provider, "kubernetes") || strings.EqualFold(provider, "docker") {
		setGenerated("guardian.cluster", strings.TrimSpace(in.Target.Cluster))
	}
	aliasKeys := make([]string, 0, len(env))
	for key := range env {
		if strings.EqualFold(key, "OTEL_SERVICE_NAME") {
			continue
		}
		if strings.HasSuffix(strings.ToUpper(key), "_OTEL_SERVICE_NAME") {
			aliasKeys = append(aliasKeys, key)
		}
	}
	sort.Strings(aliasKeys)
	for _, key := range aliasKeys {
		if value := strings.TrimSpace(env[key]); value != "" {
			setGenerated("service.alias", value)
			break
		}
	}
	if strings.EqualFold(provider, "kubernetes") {
		setGenerated("k8s.cluster.name", strings.TrimSpace(in.Target.Cluster))
	}
	if strings.EqualFold(provider, "aws") {
		setGenerated("guardian.account", strings.TrimSpace(in.Target.Account))
		setGenerated("guardian.region", strings.TrimSpace(in.Target.Region))
	}
	if identity.FullName != "" {
		setGenerated("guardian.service.full_name", identity.FullName)
		truncated := "false"
		if identity.Truncated && serviceName == identity.Name {
			truncated = "true"
		}
		setGenerated("guardian.service.truncated", truncated)
	}

	env["OTEL_RESOURCE_ATTRIBUTES"] = formatOtelResourceAttrs(attrs)
	return env
}

// ApplyLBOtelEnv gives load-balancer workloads the standard OTEL_* variables
// used by the otelc auto-export path. Without these, otelc falls back to
// https://localhost:4318 and logs export failures inside the pod.
// The load balancer uses the same canonical identity contract as every other
// managed Compute asset; LB_OTEL_SERVICE_NAME is kept as the compatibility
// knob read by the lb telemetry package.
// Explicitly configured endpoint values win; identity does not.

func ApplyLBOtelEnv(env map[string]string, in registry.AssetInput) map[string]string {
	if env == nil {
		env = map[string]string{}
	}
	set := func(key, value string) {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(env[key]) != "" {
			return
		}
		env[key] = value
	}
	identity := CanonicalServiceIdentity(in.PartitionName, in.IntentName, in.Asset.Name)
	explicitOTELServiceName := strings.TrimSpace(env["OTEL_SERVICE_NAME"])
	explicitLBServiceName := strings.TrimSpace(env["LB_OTEL_SERVICE_NAME"])
	attrs := parseOtelResourceAttrs(env["OTEL_RESOURCE_ATTRIBUTES"])
	if identity.Name != "" {
		env["OTEL_SERVICE_NAME"] = identity.Name
		attrs["service.name"] = identity.Name
		env["LB_OTEL_SERVICE_NAME"] = identity.Name
	}
	setGenerated := func(key, value string) {
		if _, exists := attrs[key]; exists || strings.TrimSpace(value) == "" {
			return
		}
		attrs[key] = value
	}
	setGenerated("service.namespace", in.PartitionName)
	setGenerated("guardian.partition", in.PartitionName)
	setGenerated("guardian.intent", in.IntentName)
	setGenerated("guardian.asset", in.Asset.Name)
	setGenerated("guardian.asset.type", string(in.Asset.Type))
	setGenerated("guardian.identity.kind", "managed")
	setGenerated("guardian.pusher", in.PusherName)
	setGenerated("guardian.provider", "kubernetes")
	setGenerated("guardian.cluster", strings.TrimSpace(in.Target.Cluster))
	setGenerated("k8s.cluster.name", strings.TrimSpace(in.Target.Cluster))
	if explicitOTELServiceName != "" && explicitOTELServiceName != identity.Name {
		setGenerated("service.alias", explicitOTELServiceName)
	}
	if explicitLBServiceName != "" && explicitLBServiceName != identity.Name {
		setGenerated("service.alias", explicitLBServiceName)
	}
	env["OTEL_RESOURCE_ATTRIBUTES"] = formatOtelResourceAttrs(attrs)
	endpoint := strings.TrimSpace(os.Getenv("GUARDIAN_OTEL_ENDPOINT"))
	traceEndpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"))
	if traceEndpoint == "" && endpoint != "" {
		scheme := "https"
		if strings.EqualFold(strings.TrimSpace(os.Getenv("GUARDIAN_OTEL_INSECURE")), "true") {
			scheme = "http"
		}
		hostPort := endpoint
		if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
			hostPort = parsed.Host
			if parsed.Scheme == "http" || parsed.Scheme == "https" {
				scheme = parsed.Scheme
			}
		}
		if host, port, err := net.SplitHostPort(hostPort); err == nil {
			if port == "4317" {
				port = "4318"
			}
			traceEndpoint = fmt.Sprintf("%s://%s/v1/traces", scheme, net.JoinHostPort(host, port))
		} else {
			traceEndpoint = fmt.Sprintf("%s://%s:4318/v1/traces", scheme, strings.TrimSuffix(hostPort, "/"))
		}
	}
	set("LB_OTEL_ENDPOINT", endpoint)
	set("LB_OTEL_METRIC_INTERVAL", "15s")
	set("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", traceEndpoint)
	set("OTEL_METRICS_EXPORTER", "none")
	set("OTEL_LOGS_EXPORTER", "none")
	return env
}

// ApplyObservabilityEnv injects identity plus the effective OTLP endpoint. The
// defaultEndpoint is provider-specific and is used when neither the intent nor
// the targeting Observability asset specifies one.

func ApplyObservabilityEnv(env map[string]string, in registry.AssetInput, provider, defaultEndpoint string) map[string]string {
	settings := ObservabilityForAsset(in)
	// Identity is independent of whether an Observability asset exists. Every
	// managed workload gets a canonical service.name; the Observability asset
	// only controls the endpoint and protocol.
	env = ApplyOtelEnvWithServiceName(env, in, provider, settings.ServiceName)
	if !settings.Enabled {
		return env
	}
	endpoint := strings.TrimSpace(settings.Endpoint)
	if endpoint == "" {
		endpoint = strings.TrimSpace(defaultEndpoint)
	}
	if endpoint != "" && strings.TrimSpace(env["OTEL_EXPORTER_OTLP_ENDPOINT"]) == "" {
		env["OTEL_EXPORTER_OTLP_ENDPOINT"] = endpoint
	}
	if strings.TrimSpace(env["OTEL_EXPORTER_OTLP_PROTOCOL"]) == "" {
		protocol := strings.TrimSpace(settings.Protocol)
		if protocol == "" {
			protocol = ProtocolForEndpoint(endpoint)
		}
		env["OTEL_EXPORTER_OTLP_PROTOCOL"] = protocol
	}
	// Keep the cross-boundary propagation contract identical for k8s, Docker,
	// and AWS workloads. Explicit workload values win.
	if strings.TrimSpace(env["OTEL_PROPAGATORS"]) == "" {
		env["OTEL_PROPAGATORS"] = "tracecontext,baggage,xray"
	}
	if strings.TrimSpace(env["OTEL_TRACES_SAMPLER"]) == "" {
		env["OTEL_TRACES_SAMPLER"] = "parentbased_always_on"
	}
	return env
}

// ProtocolForEndpoint picks the OTLP transport that matches the endpoint. The
// stable collector and AWS gateway use gRPC on 4317; HTTP endpoints on 4318
// (or explicit signal paths) use HTTP/protobuf.

func ProtocolForEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "grpc"
	}
	lower := strings.ToLower(endpoint)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if strings.Contains(lower, ":4318") || strings.Contains(lower, "/v1/") {
			return "http/protobuf"
		}
	}
	return "grpc"
}

// ObservabilitySettings is the effective observability configuration for one
// compute asset, resolved from targeting Observability assets.

type ObservabilitySettings struct {
	Enabled     bool
	Mode        string
	Endpoint    string
	Protocol    string
	ServiceName string
	CloudWatch  bool
	AssetName   string
}

// ObservabilityForAsset resolves the Observability asset that targets the given
// compute asset. The first matching asset in name order wins.

func ObservabilityForAsset(in registry.AssetInput) ObservabilitySettings {
	names := make([]string, 0, len(in.Assets))
	for name := range in.Assets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		asset := in.Assets[name]
		if asset.Type != assetdomain.TypeObservability {
			continue
		}
		_, typed, err := DecodeNamedAsset(in, name)
		if err != nil {
			continue
		}
		spec, ok := typed.(*assetdefs.ObservabilitySpec)
		if !ok || !containsString(spec.Targets, in.Asset.Name) {
			continue
		}
		settings := ObservabilitySettings{
			Enabled:     true,
			Mode:        strings.ToLower(strings.TrimSpace(spec.Mode)),
			Endpoint:    strings.TrimSpace(spec.Endpoint),
			Protocol:    strings.TrimSpace(spec.Protocol),
			ServiceName: strings.TrimSpace(spec.ServiceName),
			AssetName:   name,
		}
		if spec.CloudWatch != nil {
			settings.CloudWatch = *spec.CloudWatch
		}
		return settings
	}
	return ObservabilitySettings{}
}

func parseOtelResourceAttrs(raw string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	return out
}

func formatOtelResourceAttrs(attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(attrs[key])
	}
	return b.String()
}
