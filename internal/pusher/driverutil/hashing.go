package driverutil

import (
	"sort"

	targetdomain "github.com/radryc/guardian/internal/domain/target"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/versioning/digest"
)

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
