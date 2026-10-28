package driverutil

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	targetdomain "github.com/radryc/guardian/internal/domain/target"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/observability"
	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/pusher/secrets"
	"github.com/radryc/guardian/internal/versioning/digest"
	"gopkg.in/yaml.v3"
)

func DecodeAsset(in registry.AssetInput) (any, error) {
	typed, _, err := assetdefs.Decode(assetdomain.Spec{
		Type:       in.Asset.Type,
		Name:       in.Asset.Name,
		DependsOn:  append([]string(nil), in.Asset.DependsOn...),
		Properties: cloneMap(in.Asset.Properties),
	})
	if err != nil {
		return nil, err
	}
	return typed, nil
}

func DecodeNamedAsset(in registry.AssetInput, name string) (taskdomain.AbstractAsset, any, error) {
	asset, ok := in.Assets[name]
	if !ok {
		return taskdomain.AbstractAsset{}, nil, fmt.Errorf("referenced asset %q not found", name)
	}
	typed, _, err := assetdefs.Decode(assetdomain.Spec{
		Type:       asset.Type,
		Name:       asset.Name,
		DependsOn:  append([]string(nil), asset.DependsOn...),
		Properties: cloneMap(asset.Properties),
	})
	if err != nil {
		return taskdomain.AbstractAsset{}, nil, err
	}
	return asset, typed, nil
}

func AssetHash(in registry.AssetInput) string {
	return CompositeHash(in)
}

func CompositeHash(in registry.AssetInput, extraRefs ...string) string {
	seen := map[string]struct{}{}
	names := []string{in.Asset.Name}
	names = append(names, extraRefs...)
	collected := make([]taskdomain.AbstractAsset, 0, len(names))
	var visit func(string)
	visit = func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		if name == in.Asset.Name {
			collected = append(collected, in.Asset)
			for _, dep := range in.Asset.DependsOn {
				visit(dep)
			}
			return
		}
		asset, ok := in.Assets[name]
		if !ok {
			return
		}
		collected = append(collected, asset)
		for _, dep := range asset.DependsOn {
			visit(dep)
		}
	}
	for _, name := range names {
		visit(name)
	}
	sort.Slice(collected, func(i, j int) bool { return collected[i].Name < collected[j].Name })
	return digest.MustNormalizedHash(struct {
		Assets []taskdomain.AbstractAsset
		Target targetdomain.Placement
		Worker string
	}{
		Assets: collected,
		Target: in.Target,
	})
}

func ResourceName(prefix string, target targetdomain.Placement, partition, intent, asset string, extras ...string) string {
	parts := []string{prefix, target.Cluster, target.Namespace, target.Region, target.Account, partition, intent, asset}
	parts = append(parts, extras...)
	for i, part := range parts {
		parts[i] = sanitizePart(part)
	}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	if len(filtered) == 0 {
		filtered = []string{"guardian"}
	}
	base := strings.Join(filtered, "-")
	if len(base) <= 63 {
		return base
	}
	sum := digest.MustNormalizedHash(filtered)[:10]
	keep := 63 - 1 - len(sum)
	if keep < 1 {
		keep = 1
	}
	return strings.Trim(base[:keep], "-") + "-" + sum
}

func Labels(provider string, in registry.AssetInput, hash string) map[string]string {
	labels := map[string]string{
		"guardian.managed":       "true",
		"guardian.provider":      provider,
		"guardian.partition":     in.PartitionName,
		"guardian.intent":        in.IntentName,
		"guardian.asset":         in.Asset.Name,
		"guardian.asset.type":    in.Asset.Type,
		"guardian.identity.kind": "managed",
		"guardian.hash":          limitLabelValue(hash),
	}
	// Empty values are invalid AWS tag values; omit them instead of sending
	// `guardian.cluster=` (CloudFormation rejects empty tag values).
	if cluster := strings.TrimSpace(in.Target.Cluster); cluster != "" {
		labels["guardian.cluster"] = cluster
	}
	return labels
}

func limitLabelValue(value string) string {
	const maxLabelValueLen = 63
	if len(value) <= maxLabelValueLen {
		return value
	}
	return value[:maxLabelValueLen]
}

func ResolveEnv(ctx context.Context, resolver secrets.Resolver, values map[string]any) (map[string]string, error) {
	return secrets.ResolveStringMap(ctx, resolver, values)
}

func ConfigFiles(spec *assetdefs.ConfigSpec) map[string]string {
	if spec == nil {
		return map[string]string{}
	}
	if len(spec.Data) > 0 {
		keys := make([]string, 0, len(spec.Data))
		for key := range spec.Data {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make(map[string]string, len(keys))
		for _, key := range keys {
			out[key] = spec.Data[key]
		}
		return out
	}
	name := "config" + extensionForFormat(spec.Format)
	return map[string]string{name: spec.Content}
}

func SingleConfigFile(spec *assetdefs.ConfigSpec) (string, string, bool) {
	files := ConfigFiles(spec)
	if len(files) != 1 {
		return "", "", false
	}
	for name, content := range files {
		return name, content, true
	}
	return "", "", false
}

func BoolValue(v *bool) bool {
	return v != nil && *v
}

func IntValue(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

func LoadPayload[T any](ctx context.Context, in registry.AssetInput, providerKeys []string, out *T) (bool, error) {
	if out == nil {
		return false, fmt.Errorf("payload decode target is nil")
	}
	if in.Store == nil || len(in.Asset.Payload) == 0 {
		return false, nil
	}
	logicalPath := ""
	for _, key := range providerKeys {
		if key == "" {
			continue
		}
		if value := strings.TrimSpace(in.Asset.Payload[key]); value != "" {
			logicalPath = value
			break
		}
	}
	if logicalPath == "" {
		return false, nil
	}
	content, err := in.Store.ReadFile(ctx, logicalPath)
	if err != nil {
		return false, fmt.Errorf("read payload %s: %w", logicalPath, err)
	}
	if err := yaml.Unmarshal(content, out); err != nil {
		return false, fmt.Errorf("decode payload %s: %w", logicalPath, err)
	}
	return true, nil
}

func FirstPort(ports []assetdefs.PortSpec) int {
	for _, port := range ports {
		if port.ServicePort != nil {
			return *port.ServicePort
		}
		if port.Port != nil {
			return *port.Port
		}
		if port.ContainerPort != nil {
			return *port.ContainerPort
		}
	}
	return 0
}

// BuildBootstrapEntry builds one segment of the LB_BOOTSTRAP string for a
// single ListenerSpec and its resolved backend addresses.
//
// Output format:
//
//	[name[@protocol][description]:]externalPort=backend1,backend2,...
//
// Port allocation:
//   - Static (externalPort set):  uses the pinned value.
//   - Static (default):           uses listener.Port as external port.
//   - Dynamic (dynamic: true):    omits externalPort so the lb edge allocates
//     a free port at runtime.
func BuildBootstrapEntry(listener assetdefs.ListenerSpec, backends []string) string {
	if listener.Port == nil {
		return ""
	}

	// --- LHS: [name[@protocol][description]:]port ---
	var lhs string
	hasLabel := listener.Name != "" || listener.Protocol != ""
	if hasLabel {
		name := listener.Name
		if name == "" {
			name = fmt.Sprintf("listener-%d", *listener.Port)
		}
		var proto string
		if listener.Protocol != "" {
			proto = "@" + strings.ToLower(listener.Protocol)
		}
		var desc string
		if listener.Description != "" {
			desc = "[" + listener.Description + "]"
		}
		// external port segment
		var extPort string
		if listener.Dynamic {
			// dynamic: no external port → lb allocates one
			extPort = ""
		} else if listener.ExternalPort != nil {
			extPort = fmt.Sprintf(":%d", *listener.ExternalPort)
		} else {
			extPort = fmt.Sprintf(":%d", *listener.Port)
		}
		lhs = name + proto + desc + extPort
	} else {
		if listener.Dynamic {
			lhs = ""
		} else if listener.ExternalPort != nil {
			lhs = fmt.Sprintf("%d", *listener.ExternalPort)
		} else {
			lhs = fmt.Sprintf("%d", *listener.Port)
		}
	}

	return lhs + "=" + strings.Join(backends, ",")
}

func sanitizePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range value {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

func extensionForFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return ".json"
	case "yaml", "yml":
		return ".yaml"
	case "toml":
		return ".toml"
	case "ini":
		return ".ini"
	case "cfg", "conf":
		return ".cfg"
	case "text", "txt":
		return ".txt"
	default:
		return ".txt"
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(in any) any {
	switch typed := in.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, value := range typed {
			out[i] = cloneValue(value)
		}
		return out
	default:
		return typed
	}
}

// MaxServiceNameLength is kept for callers that need the identity cap.
const MaxServiceNameLength = observability.MaxServiceNameLength

// ServiceIdentity is an alias for the shared canonical identity type.
type ServiceIdentity = observability.ServiceIdentity

// CanonicalServiceIdentity builds <partition>.<intent>.<asset> and applies a
// deterministic hash suffix when the name exceeds MaxServiceNameLength.
func CanonicalServiceIdentity(partition, intent, asset string) ServiceIdentity {
	return observability.CanonicalServiceIdentity(partition, intent, asset)
}

// AutoInstrumentDisabled reports whether a compute asset opts out of generated
// observability through the documented workload label. The label is also
// propagated to Kubernetes workloads so operators can see the opt-out.
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

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
