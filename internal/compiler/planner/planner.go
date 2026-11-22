package planner

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/compiler/dag"
	"github.com/radryc/guardian/internal/compiler/manifest"
	"github.com/radryc/guardian/internal/compiler/resolver"
	"github.com/radryc/guardian/internal/compiler/validator"
	"github.com/radryc/guardian/internal/compiler/values"
	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	partitiondomain "github.com/radryc/guardian/internal/domain/partition"
	"github.com/radryc/guardian/internal/versioning/digest"
	"github.com/radryc/guardian/internal/versioning/revisions"
)

type CompiledPartition struct {
	PartitionRevision string
	ConfigVersionID   string
	IntentVersions    map[string]string
	IntentOrder       []string
	Intents           map[string]*CompiledIntent
}

type CompiledIntent struct {
	Name            string
	Spec            *intentdomain.Intent
	IntentVersionID string
	IntentSpecHash  string
	AssetVersionIDs map[string]string
	AssetVersions   map[string]string
	AssetOrder      []string
	OutputRefs      []resolver.OutputRef
}

type CompileInput struct {
	PartitionName    string
	ConfigContent    []byte
	IntentContents   map[string][]byte
	IntentVersionIDs map[string]string
	IntentModTimes   map[string]time.Time
	ConfigVersionID  string
	CurrentOutputs   map[string]map[string]string
}

func Compile(ctx context.Context, input CompileInput) (*CompiledPartition, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	part, err := manifest.ParsePartition(input.ConfigContent)
	if err != nil {
		return nil, err
	}
	if input.PartitionName != "" && part.Metadata.Name != input.PartitionName {
		return nil, fmt.Errorf("partition name mismatch: expected %q got %q", input.PartitionName, part.Metadata.Name)
	}
	if err := resolvePartition(part); err != nil {
		return nil, fmt.Errorf("partition values: %w", err)
	}
	if err := validator.ValidatePartition(part); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(input.IntentContents))
	for name := range input.IntentContents {
		names = append(names, name)
	}
	sort.Strings(names)

	result := &CompiledPartition{
		ConfigVersionID: input.ConfigVersionID,
		IntentVersions:  map[string]string{},
		Intents:         map[string]*CompiledIntent{},
	}
	joinGraph := dag.New()

	for _, key := range names {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		parsed, err := manifest.ParseIntent(input.IntentContents[key])
		if err != nil {
			return nil, fmt.Errorf("intent %s: %w", key, err)
		}
		if key != "" && parsed.Metadata.Name != key {
			return nil, fmt.Errorf("intent map key %q does not match metadata.name %q", key, parsed.Metadata.Name)
		}
		if err := ApplyValues(part.Spec.Values, parsed); err != nil {
			return nil, fmt.Errorf("intent %s values: %w", parsed.Metadata.Name, err)
		}
		if err := validator.ValidateIntent(parsed, names, nil); err != nil {
			return nil, fmt.Errorf("intent %s invalid: %w", parsed.Metadata.Name, err)
		}
		if synthetic, ok := defaultObservabilityAsset(part.Spec.Defaults.Observability, parsed); ok {
			parsed.Spec.Assets = append(parsed.Spec.Assets, synthetic)
		}

		joinGraph.AddNode(parsed.Metadata.Name, parsed.Spec.Joins)

		resolvedIntent := *parsed
		resolvedAssets := make([]intentdomain.AssetSpec, 0, len(parsed.Spec.Assets))
		assetGraph := dag.New()
		assetVersionIDs := make(map[string]string, len(parsed.Spec.Assets))
		assetVersions := make(map[string]string, len(parsed.Spec.Assets))
		skippedAssetNames := map[string]bool{}
		refs := make([]resolver.OutputRef, 0)
		for _, asset := range parsed.Spec.Assets {
			if asset.Skip {
				skippedAssetNames[asset.Name] = true
				continue
			}
			refs = append(refs, resolver.FindRefs(asset.Properties)...)
			resolvedAsset := asset
			resolvedProps, err := resolver.ResolveProperties(asset.Properties, input.CurrentOutputs)
			if err == nil {
				resolvedAsset.Properties = resolvedProps
			} else {
				resolvedAsset.Properties = cloneProperties(asset.Properties)
			}
			resolvedAsset.Version = strings.TrimSpace(resolvedAsset.Version)
			resolvedAssets = append(resolvedAssets, resolvedAsset)
			filteredDeps := filterSkippedDeps(asset.DependsOn, skippedAssetNames)
			assetGraph.AddNode(asset.Name, filteredDeps)
			assetVersionID := revisions.AssetVersionID(parsed.Metadata.Name, resolvedAsset)
			assetVersionIDs[asset.Name] = assetVersionID
			assetVersion := resolvedAsset.Version
			if assetVersion == "" {
				assetVersion = revisions.DerivedAssetVersionAt(assetVersionID, input.IntentModTimes[parsed.Metadata.Name])
			}
			assetVersions[asset.Name] = assetVersion
		}
		if len(skippedAssetNames) > 0 {
			log.Printf("planner: intent %s skipped %d assets, removing from dependency graph", parsed.Metadata.Name, len(skippedAssetNames))
		}
		assetOrder, err := assetGraph.TopologicalSort()
		if err != nil {
			return nil, fmt.Errorf("intent %s asset order: %w", parsed.Metadata.Name, err)
		}
		resolvedIntent.Spec.Assets = resolvedAssets

		intentVersionID := input.IntentVersionIDs[parsed.Metadata.Name]
		if intentVersionID == "" {
			intentVersionID = "ver_" + digest.ContentHash(input.IntentContents[key])[:16]
		}
		result.IntentVersions[parsed.Metadata.Name] = intentVersionID
		result.Intents[parsed.Metadata.Name] = &CompiledIntent{
			Name:            parsed.Metadata.Name,
			Spec:            &resolvedIntent,
			IntentVersionID: intentVersionID,
			IntentSpecHash:  digest.MustNormalizedHash(resolvedIntent.Spec),
			AssetVersionIDs: assetVersionIDs,
			AssetVersions:   assetVersions,
			AssetOrder:      assetOrder,
			OutputRefs:      dedupeRefs(refs),
		}
	}

	order, err := joinGraph.TopologicalSort()
	if err != nil {
		return nil, fmt.Errorf("intent dependency graph invalid: %w", err)
	}
	result.IntentOrder = order
	result.PartitionRevision = revisions.PartitionRevision(input.ConfigVersionID, result.IntentVersions)
	return result, nil
}

func resolvePartition(part *partitiondomain.Partition) error {
	if part == nil || len(part.Spec.Values) == 0 {
		return nil
	}
	vals := part.Spec.Values
	strings := []*string{
		&part.Spec.DeletionPolicy,
		&part.Spec.Reconciliation.Mode,
		&part.Spec.Reconciliation.Interval,
		&part.Spec.PrimarySet,
		&part.Spec.Defaults.TargetPusher,
		&part.Spec.Defaults.Target.Cluster,
		&part.Spec.Defaults.Target.Namespace,
		&part.Spec.Defaults.Observability.AutoInstrument,
		&part.Spec.Defaults.Observability.ServiceName,
		&part.Spec.Defaults.Observability.OTLPEndpoint,
		&part.Spec.Defaults.Observability.CloudWatch,
		&part.Spec.Defaults.Observability.Instrumentation,
		&part.Spec.Defaults.Probes.Interval,
		&part.Spec.Defaults.Probes.Timeout,
	}
	for _, field := range strings {
		resolved, err := values.ResolveString(*field, vals)
		if err != nil {
			return err
		}
		*field = resolved
	}
	var err error
	if part.Metadata.Labels, err = values.ResolveStringMap(part.Metadata.Labels, vals); err != nil {
		return err
	}
	if part.Spec.Labels, err = values.ResolveStringMap(part.Spec.Labels, vals); err != nil {
		return err
	}
	if part.Spec.Sets, err = values.ResolveStringSlice(part.Spec.Sets, vals); err != nil {
		return err
	}
	if part.Spec.Defaults.Probes.HTTPPaths, err = values.ResolveStringSlice(part.Spec.Defaults.Probes.HTTPPaths, vals); err != nil {
		return err
	}
	return nil
}

// ApplyValues resolves partition, intent, and asset values on a parsed intent
// using the precedence partition -> intent -> asset.
func ApplyValues(partitionValues map[string]any, intent *intentdomain.Intent) error {
	if intent == nil {
		return nil
	}
	intentValues := values.Merge(partitionValues, intent.Spec.Values)
	if err := resolveIntent(&intent.Spec, intentValues); err != nil {
		return err
	}
	for i := range intent.Spec.Assets {
		assetValues := values.Merge(intentValues, intent.Spec.Assets[i].Values)
		if err := resolveAsset(&intent.Spec.Assets[i], assetValues); err != nil {
			return fmt.Errorf("asset %s: %w", intent.Spec.Assets[i].Name, err)
		}
	}
	return nil
}

func resolveIntent(spec *intentdomain.IntentSpec, vals map[string]any) error {
	if spec == nil || len(vals) == 0 {
		return nil
	}
	strings := []*string{
		&spec.IntentType,
		&spec.Implementation,
		&spec.TargetPusher,
		&spec.Target.Cluster,
		&spec.Target.Namespace,
	}
	for _, field := range strings {
		resolved, err := values.ResolveString(*field, vals)
		if err != nil {
			return err
		}
		*field = resolved
	}
	var err error
	if spec.Joins, err = values.ResolveStringSlice(spec.Joins, vals); err != nil {
		return err
	}
	for i := range spec.Hints {
		for _, field := range []*string{&spec.Hints[i].Path, &spec.Hints[i].Title, &spec.Hints[i].Description} {
			resolved, err := values.ResolveString(*field, vals)
			if err != nil {
				return err
			}
			*field = resolved
		}
	}
	return nil
}

func resolveAsset(asset *assetdomain.Spec, vals map[string]any) error {
	if asset == nil || len(vals) == 0 {
		return nil
	}
	var err error
	if asset.Properties, err = values.ResolveMap(asset.Properties, vals); err != nil {
		return err
	}
	if asset.Payload, err = values.ResolveStringMap(asset.Payload, vals); err != nil {
		return err
	}
	if asset.DependsOn, err = values.ResolveStringSlice(asset.DependsOn, vals); err != nil {
		return err
	}
	if asset.FlowMetricGroups, err = values.ResolveStringSlice(asset.FlowMetricGroups, vals); err != nil {
		return err
	}
	for i := range asset.Hints {
		for _, field := range []*string{&asset.Hints[i].Path, &asset.Hints[i].Title, &asset.Hints[i].Description} {
			resolved, err := values.ResolveString(*field, vals)
			if err != nil {
				return err
			}
			*field = resolved
		}
	}
	if asset.Version, err = values.ResolveString(asset.Version, vals); err != nil {
		return err
	}
	return nil
}

func defaultObservabilityAsset(defaults partitiondomain.ObservabilityDefaults, intent *intentdomain.Intent) (assetdomain.Spec, bool) {
	if intent == nil || strings.EqualFold(intent.Spec.IntentType, intentdomain.TypeObserved) {
		return assetdomain.Spec{}, false
	}
	if defaults.Enabled == nil || !*defaults.Enabled {
		return assetdomain.Spec{}, false
	}
	if strings.EqualFold(defaults.AutoInstrument, "none") || strings.EqualFold(defaults.Instrumentation, "none") {
		return assetdomain.Spec{}, false
	}
	existing := map[string]bool{}
	targets := make([]string, 0)
	for _, asset := range intent.Spec.Assets {
		existing[asset.Name] = true
		if asset.Type == assetdomain.TypeObservability {
			return assetdomain.Spec{}, false
		}
		if asset.Type == "Compute" {
			targets = append(targets, asset.Name)
		}
	}
	if len(targets) == 0 {
		return assetdomain.Spec{}, false
	}
	name := "observability"
	if existing[name] {
		name = "observability-default"
	}
	properties := map[string]any{
		"provider": "otel",
		"mode":     firstNonEmpty(defaults.Instrumentation, "auto"),
		"targets":  targets,
	}
	if serviceName := strings.TrimSpace(defaults.ServiceName); serviceName != "" {
		properties["serviceName"] = serviceName
	}
	if endpoint := strings.TrimSpace(defaults.OTLPEndpoint); endpoint != "" {
		properties["endpoint"] = endpoint
	}
	switch strings.ToLower(strings.TrimSpace(defaults.CloudWatch)) {
	case "true":
		properties["cloudwatch"] = true
	case "false":
		properties["cloudwatch"] = false
	}
	return assetdomain.Spec{Type: assetdomain.TypeObservability, Name: name, Properties: properties}, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func dedupeRefs(refs []resolver.OutputRef) []resolver.OutputRef {
	seen := map[string]resolver.OutputRef{}
	for _, ref := range refs {
		seen[ref.Placeholder] = ref
	}
	out := make([]resolver.OutputRef, 0, len(seen))
	for _, ref := range seen {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IntentName == out[j].IntentName {
			if out[i].OutputKey == out[j].OutputKey {
				return out[i].Placeholder < out[j].Placeholder
			}
			return out[i].OutputKey < out[j].OutputKey
		}
		return out[i].IntentName < out[j].IntentName
	})
	return out
}

func cloneProperties(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out[key] = cloneValue(in[key])
	}
	return out
}

func cloneValue(in any) any {
	switch typed := in.(type) {
	case map[string]any:
		return cloneProperties(typed)
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

func filterSkippedDeps(deps []string, skipped map[string]bool) []string {
	if len(skipped) == 0 {
		return deps
	}
	filtered := make([]string, 0, len(deps))
	for _, dep := range deps {
		if !skipped[dep] {
			filtered = append(filtered, dep)
		}
	}
	return filtered
}
