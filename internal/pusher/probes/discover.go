package probes

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/compiler/manifest"
	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	partitiondomain "github.com/radryc/guardian/internal/domain/partition"
	statedomain "github.com/radryc/guardian/internal/domain/state"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/pusher/driverutil"
	"github.com/radryc/guardian/internal/pusher/secrets"
)

// probePolicy is the effective probe configuration for one partition. It
// starts from pusher-level defaults and is overridden by partition defaults.
type probePolicy struct {
	Enabled       bool
	Interval      time.Duration
	Timeout       time.Duration
	Retries       int
	HTTPPaths     []string
	TCPPorts      []int
	MaxTargets    int
	AllowExternal bool
}

func (r *Runner) defaultProbePolicy() probePolicy {
	return probePolicy{
		Enabled:       r.cfg.Enabled,
		Interval:      r.cfg.Interval,
		Timeout:       r.cfg.Timeout,
		Retries:       r.cfg.Retries,
		HTTPPaths:     append([]string(nil), r.cfg.HTTPPaths...),
		TCPPorts:      append([]int(nil), r.cfg.TCPPorts...),
		AllowExternal: r.cfg.AllowExternal,
	}
}

func (r *Runner) partitionProbePolicy(defaults partitiondomain.ProbeDefaults) probePolicy {
	policy := r.defaultProbePolicy()
	if defaults.Enabled != nil {
		policy.Enabled = *defaults.Enabled
	}
	if value := strings.TrimSpace(defaults.Interval); value != "" {
		policy.Interval = parseDuration(value, policy.Interval)
	}
	if value := strings.TrimSpace(defaults.Timeout); value != "" {
		policy.Timeout = parseDuration(value, policy.Timeout)
	}
	if defaults.Retries != nil && *defaults.Retries >= 0 {
		policy.Retries = *defaults.Retries
	}
	if len(defaults.HTTPPaths) > 0 {
		policy.HTTPPaths = append([]string(nil), defaults.HTTPPaths...)
	}
	if len(defaults.TCPPorts) > 0 {
		policy.TCPPorts = append([]int(nil), defaults.TCPPorts...)
	}
	if defaults.MaxTargets != nil && *defaults.MaxTargets > 0 {
		policy.MaxTargets = *defaults.MaxTargets
	}
	if defaults.AllowExternal != nil {
		policy.AllowExternal = *defaults.AllowExternal
	}
	return policy
}

func (r *Runner) discover(ctx context.Context) ([]Target, error) {
	targets := make([]Target, 0)
	if r.cfg.ExternalTargets != nil {
		external, err := r.cfg.ExternalTargets(ctx)
		if err != nil {
			return nil, err
		}
		targets = append(targets, external...)
	}
	if r.cfg.Store == nil {
		return r.finalizeTargets(targets), nil
	}
	entries, err := r.cfg.Store.ListDir(ctx, paths.PartitionsRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return r.finalizeTargets(targets), nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		partition := entry.Name
		defaultPusher := ""
		policy := r.defaultProbePolicy()
		if raw, err := r.cfg.Store.ReadFile(ctx, paths.PartitionConfig(partition)); err == nil {
			if parsed, err := manifest.ParsePartition(raw); err == nil {
				defaultPusher = strings.TrimSpace(parsed.Spec.Defaults.TargetPusher)
				policy = r.partitionProbePolicy(parsed.Spec.Defaults.Probes)
			}
		}
		if !policy.Enabled {
			continue
		}
		intentEntries, err := r.cfg.Store.ListDir(ctx, paths.PartitionIntentsDir(partition))
		if err != nil {
			continue
		}
		partitionTargets := make([]Target, 0)
		for _, intentEntry := range intentEntries {
			if intentEntry.IsDir || !strings.HasSuffix(intentEntry.Name, ".yaml") {
				continue
			}
			intentName := strings.TrimSuffix(intentEntry.Name, ".yaml")
			intent, err := r.loadIntent(ctx, partition, intentName)
			if err != nil || intent == nil {
				continue
			}
			targetPusher := strings.TrimSpace(intent.Spec.TargetPusher)
			if targetPusher == "" {
				targetPusher = defaultPusher
			}
			if targetPusher != r.cfg.Pusher {
				continue
			}
			state, err := r.loadIntentState(ctx, partition, intentName)
			if err != nil || state == nil {
				continue
			}
			partitionTargets = append(partitionTargets, r.targetsForIntent(ctx, intent, state, policy)...)
		}
		sortTargets(partitionTargets)
		if policy.MaxTargets > 0 && len(partitionTargets) > policy.MaxTargets {
			partitionTargets = partitionTargets[:policy.MaxTargets]
		}
		targets = append(targets, partitionTargets...)
	}
	return r.finalizeTargets(targets), nil
}

// finalizeTargets deduplicates and applies sharding/limits to targets from all
// discovery sources. External onboarding targets and store targets can overlap.
func (r *Runner) finalizeTargets(targets []Target) []Target {
	seen := make(map[string]struct{}, len(targets))
	deduped := targets[:0]
	for _, target := range targets {
		key := target.ID
		if key == "" {
			key = target.Partition + "|" + target.Intent + "|" + target.Asset + "|" + target.Name + "|" + target.Type + "|" + targetValue(target)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, target)
	}
	targets = deduped
	sortTargets(targets)
	if r.cfg.ShardCount > 1 {
		owned := targets[:0]
		for _, target := range targets {
			if r.ownsShard(target.ID) {
				owned = append(owned, target)
			}
		}
		targets = owned
	}
	if r.cfg.MaxTargets > 0 && len(targets) > r.cfg.MaxTargets {
		targets = targets[:r.cfg.MaxTargets]
	}
	return targets
}

func (r *Runner) ownsShard(id string) bool {
	if r.cfg.ShardCount <= 1 {
		return true
	}
	sum := sha256.Sum256([]byte(id))
	index := int(binary.BigEndian.Uint64(sum[:8]) % uint64(r.cfg.ShardCount))
	return index == r.cfg.ShardIndex
}

func (r *Runner) loadIntent(ctx context.Context, partition, intent string) (*intentView, error) {
	raw, err := r.cfg.Store.ReadFile(ctx, paths.IntentManifest(partition, intent))
	if err != nil {
		return nil, err
	}
	parsed, err := manifest.ParseIntent(raw)
	if err != nil {
		return nil, err
	}
	return &intentView{Partition: partition, Name: intent, Spec: parsed.Spec}, nil
}

type intentView struct {
	Partition string
	Name      string
	Spec      intentdomain.IntentSpec
}

func (r *Runner) loadIntentState(ctx context.Context, partition, intent string) (*statedomain.IntentState, error) {
	raw, err := r.cfg.Store.ReadFile(ctx, paths.IntentState(partition, intent))
	if err != nil {
		return nil, err
	}
	var state statedomain.IntentState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (r *Runner) targetsForIntent(ctx context.Context, intent *intentView, state *statedomain.IntentState, policy probePolicy) []Target {
	targets := make([]Target, 0)
	explicitTargets := map[string]bool{}
	outputs := state.Outputs

	for _, asset := range intent.Spec.Assets {
		if asset.Type != assetdomain.TypeProbe {
			continue
		}
		target, managed, ok := r.explicitProbe(ctx, intent, state, asset, outputs, policy)
		if managed && target.Asset != "" {
			// A disabled Probe asset still owns its target asset: autoprobes
			// must not silently replace it.
			explicitTargets[target.Asset] = true
		}
		if ok {
			targets = append(targets, target)
		}
	}

	for key, value := range outputs {
		asset, output, ok := strings.Cut(key, ".")
		if !ok || value == "" || explicitTargets[asset] {
			continue
		}
		switch output {
		case "address", "endpoint", "url", "dns_name", "host":
		default:
			continue
		}
		targets = append(targets, r.autoprobeTargets(intent, asset, output, value, policy)...)
	}
	return targets
}

func (r *Runner) explicitProbe(ctx context.Context, intent *intentView, state *statedomain.IntentState, asset assetdomain.Spec, outputs map[string]string, policy probePolicy) (Target, bool, bool) {
	typed, _, err := assetdefs.Decode(asset)
	if err != nil {
		return Target{}, false, false
	}
	spec, ok := typed.(*assetdefs.ProbeSpec)
	if !ok {
		return Target{}, false, false
	}
	assetName := strings.TrimSpace(spec.Target)
	if assetName == "" {
		assetName = asset.Name
	}
	resolved := resolveProbeAddress(spec, outputs, assetName)
	probeType := strings.ToLower(strings.TrimSpace(spec.Type))
	if probeType == "" {
		probeType = inferProbeType(resolved, spec.Port)
	}
	if probeType == "dns" && resolved == "" {
		resolved = strings.TrimSpace(spec.Name)
	}
	managed := Target{Asset: assetName}
	if spec.Enabled != nil && !*spec.Enabled {
		return managed, true, false
	}
	if resolved == "" && spec.URL == "" && spec.Address == "" {
		return managed, true, false
	}
	external := strings.TrimSpace(spec.Target) == "" &&
		(strings.TrimSpace(spec.URL) != "" ||
			strings.TrimSpace(spec.Address) != "" ||
			(probeType == "dns" && strings.TrimSpace(spec.Name) != ""))
	if external && !policy.AllowExternal {
		return managed, true, false
	}
	interval := parseDuration(spec.Interval, policy.Interval)
	timeout := parseDuration(spec.Timeout, policy.Timeout)
	retries := policy.Retries
	if spec.Retries != nil {
		retries = *spec.Retries
	}
	headers, err := secrets.ResolveStringMap(ctx, r.cfg.Resolver, spec.Headers)
	if err != nil {
		headers = nil
	}
	target := Target{
		Name:         firstNonEmpty(spec.Name, asset.Name),
		Service:      "",
		Partition:    intent.Partition,
		Intent:       intent.Name,
		Asset:        assetName,
		AssetType:    assetTypeFor(intent, assetName),
		Mode:         "explicit",
		Type:         probeType,
		URL:          spec.URL,
		Address:      spec.Address,
		Port:         spec.Port,
		Path:         spec.Path,
		Paths:        append([]string(nil), policy.HTTPPaths...),
		Method:       firstNonEmpty(spec.Method, "GET"),
		Headers:      headers,
		ExpectStatus: spec.ExpectStatus,
		ExpectBody:   spec.ExpectBody,
		ExpectIPs:    spec.ExpectIPs,
		Interval:     interval,
		Timeout:      timeout,
		Retries:      retries,
		Insecure:     spec.InsecureSkipVerify != nil && *spec.InsecureSkipVerify,
		Labels:       spec.Labels,
	}
	if target.Type == "" {
		target.Type = "tcp"
	}
	if target.URL == "" && target.Address == "" {
		target.Address = resolved
	}
	if target.Port == 0 && target.Address != "" {
		target.Port = portFromAddress(target.Address)
	}
	identity := driverutil.CanonicalServiceIdentity(intent.Partition, intent.Name, assetName)
	target.Service = identity.Name
	target.FullName = identity.FullName
	target.ID = ProbeID(r.cfg.Pusher, intent.Partition, intent.Name, assetName, target.Name, target.Type, targetValue(target))
	return target, true, true
}

func (r *Runner) autoprobeTargets(intent *intentView, asset, output, value string, policy probePolicy) []Target {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	probeType := inferProbeType(value, 0)
	if output == "dns_name" {
		probeType = "http"
	}
	name := "auto-" + probeType
	if output != "" {
		name += "-" + output
	}
	base := Target{
		Name:      name,
		Partition: intent.Partition,
		Intent:    intent.Name,
		Asset:     asset,
		AssetType: assetTypeFor(intent, asset),
		Mode:      "auto",
		Type:      probeType,
		Paths:     append([]string(nil), policy.HTTPPaths...),
		Interval:  policy.Interval,
		Timeout:   policy.Timeout,
		Retries:   policy.Retries,
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		base.URL = value
	} else {
		base.Address = value
		base.Port = portFromAddress(value)
	}
	identity := driverutil.CanonicalServiceIdentity(intent.Partition, intent.Name, asset)
	base.Service = identity.Name
	base.FullName = identity.FullName

	// Host-only outputs have no port. Try the configured TCP ports instead of
	// silently probing port 80 only.
	if base.Type == "tcp" && base.Port == 0 && len(policy.TCPPorts) > 0 {
		targets := make([]Target, 0, len(policy.TCPPorts))
		for _, port := range policy.TCPPorts {
			if port <= 0 {
				continue
			}
			target := base
			target.Port = port
			target.Name = fmt.Sprintf("%s-%d", base.Name, port)
			target.ID = ProbeID(r.cfg.Pusher, intent.Partition, intent.Name, asset, target.Name, target.Type, targetValue(target))
			targets = append(targets, target)
		}
		if len(targets) > 0 {
			return targets
		}
	}
	base.ID = ProbeID(r.cfg.Pusher, intent.Partition, intent.Name, asset, base.Name, base.Type, targetValue(base))
	return []Target{base}
}

func assetTypeFor(intent *intentView, asset string) string {
	if intent == nil {
		return ""
	}
	for _, candidate := range intent.Spec.Assets {
		if candidate.Name == asset {
			return string(candidate.Type)
		}
	}
	return ""
}

func resolveProbeAddress(spec *assetdefs.ProbeSpec, outputs map[string]string, asset string) string {
	if spec.URL != "" {
		return spec.URL
	}
	if spec.Address != "" {
		return spec.Address
	}
	for _, key := range []string{"url", "endpoint", "address", "dns_name", "host"} {
		if value := strings.TrimSpace(outputs[asset+"."+key]); value != "" {
			return value
		}
	}
	return ""
}

func inferProbeType(value string, explicitPort int) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "https://") {
		return "https"
	}
	if strings.HasPrefix(value, "http://") {
		return "http"
	}
	port := explicitPort
	if port == 0 {
		port = portFromAddress(value)
	}
	switch port {
	case 443:
		return "https"
	case 80, 3000, 8080, 18080, 9090, 4318:
		return "http"
	case 4317:
		return "grpc"
	default:
		return "tcp"
	}
}

func portFromAddress(value string) int {
	if parsed, err := url.Parse(value); err == nil && parsed.Port() != "" {
		if port, err := strconv.Atoi(parsed.Port()); err == nil {
			return port
		}
	}
	value = strings.TrimPrefix(strings.TrimPrefix(value, "http://"), "https://")
	if host, port, err := net.SplitHostPort(value); err == nil && host != "" {
		if parsed, err := strconv.Atoi(port); err == nil {
			return parsed
		}
	}
	return 0
}

func targetValue(target Target) string {
	if target.URL != "" {
		return target.URL
	}
	if target.Address == "" {
		return target.Name
	}
	if target.Port > 0 && !strings.Contains(target.Address, ":") {
		return fmt.Sprintf("%s:%d", target.Address, target.Port)
	}
	return target.Address
}

func parseDuration(value string, fallback time.Duration) time.Duration {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
