package probes

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
)

func (r *Runner) discover(ctx context.Context) ([]Target, error) {
	entries, err := r.cfg.Store.ListDir(ctx, paths.PartitionsRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	targets := make([]Target, 0)
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		partition := entry.Name
		intentEntries, err := r.cfg.Store.ListDir(ctx, paths.PartitionIntentsDir(partition))
		if err != nil {
			continue
		}
		for _, intentEntry := range intentEntries {
			if intentEntry.IsDir || !strings.HasSuffix(intentEntry.Name, ".yaml") {
				continue
			}
			intentName := strings.TrimSuffix(intentEntry.Name, ".yaml")
			intent, err := r.loadIntent(ctx, partition, intentName)
			if err != nil || intent == nil {
				continue
			}
			if strings.TrimSpace(intent.Spec.TargetPusher) != r.cfg.Pusher {
				continue
			}
			state, err := r.loadIntentState(ctx, partition, intentName)
			if err != nil || state == nil {
				continue
			}
			targets = append(targets, r.targetsForIntent(intent, state)...)
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	if len(targets) > r.cfg.MaxTargets {
		targets = targets[:r.cfg.MaxTargets]
	}
	return targets, nil
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

func (r *Runner) targetsForIntent(intent *intentView, state *statedomain.IntentState) []Target {
	targets := make([]Target, 0)
	explicitTargets := map[string]bool{}
	outputs := state.Outputs

	for _, asset := range intent.Spec.Assets {
		if asset.Type != assetdomain.TypeProbe {
			continue
		}
		target, ok := r.explicitProbe(intent, state, asset, outputs)
		if !ok {
			continue
		}
		targets = append(targets, target)
		if target.Asset != "" {
			explicitTargets[target.Asset] = true
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
		target, ok := r.autoprobeTarget(intent, state, asset, output, value)
		if ok {
			targets = append(targets, target)
		}
	}
	return targets
}

func (r *Runner) explicitProbe(intent *intentView, state *statedomain.IntentState, asset assetdomain.Spec, outputs map[string]string) (Target, bool) {
	typed, _, err := assetdefs.Decode(asset)
	if err != nil {
		return Target{}, false
	}
	spec, ok := typed.(*assetdefs.ProbeSpec)
	if !ok || (spec.Enabled != nil && !*spec.Enabled) {
		return Target{}, false
	}
	assetName := spec.Target
	if assetName == "" {
		assetName = asset.Name
	}
	resolved := resolveProbeAddress(spec, outputs, assetName)
	if resolved == "" && spec.URL == "" && spec.Address == "" && spec.Name == "" {
		return Target{}, false
	}
	probeType := strings.ToLower(strings.TrimSpace(spec.Type))
	if probeType == "" {
		probeType = inferProbeType(resolved, spec.Port)
	}
	interval := parseDuration(spec.Interval, r.cfg.Interval)
	timeout := parseDuration(spec.Timeout, r.cfg.Timeout)
	retries := r.cfg.Retries
	if spec.Retries != nil {
		retries = *spec.Retries
	}
	target := Target{
		Name:         firstNonEmpty(spec.Name, asset.Name),
		Service:      "",
		Partition:    intent.Partition,
		Intent:       intent.Name,
		Asset:        assetName,
		Mode:         "explicit",
		Type:         probeType,
		URL:          spec.URL,
		Address:      spec.Address,
		Port:         spec.Port,
		Path:         spec.Path,
		Method:       firstNonEmpty(spec.Method, "GET"),
		Headers:      spec.Headers,
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
	return target, true
}

func (r *Runner) autoprobeTarget(intent *intentView, state *statedomain.IntentState, asset, output, value string) (Target, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Target{}, false
	}
	probeType := inferProbeType(value, 0)
	if output == "dns_name" {
		probeType = "http"
	}
	target := Target{
		Name:      "auto-" + probeType,
		Partition: intent.Partition,
		Intent:    intent.Name,
		Asset:     asset,
		Mode:      "auto",
		Type:      probeType,
		Interval:  r.cfg.Interval,
		Timeout:   r.cfg.Timeout,
		Retries:   r.cfg.Retries,
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		target.URL = value
	} else {
		target.Address = value
		target.Port = portFromAddress(value)
	}
	identity := driverutil.CanonicalServiceIdentity(intent.Partition, intent.Name, asset)
	target.Service = identity.Name
	target.FullName = identity.FullName
	target.ID = ProbeID(r.cfg.Pusher, intent.Partition, intent.Name, asset, target.Name, target.Type, targetValue(target))
	return target, true
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
