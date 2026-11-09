package observed

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	statedomain "github.com/radryc/guardian/internal/domain/state"
	"github.com/radryc/guardian/internal/gcpobserve"
	"github.com/radryc/guardian/internal/k8sobserve"
	"github.com/radryc/guardian/internal/observedgen"
	"github.com/radryc/guardian/internal/orchestrator/common"
)

// resourceGroup batches intents that share one observed API scope so the
// collector fetches each provider inventory once per pass instead of once per
// intent (which was O(intents × inventory bytes)).
type resourceGroup struct {
	provider  string
	endpoint  Endpoint
	project   string
	region    string
	cluster   string
	namespace string
	intents   []*intentdomain.Intent
}

// collectResourceInventories publishes live resources for every standard
// intent, fetching each pusher inventory at most once. Kubernetes and GCP
// pushers expose provider inventories through their observed read APIs; the AWS
// observed path lives in collectIntent.
func (c *Collector) collectResourceInventories(ctx context.Context, partition string, intents []*intentdomain.Intent) {
	groups := map[string]*resourceGroup{}
	keys := make([]string, 0)
	for _, intent := range intents {
		endpoint, ok := c.endpointFor(intent.Spec.TargetPusher)
		if !ok || endpoint.Client == nil {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(endpoint.Provider))
		if provider != "kubernetes" && provider != "gcp" {
			continue
		}
		group := &resourceGroup{
			provider:  provider,
			endpoint:  endpoint,
			project:   strings.TrimSpace(intent.Spec.Target.Account),
			region:    strings.TrimSpace(intent.Spec.Target.Region),
			cluster:   strings.TrimSpace(intent.Spec.Target.Cluster),
			namespace: strings.TrimSpace(intent.Spec.Target.Namespace),
		}
		key := strings.Join([]string{group.provider, group.endpoint.Name, group.project, group.region, group.cluster, group.namespace}, "|")
		current, ok := groups[key]
		if !ok {
			current = group
			groups[key] = current
			keys = append(keys, key)
		}
		current.intents = append(current.intents, intent)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := groups[key]
		var err error
		switch group.provider {
		case "kubernetes":
			err = c.collectKubernetesInventory(ctx, partition, group)
		case "gcp":
			err = c.collectGCPInventory(ctx, partition, group)
		}
		if err != nil {
			log.Printf("[observed] %s: resource inventory (%s): %v", partition, group.provider, err)
		}
	}
}

func (c *Collector) collectKubernetesInventory(ctx context.Context, partition string, group *resourceGroup) error {
	stacks, err := group.endpoint.Client.K8sStacks(ctx, group.cluster, group.namespace, partition, "")
	if err != nil {
		return fmt.Errorf("kubernetes observed stacks: %w", err)
	}
	byStack := make(map[string]k8sobserve.Inventory, len(stacks))
	for _, stack := range stacks {
		byStack[strings.ToLower(strings.TrimSpace(stack.Partition+"/"+stack.Intent))] = stack
	}
	for _, intent := range group.intents {
		stack, ok := byStack[strings.ToLower(partition+"/"+intent.Metadata.Name)]
		if !ok {
			continue
		}
		state, err := c.loadResourceState(ctx, partition, intent)
		if err != nil {
			log.Printf("[observed] %s/%s: resource inventory: %v", partition, intent.Metadata.Name, err)
			continue
		}
		if err := c.persistResourceInventory(ctx, partition, intent, state, k8sResourceObservations(stack)); err != nil {
			log.Printf("[observed] %s/%s: resource inventory: %v", partition, intent.Metadata.Name, err)
		}
	}
	return nil
}

func (c *Collector) collectGCPInventory(ctx context.Context, partition string, group *resourceGroup) error {
	stacks, err := group.endpoint.Client.GCPStacks(ctx, group.project, group.region)
	if err != nil {
		return fmt.Errorf("gcp observed stacks: %w", err)
	}
	byStack := make(map[string]*gcpobserve.StackState, len(stacks))
	for i := range stacks {
		byStack[strings.ToLower(strings.TrimSpace(stacks[i].StackName))] = &stacks[i]
	}
	for _, intent := range group.intents {
		stack, ok := byStack[strings.ToLower(partition+"/"+intent.Metadata.Name)]
		if !ok {
			continue
		}
		state, err := c.loadResourceState(ctx, partition, intent)
		if err != nil {
			log.Printf("[observed] %s/%s: resource inventory: %v", partition, intent.Metadata.Name, err)
			continue
		}
		if err := c.persistResourceInventory(ctx, partition, intent, state, gcpResourceObservations(partition, *stack, intent, state)); err != nil {
			log.Printf("[observed] %s/%s: resource inventory: %v", partition, intent.Metadata.Name, err)
		}
	}
	return nil
}

func (c *Collector) loadResourceState(ctx context.Context, partition string, intent *intentdomain.Intent) (*statedomain.IntentState, error) {
	intentName := intent.Metadata.Name
	state, err := common.LoadIntentState(ctx, c.Store, partition, intentName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if state == nil {
		state = &statedomain.IntentState{
			APIVersion:   "guardian/v1alpha1",
			Kind:         "IntentState",
			Partition:    partition,
			Intent:       intentName,
			TargetPusher: intent.Spec.TargetPusher,
			Target:       intent.Spec.Target,
			Joins:        append([]string(nil), intent.Spec.Joins...),
			Outputs:      map[string]string{},
		}
	}
	if state.Outputs == nil {
		state.Outputs = map[string]string{}
	}
	return state, nil
}

func (c *Collector) persistResourceInventory(ctx context.Context, partition string, intent *intentdomain.Intent, state *statedomain.IntentState, resources []statedomain.ResourceObservation) error {
	// Merge against the latest persisted state instead of overwriting it with
	// the snapshot loaded before the provider call. The reconciler and result
	// processor may have advanced status or queued a task in the meantime.
	base := statedomain.CloneIntentState(state)
	state.Resources = statedomain.CloneResourceObservations(resources)
	state.Timestamps.LastObservedAt = time.Now().UTC()
	updated, err := c.Dispatcher.UpdateIntentState(ctx, partition, intent.Metadata.Name, func(latest *statedomain.IntentState) error {
		statedomain.MergeIntentState(latest, base, state)
		return nil
	})
	if err != nil {
		return err
	}
	log.Printf("[observed] %s/%s resources=%d provider=%s", partition, intent.Metadata.Name, len(updated.Resources), resourceProvider(updated.Resources))
	return nil
}

func resourceProvider(resources []statedomain.ResourceObservation) string {
	for _, resource := range resources {
		if resource.Provider != "" {
			return resource.Provider
		}
	}
	return ""
}

func k8sResourceObservations(inventory k8sobserve.Inventory) []statedomain.ResourceObservation {
	observedAt := inventory.GeneratedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	used := map[string]int{}
	out := make([]statedomain.ResourceObservation, 0, len(inventory.Resources))
	for _, resource := range inventory.Resources {
		kind := strings.TrimSpace(resource.Kind)
		name := strings.TrimSpace(resource.Name)
		if kind == "" || name == "" {
			continue
		}
		ready := strings.TrimSpace(resource.Ready)
		if ready == "" {
			ready = strings.TrimSpace(resource.Status)
		}
		logicalID := strings.Trim(strings.Join([]string{resource.Namespace, kind, name}, "/"), "/")
		seed := kind
		if asset := strings.TrimSpace(resource.Asset); asset != "" {
			seed += "-" + asset
		} else {
			seed += "-" + name
		}
		out = append(out, statedomain.ResourceObservation{
			LogicalID:     logicalID,
			CfnType:       kind,
			AssetName:     uniqueResourceAssetName("k8s-res-"+observedgen.Slug(seed), used),
			Name:          name,
			Status:        statedomain.ResourceInSync,
			Provider:      "kubernetes",
			Location:      strings.TrimSpace(resource.Namespace),
			PhysicalID:    logicalID,
			LiveStatus:    ready,
			LiveUpdatedAt: observedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LogicalID < out[j].LogicalID })
	return out
}

func gcpResourceObservations(partition string, stack gcpobserve.StackState, intent *intentdomain.Intent, state *statedomain.IntentState) []statedomain.ResourceObservation {
	observedAt := time.Now().UTC()
	used := map[string]int{}
	assets := make([]string, 0, len(intent.Spec.Assets))
	for _, asset := range intent.Spec.Assets {
		assets = append(assets, asset.Name)
	}
	sort.Strings(assets)

	// First pass: identify the durable resources owned by this intent.
	type candidate struct {
		resource gcpobserve.StackResource
		owned    bool
		revision bool
	}
	candidates := make([]candidate, 0, len(stack.Resources))
	for _, resource := range stack.Resources {
		revision := strings.Contains(strings.ToLower(resource.AssetType), "/revision")
		candidates = append(candidates, candidate{
			resource: resource,
			owned:    gcpResourceOwned(partition, intent.Metadata.Name, resource, assets, state.Outputs),
			revision: revision,
		})
	}
	// A revision belongs to the intent when its service sibling is owned.
	for i := range candidates {
		if !candidates[i].revision || candidates[i].owned {
			continue
		}
		for j := range candidates {
			if candidates[j].revision || !candidates[j].owned {
				continue
			}
			if gcpRevisionBelongsTo(candidates[i].resource, candidates[j].resource) {
				candidates[i].owned = true
				break
			}
		}
	}

	serviceByRevisionPrefix := map[string]string{}
	for _, item := range candidates {
		if item.revision || !item.owned {
			continue
		}
		serviceByRevisionPrefix[item.resource.DisplayName] = item.resource.Name
	}

	out := make([]statedomain.ResourceObservation, 0, len(candidates))
	for _, item := range candidates {
		if !item.owned {
			continue
		}
		resource := item.resource
		display := strings.TrimSpace(resource.DisplayName)
		if display == "" {
			display = lastResourceSegment(resource.Name)
		}
		updated := resource.UpdateTime
		if item.revision && !resource.CreateTime.IsZero() {
			updated = resource.CreateTime
		}
		if item.revision && updated.IsZero() {
			// Older pushers do not include createTime. Cloud Run revision names
			// carry a monotonically increasing numeric suffix, so use it as a
			// deterministic recency tie-breaker.
			updated = time.Unix(0, gcpRevisionOrdinal(display)+1)
		}
		if updated.IsZero() {
			updated = observedAt
		}
		status := statedomain.ResourceInSync
		if resource.State != "" && !strings.EqualFold(resource.State, "ACTIVE") {
			status = statedomain.ResourceChanged
		}
		observation := statedomain.ResourceObservation{
			LogicalID:     resource.Name,
			CfnType:       resource.AssetType,
			AssetName:     uniqueResourceAssetName("gcp-res-"+observedgen.Slug(display), used),
			Name:          display,
			Status:        status,
			Provider:      "gcp",
			Location:      strings.TrimSpace(resource.Location),
			PhysicalID:    resource.Name,
			LiveStatus:    strings.TrimSpace(resource.State),
			LiveUpdatedAt: updated,
			Ephemeral:     item.revision,
		}
		if item.revision {
			observation.Parent = gcpServiceForRevision(resource, serviceByRevisionPrefix)
		}
		out = append(out, observation)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CfnType != out[j].CfnType {
			return out[i].CfnType < out[j].CfnType
		}
		return out[i].LogicalID < out[j].LogicalID
	})
	return out
}

func gcpResourceOwned(partition, intent string, resource gcpobserve.StackResource, assets []string, outputs map[string]string) bool {
	candidates := []string{resource.Name, resource.DisplayName}
	prefixes := []string{
		"guardian-" + partition + "-" + intent + "-",
		"guardian-" + gcpNameSegment(partition) + "-" + gcpNameSegment(intent) + "-",
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		for _, prefix := range prefixes {
			if !strings.HasPrefix(candidate, prefix) {
				continue
			}
			remainder := strings.TrimPrefix(candidate, prefix)
			for _, asset := range assets {
				for _, assetName := range []string{asset, gcpNameSegment(asset)} {
					if remainder == assetName || strings.HasPrefix(remainder, assetName+"-") {
						return true
					}
				}
			}
		}
	}
	// Adopted resources keep their original names; match the asset output.
	for _, asset := range assets {
		output := strings.TrimSpace(outputs[asset+".service"])
		if output == "" {
			continue
		}
		for _, candidate := range candidates {
			if strings.EqualFold(lastResourceSegment(candidate), output) {
				return true
			}
		}
	}
	return false
}

func gcpRevisionBelongsTo(revision, service gcpobserve.StackResource) bool {
	revisionName := strings.TrimSpace(revision.DisplayName)
	serviceName := strings.TrimSpace(service.DisplayName)
	if revisionName == "" || serviceName == "" {
		return false
	}
	return strings.HasPrefix(revisionName, serviceName+"-")
}

func gcpServiceForRevision(revision gcpobserve.StackResource, services map[string]string) string {
	name := strings.TrimSpace(revision.DisplayName)
	if name == "" {
		return ""
	}
	best := ""
	bestID := ""
	for service, logicalID := range services {
		if service == "" || !strings.HasPrefix(name, service+"-") {
			continue
		}
		if len(service) > len(best) {
			best = service
			bestID = logicalID
		}
	}
	return bestID
}

func lastResourceSegment(name string) string {
	name = strings.TrimRight(strings.TrimSpace(name), "/")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// gcpRevisionOrdinal extracts the numeric revision suffix from a Cloud Run
// revision name such as service-00002-abc.
func gcpRevisionOrdinal(name string) int64 {
	parts := strings.Split(strings.TrimSpace(name), "-")
	for i := len(parts) - 1; i >= 0; i-- {
		// Cloud Run revision names use a five-digit ordinal followed by a short
		// hash. Only accept the ordinal; a three-character hash can be numeric
		// too (for example ...-00002-876).
		if len(parts[i]) < 5 {
			continue
		}
		digits := true
		for _, r := range parts[i] {
			if r < '0' || r > '9' {
				digits = false
				break
			}
		}
		if !digits {
			continue
		}
		var value int64
		for _, r := range parts[i] {
			value = value*10 + int64(r-'0')
		}
		return value
	}
	return 0
}

func uniqueResourceAssetName(base string, used map[string]int) string {
	base = truncateResourceAssetName(base)
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
	return name
}

func truncateResourceAssetName(name string) string {
	if len(name) <= maxResourceAssetName {
		return name
	}
	return strings.TrimRight(name[:maxResourceAssetName], "-")
}

var gcpNameSanitizer = strings.NewReplacer("_", "-", ".", "-")

func gcpNameSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = gcpNameSanitizer.Replace(value)
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
