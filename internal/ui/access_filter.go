package ui

import (
	"context"
	"strings"

	"github.com/radryc/guardian/internal/observability"
	"github.com/radryc/guardian/internal/pusher/probes"
	"github.com/radryc/guardian/internal/registry"
	setstore "github.com/radryc/guardian/internal/sets"
	"github.com/radryc/guardian/internal/unmanaged"
	coreauthz "github.com/radryc/monofs/pkg/authz"
)

// readFilterActive reports whether partition-scoped reads must be filtered for
// this caller. It is false when the access policy is disabled or in observe
// mode (CanSee already returns true in those cases), for trusted machine
// callers, and for admins.
func (s *Server) readFilterActive(ctx context.Context) bool {
	if s == nil || s.access == nil || !s.access.Enforcing() || isM2MTrusted(ctx) {
		return false
	}
	return !s.access.IsAdmin(ctx)
}

// visibleRepoBindings keeps the bindings whose partition the caller may see.
func (s *Server) visibleRepoBindings(ctx context.Context, repo registry.Repo) []registry.RepoBinding {
	bindings := repo.BindingList()
	if !s.readFilterActive(ctx) {
		return bindings
	}
	out := make([]registry.RepoBinding, 0, len(bindings))
	for _, binding := range bindings {
		if s.canSeePartition(ctx, strings.TrimSpace(binding.Partition)) {
			out = append(out, binding)
		}
	}
	return out
}

// filterRepos hides repositories that only feed invisible partitions and trims
// multi-partition bindings down to the ones the caller may see.
func (s *Server) filterRepos(ctx context.Context, repos []registry.Repo) []registry.Repo {
	if !s.readFilterActive(ctx) {
		return repos
	}
	out := make([]registry.Repo, 0, len(repos))
	for _, repo := range repos {
		visible := s.visibleRepoBindings(ctx, repo)
		if len(visible) == 0 {
			continue
		}
		repo.Bindings = visible
		repo.Partition = visible[0].Partition
		repo.Path = visible[0].Path
		out = append(out, repo)
	}
	return out
}

// pusherAccess decides which registered pushers a caller may see. Pushers that
// are not referenced by any repository are treated as global (for example the
// k8s/gcp fleet pushers) and stay visible. A pusher referenced only by hidden
// repositories is hidden.
type pusherAccess struct {
	restricted bool
	known      map[string]bool
	visible    map[string]bool
}

func (s *Server) pusherAccessFor(ctx context.Context) (*pusherAccess, error) {
	pa := &pusherAccess{}
	if !s.readFilterActive(ctx) {
		return pa, nil
	}
	pa.restricted = true
	pa.known = map[string]bool{}
	pa.visible = map[string]bool{}
	repos, err := registry.New(s.store).ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	for _, repo := range repos {
		name := strings.TrimSpace(repo.Pusher)
		if name == "" {
			continue
		}
		pa.known[name] = true
		if len(s.visibleRepoBindings(ctx, repo)) > 0 {
			pa.visible[name] = true
		}
	}
	return pa, nil
}

func (pa *pusherAccess) allows(name string) bool {
	if pa == nil || !pa.restricted {
		return true
	}
	name = strings.TrimSpace(name)
	if !pa.known[name] {
		return true
	}
	return pa.visible[name]
}

// mayViewPusherTokens reports whether the caller may see raw pusher bearer
// tokens. Tokens are only exposed to authenticated admins: non-admin callers,
// anonymous callers, trusted machine callers, and every caller while the
// access policy is disabled or unloaded get redacted values.
func (s *Server) mayViewPusherTokens(ctx context.Context) bool {
	if s == nil || s.access == nil {
		return false
	}
	identity, ok := coreauthz.IdentityFromContext(ctx)
	if !ok || identity.IsAnonymous() {
		return false
	}
	return s.access.IsAdmin(ctx)
}

// filterScanPushers narrows the configured scan pushers to the ones the caller
// may see.
func (s *Server) filterScanPushers(pushers []string, pa *pusherAccess) []string {
	if pa == nil || !pa.restricted {
		return pushers
	}
	out := make([]string, 0, len(pushers))
	for _, name := range pushers {
		if pa.allows(name) {
			out = append(out, name)
		}
	}
	return out
}

// filterPushers removes pushers used exclusively by invisible partitions and
// redacts their bearer tokens unless the caller is an authenticated admin.
// Redaction applies regardless of whether the access policy is enforcing, so a
// disabled/observe deployment no longer discloses tokens to ordinary callers.
func (s *Server) filterPushers(ctx context.Context, pushers []registry.Pusher, pa *pusherAccess) []registry.Pusher {
	restricted := pa != nil && pa.restricted
	redact := !s.mayViewPusherTokens(ctx)
	if !restricted && !redact {
		return pushers
	}
	out := make([]registry.Pusher, 0, len(pushers))
	for _, pusher := range pushers {
		if restricted && !pa.allows(pusher.Name) {
			continue
		}
		if redact {
			pusher.Token = ""
		}
		out = append(out, pusher)
	}
	return out
}

// filterProbes keeps synthetic probes for partitions the caller may see.
func (s *Server) filterProbes(ctx context.Context, results []probes.Result) []probes.Result {
	if !s.readFilterActive(ctx) {
		return results
	}
	out := make([]probes.Result, 0, len(results))
	for _, result := range results {
		if s.canSeePartition(ctx, strings.TrimSpace(result.Partition)) {
			out = append(out, result)
		}
	}
	return out
}

// filterIdentities keeps telemetry identities for partitions the caller may see.
func (s *Server) filterIdentities(ctx context.Context, records []observability.IdentityRecord) []observability.IdentityRecord {
	if !s.readFilterActive(ctx) {
		return records
	}
	out := make([]observability.IdentityRecord, 0, len(records))
	for _, record := range records {
		if s.canSeePartition(ctx, strings.TrimSpace(record.Partition)) {
			out = append(out, record)
		}
	}
	return out
}

// visibleSetIDs marks sets that hold at least one visible partition, directly
// or through a descendant. Empty sets are dropped for restricted callers so
// their names/descriptions cannot describe hidden partitions.
func visibleSetIDs(all []setstore.Set, visible map[string]bool) map[string]bool {
	byID := make(map[string]setstore.Set, len(all))
	children := make(map[string][]string, len(all))
	for _, set := range all {
		byID[set.ID] = set
	}
	for _, set := range all {
		if set.ParentID == "" {
			continue
		}
		if _, ok := byID[set.ParentID]; ok {
			children[set.ParentID] = append(children[set.ParentID], set.ID)
		}
	}

	keep := make(map[string]bool, len(all))
	var visit func(id string, stack map[string]bool) bool
	visit = func(id string, stack map[string]bool) bool {
		if kept, ok := keep[id]; ok {
			return kept
		}
		if stack[id] {
			return false
		}
		stack[id] = true
		defer delete(stack, id)

		set, ok := byID[id]
		if !ok {
			return false
		}
		for _, partition := range set.Partitions {
			if visible[strings.TrimSpace(partition)] {
				keep[id] = true
				return true
			}
		}
		for _, child := range children[id] {
			if visit(child, stack) {
				keep[id] = true
				return true
			}
		}
		keep[id] = false
		return false
	}
	for id := range byID {
		visit(id, map[string]bool{})
	}
	return keep
}

// filterSetsForAccess drops sets with no visible partitions for restricted
// callers, preserving the hierarchy for sets that have a visible descendant.
func (s *Server) filterSetsForAccess(ctx context.Context, all []setstore.Set, visible map[string]bool) []setstore.Set {
	if !s.readFilterActive(ctx) {
		return all
	}
	keep := visibleSetIDs(all, visible)
	out := make([]setstore.Set, 0, len(all))
	for _, set := range all {
		if keep[set.ID] {
			out = append(out, set)
		}
	}
	return out
}

// filterUnmanagedResult removes stacks and warnings that reference partitions
// or pushers the caller may not see.
func (s *Server) filterUnmanagedResult(ctx context.Context, result *unmanaged.Result, pa *pusherAccess) *unmanaged.Result {
	if result == nil || !s.readFilterActive(ctx) {
		return result
	}
	hiddenPushers := map[string]bool{}
	if pa != nil {
		for name := range pa.known {
			if !pa.visible[name] {
				hiddenPushers[name] = true
			}
		}
	}
	out := &unmanaged.Result{Warnings: make([]string, 0, len(result.Warnings))}
	for _, stack := range result.Stacks {
		if stack.Partition != "" {
			if !s.canSeePartition(ctx, strings.TrimSpace(stack.Partition)) {
				continue
			}
		} else if !pa.allows(stack.Pusher) {
			continue
		}
		out.Stacks = append(out.Stacks, stack)
	}
	for _, warning := range result.Warnings {
		hidden := false
		for name := range hiddenPushers {
			if name != "" && strings.Contains(warning, name) {
				hidden = true
				break
			}
		}
		if !hidden {
			out.Warnings = append(out.Warnings, warning)
		}
	}
	return out
}
