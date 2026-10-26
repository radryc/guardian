package observed

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/awsobserve"
	"github.com/rydzu/ainfra/guardian/internal/cdktemplate"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	"github.com/rydzu/ainfra/guardian/internal/observedgen"
)

const maxResourceAssetName = 64

// buildResourceObservations turns desired/live template inventories plus the
// changed-resource keys from the template comparison into the per-resource
// observation list shown as synthetic assets in the UI.
func buildResourceObservations(desired, live []cdktemplate.Resource, changed []string, liveResources []awsobserve.StackResource) []statedomain.ResourceObservation {
	statuses := parseChangedResources(changed)
	liveByID := make(map[string]awsobserve.StackResource, len(liveResources))
	for _, resource := range liveResources {
		if id := strings.TrimSpace(resource.LogicalID); id != "" {
			liveByID[id] = resource
		}
	}
	applyLive := func(observation *statedomain.ResourceObservation) {
		live, ok := liveByID[observation.LogicalID]
		if !ok {
			return
		}
		observation.PhysicalID = live.PhysicalID
		observation.LiveStatus = live.Status
		observation.LiveStatusReason = live.StatusReason
		observation.DriftStatus = live.DriftStatus
		observation.LiveUpdatedAt = live.UpdatedAt
	}
	copyTemplateEdges := func(observation *statedomain.ResourceObservation, resource cdktemplate.Resource) {
		observation.DependsOn = append([]string(nil), resource.DependsOn...)
		observation.Parent = resource.Parent
		observation.Relations = make([]statedomain.ResourceRelation, 0, len(resource.Relations))
		for _, relation := range resource.Relations {
			observation.Relations = append(observation.Relations, statedomain.ResourceRelation{
				Kind:   relation.Kind,
				Target: relation.Target,
				Label:  relation.Label,
			})
		}
	}

	out := make([]statedomain.ResourceObservation, 0, len(desired)+len(live))
	seen := make(map[string]struct{}, len(desired))
	for _, resource := range desired {
		if strings.TrimSpace(resource.LogicalID) == "" || strings.TrimSpace(resource.Type) == "" {
			continue
		}
		if strings.EqualFold(resource.Type, "AWS::CDK::Metadata") {
			continue
		}
		status, ok := statuses[resource.LogicalID]
		if !ok || status == statedomain.ResourceRemoved {
			status = statedomain.ResourceInSync
		}
		observation := statedomain.ResourceObservation{
			LogicalID: resource.LogicalID,
			CfnType:   resource.Type,
			Status:    status,
			Condition: resource.Condition,
		}
		copyTemplateEdges(&observation, resource)
		applyLive(&observation)
		out = append(out, observation)
		seen[resource.LogicalID] = struct{}{}
	}

	// A removed resource no longer exists in the desired template. Keep it in
	// the inventory so the UI can show what disappeared instead of only
	// flagging the root stack asset.
	for _, resource := range live {
		if _, ok := seen[resource.LogicalID]; ok {
			continue
		}
		if strings.EqualFold(resource.Type, "AWS::CDK::Metadata") {
			continue
		}
		if status, ok := statuses[resource.LogicalID]; !ok || status != statedomain.ResourceRemoved {
			continue
		}
		observation := statedomain.ResourceObservation{
			LogicalID: resource.LogicalID,
			CfnType:   resource.Type,
			Status:    statedomain.ResourceRemoved,
			Condition: resource.Condition,
		}
		copyTemplateEdges(&observation, resource)
		applyLive(&observation)
		out = append(out, observation)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].LogicalID < out[j].LogicalID
	})
	assignResourceAssetNames(out)
	return out
}

// assignResourceAssetNames gives every observation a stable, readable asset
// name. Names are assigned after sorting by logical ID so collisions resolve
// deterministically.
func assignResourceAssetNames(resources []statedomain.ResourceObservation) {
	used := make(map[string]int, len(resources))
	for i := range resources {
		base := truncateAssetName("res-" + observedgen.Slug(resources[i].LogicalID))
		name := base
		if count := used[base]; count > 0 {
			suffix := fmt.Sprintf("-%d", count+1)
			trimmed := strings.TrimRight(base, "-")
			if limit := maxResourceAssetName - len(suffix); len(trimmed) > limit {
				trimmed = strings.TrimRight(trimmed[:limit], "-")
			}
			name = trimmed + suffix
		}
		used[base]++
		resources[i].AssetName = name
	}
}

func truncateAssetName(name string) string {
	if len(name) <= maxResourceAssetName {
		return name
	}
	return strings.TrimRight(name[:maxResourceAssetName], "-")
}

func parseChangedResources(changed []string) map[string]string {
	out := make(map[string]string, len(changed))
	for _, entry := range changed {
		kind, logicalID, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok {
			continue
		}
		logicalID = strings.TrimSpace(logicalID)
		if logicalID == "" {
			continue
		}
		switch kind {
		case "changed":
			out[logicalID] = statedomain.ResourceChanged
		case "added":
			out[logicalID] = statedomain.ResourceAdded
		case "removed":
			out[logicalID] = statedomain.ResourceRemoved
		}
	}
	return out
}

// mergeResourceObservations combines inventories from every CDKStack asset in
// an intent. Stack == intent is the normal model, but older bundles may carry
// more than one stack asset; in that case prefix names with the stack asset to
// keep them unique.
func mergeResourceObservations(results []assetResult) []statedomain.ResourceObservation {
	if len(results) == 1 {
		return statedomain.CloneResourceObservations(results[0].snapshot.Resources)
	}
	out := make([]statedomain.ResourceObservation, 0)
	for _, result := range results {
		prefix := result.asset + "-"
		for _, resource := range result.snapshot.Resources {
			copied := resource
			copied.DependsOn = append([]string(nil), resource.DependsOn...)
			copied.Relations = append([]statedomain.ResourceRelation(nil), resource.Relations...)
			copied.AssetName = truncateAssetName(prefix + resource.AssetName)
			out = append(out, copied)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AssetName == out[j].AssetName {
			return out[i].LogicalID < out[j].LogicalID
		}
		return out[i].AssetName < out[j].AssetName
	})
	return out
}
