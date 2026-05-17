package planner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/compiler/dag"
	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	"github.com/rydzu/ainfra/guardian/internal/compiler/resolver"
	"github.com/rydzu/ainfra/guardian/internal/compiler/validator"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	"github.com/rydzu/ainfra/guardian/internal/versioning/digest"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
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
		if err := validator.ValidateIntent(parsed, names, nil); err != nil {
			return nil, fmt.Errorf("intent %s invalid: %w", parsed.Metadata.Name, err)
		}

		joinGraph.AddNode(parsed.Metadata.Name, parsed.Spec.Joins)

		resolvedIntent := *parsed
		resolvedAssets := make([]intentdomain.AssetSpec, len(parsed.Spec.Assets))
		assetGraph := dag.New()
		assetVersionIDs := make(map[string]string, len(parsed.Spec.Assets))
		assetVersions := make(map[string]string, len(parsed.Spec.Assets))
		refs := make([]resolver.OutputRef, 0)
		for idx, asset := range parsed.Spec.Assets {
			refs = append(refs, resolver.FindRefs(asset.Properties)...)
			resolvedAsset := asset
			resolvedProps, err := resolver.ResolveProperties(asset.Properties, input.CurrentOutputs)
			if err == nil {
				resolvedAsset.Properties = resolvedProps
			} else {
				resolvedAsset.Properties = cloneProperties(asset.Properties)
			}
			resolvedAsset.Version = strings.TrimSpace(resolvedAsset.Version)
			resolvedAssets[idx] = resolvedAsset
			assetGraph.AddNode(asset.Name, asset.DependsOn)
			assetVersionID := revisions.AssetVersionID(parsed.Metadata.Name, resolvedAsset)
			assetVersionIDs[asset.Name] = assetVersionID
			assetVersion := resolvedAsset.Version
			if assetVersion == "" {
				assetVersion = revisions.DerivedAssetVersionAt(assetVersionID, input.IntentModTimes[parsed.Metadata.Name])
			}
			assetVersions[asset.Name] = assetVersion
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
