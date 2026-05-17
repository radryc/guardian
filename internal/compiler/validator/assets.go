package validator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/compiler/dag"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
)

func validateIntentAssets(assets []intentdomain.AssetSpec, intentHints []assetdomain.Hint) error {
	assetNames := map[string]struct{}{}
	assetTypes := map[string]string{}
	graph := dag.New()
	orderedAssets := append([]intentdomain.AssetSpec(nil), assets...)
	sort.SliceStable(orderedAssets, func(a, b int) bool { return orderedAssets[a].Name < orderedAssets[b].Name })

	for _, asset := range orderedAssets {
		if asset.Name == "" {
			return fmt.Errorf("asset name is required")
		}
		if !namePattern.MatchString(asset.Name) {
			return fmt.Errorf("invalid asset name %q", asset.Name)
		}
		if asset.Type == "" {
			return fmt.Errorf("asset %q type is required", asset.Name)
		}
		if _, ok := assetdefs.DefinitionFor(asset.Type); !ok {
			return fmt.Errorf("asset %q has unsupported type %q", asset.Name, asset.Type)
		}
		if _, exists := assetNames[asset.Name]; exists {
			return fmt.Errorf("duplicate asset name %q", asset.Name)
		}
		assetNames[asset.Name] = struct{}{}
		assetTypes[asset.Name] = asset.Type
		for provider, logicalPath := range asset.Payload {
			if provider == "" {
				return fmt.Errorf("asset %q payload key is required", asset.Name)
			}
			logicalPath = strings.TrimSpace(logicalPath)
			if logicalPath == "" {
				return fmt.Errorf("asset %q payload[%q] must not be empty", asset.Name, provider)
			}
			if !strings.HasPrefix(logicalPath, "/") {
				return fmt.Errorf("asset %q payload[%q] must be an absolute logical path", asset.Name, provider)
			}
		}
		if asset.Type == assetdomain.TypeCDKStack {
			logicalPath := strings.TrimSpace(asset.Payload["aws"])
			if logicalPath == "" {
				return fmt.Errorf("asset %q (%s): payload.aws is required", asset.Name, asset.Type)
			}
		}
		if err := assetdefs.ValidateAssetHints(asset.Hints); err != nil {
			return fmt.Errorf("asset %q: %w", asset.Name, err)
		}
	}
	if err := assetdefs.ValidateIntentHints(intentHints, assetNames); err != nil {
		return err
	}

	ctx := assetdefs.ValidationContext{AssetTypes: assetTypes}
	for _, asset := range orderedAssets {
		for _, dep := range asset.DependsOn {
			if _, ok := assetNames[dep]; !ok {
				return fmt.Errorf("asset %q dependsOn unknown asset %q", asset.Name, dep)
			}
		}
		if err := assetdefs.Validate(asset, ctx); err != nil {
			return fmt.Errorf("asset %q (%s): %w", asset.Name, asset.Type, err)
		}
		graph.AddNode(asset.Name, asset.DependsOn)
	}
	if _, err := graph.TopologicalSort(); err != nil {
		return fmt.Errorf("asset dependency graph invalid: %w", err)
	}
	return nil
}
