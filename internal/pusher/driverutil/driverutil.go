package driverutil

import (
	"context"
	"fmt"
	"sort"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/pusher/secrets"
	"github.com/rydzu/ainfra/guardian/internal/versioning/digest"
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
		"guardian.managed":   "true",
		"guardian.provider":  provider,
		"guardian.partition": in.PartitionName,
		"guardian.intent":    in.IntentName,
		"guardian.asset":     in.Asset.Name,
		"guardian.type":      in.Asset.Type,
		"guardian.hash":      limitLabelValue(hash),
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

// MaxServiceNameLength is the backend-safe cap for the generated service.name.
// 200 matches the AWS X-Ray segment-name limit and is accepted by the other
// Doctor/Grafana backends.
const MaxServiceNameLength = 200

// ServiceIdentity is the canonical telemetry identity of one asset.
type ServiceIdentity struct {
	Name      string
	FullName  string
	Truncated bool
}

// CanonicalServiceIdentity builds <partition>.<intent>.<asset> and applies a
// deterministic hash suffix when the name exceeds MaxServiceNameLength.
func CanonicalServiceIdentity(partition, intent, asset string) ServiceIdentity {
	parts := make([]string, 0, 3)
	for _, part := range []string{partition, intent, asset} {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	full := strings.Join(parts, ".")
	if full == "" {
		return ServiceIdentity{}
	}
	if len(full) <= MaxServiceNameLength {
		return ServiceIdentity{Name: full, FullName: full}
	}
	sum := digest.MustNormalizedHash([]string{full})[:10]
	keep := MaxServiceNameLength - 1 - len(sum)
	if keep < 1 {
		keep = 1
	}
	prefix := strings.TrimRight(full[:keep], "._-")
	if prefix == "" {
		prefix = "service"
	}
	return ServiceIdentity{Name: prefix + "." + sum, FullName: full, Truncated: true}
}

// ApplyOtelEnv injects the canonical identity and topology attributes into the
// workload environment. Explicit OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES
// values are preserved; generated values only fill gaps.
func ApplyOtelEnv(env map[string]string, in registry.AssetInput, provider string) map[string]string {
	if env == nil {
		env = map[string]string{}
	}
	identity := CanonicalServiceIdentity(in.PartitionName, in.IntentName, in.Asset.Name)
	attrs := parseOtelResourceAttrs(env["OTEL_RESOURCE_ATTRIBUTES"])

	serviceName := strings.TrimSpace(env["OTEL_SERVICE_NAME"])
	if serviceName == "" {
		if existing := strings.TrimSpace(attrs["service.name"]); existing != "" {
			serviceName = existing
		} else {
			serviceName = identity.Name
		}
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
	setGenerated("service.namespace", in.PartitionName)
	setGenerated("guardian.partition", in.PartitionName)
	setGenerated("guardian.intent", in.IntentName)
	setGenerated("guardian.asset", in.Asset.Name)
	setGenerated("guardian.pusher", in.PusherName)
	setGenerated("guardian.provider", provider)
	setGenerated("guardian.cluster", strings.TrimSpace(in.Target.Cluster))
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
