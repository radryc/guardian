// Package authz provides Guardian control-plane authorization: enforcing the
// maintainer role before a partition is modified/deployed/reconciled, and
// scoping the reconciler to the partitions it manages (authz epic E).
//
// It reuses the shared identity/grant primitives from MonoFS
// (github.com/radryc/monofs/pkg/authz), imported here as coreauthz.
package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	coreauthz "github.com/radryc/monofs/pkg/authz"
)

// Guard enforces control-plane authorization on partition mutations.
type Guard struct {
	eval    coreauthz.GrantEvaluator
	enforce bool
}

// NewGuard builds a control-plane guard. When enforce is false or eval is nil,
// authorization is a no-op (observe mode).
func NewGuard(eval coreauthz.GrantEvaluator, enforce bool) *Guard {
	return &Guard{eval: eval, enforce: enforce}
}

// AuthorizeModify requires the maintainer (or admin) role on the partition for
// deploy/reconcile/modify operations. Identity is read from ctx.
func (g *Guard) AuthorizeModify(ctx context.Context, partition string) error {
	if g == nil || !g.enforce || g.eval == nil {
		return nil
	}
	if strings.TrimSpace(partition) == "" {
		return fmt.Errorf("authz: partition is required for control-plane authorization")
	}
	id, _ := coreauthz.IdentityFromContext(ctx)
	if g.eval.Can(ctx, id, partition, coreauthz.ActionModify) {
		return nil
	}
	return fmt.Errorf("principal %q is not authorized to modify partition %q", id.PrincipalID(), partition)
}

// ServicePrincipal is a machine identity (e.g. the reconciler) scoped to the
// partitions it manages (authz epic E3).
type ServicePrincipal struct {
	Identity   coreauthz.Identity
	partitions map[string]bool
}

// NewServicePrincipal builds a service principal with the given client id that
// manages exactly the listed partitions.
func NewServicePrincipal(clientID string, partitions []string) *ServicePrincipal {
	set := make(map[string]bool, len(partitions))
	for _, p := range partitions {
		if p = strings.TrimSpace(p); p != "" {
			set[p] = true
		}
	}
	return &ServicePrincipal{
		Identity:   coreauthz.Identity{ClientID: clientID},
		partitions: set,
	}
}

// Manages reports whether the service principal is responsible for a partition.
func (sp *ServicePrincipal) Manages(partition string) bool {
	return sp != nil && sp.partitions[partition]
}

// ScopedEvaluator returns a GrantEvaluator that permits actions only on the
// partitions this service principal manages, delegating the actual grant check
// to inner. This ensures the reconciler cannot act outside its partition set.
func (sp *ServicePrincipal) ScopedEvaluator(inner coreauthz.GrantEvaluator) coreauthz.GrantEvaluator {
	return scopedEvaluator{inner: inner, partitions: sp.partitions}
}

type scopedEvaluator struct {
	inner      coreauthz.GrantEvaluator
	partitions map[string]bool
}

func (s scopedEvaluator) Can(ctx context.Context, id coreauthz.Identity, partition string, action coreauthz.Action) bool {
	if !s.partitions[partition] {
		return false
	}
	if s.inner == nil {
		return false
	}
	return s.inner.Can(ctx, id, partition, action)
}

// LoadGuard builds a Guard from a grant store JSON file (empty path = in-memory
// empty store). It is a convenience for guardiand/guardianctl wiring.
func LoadGuard(grantsPath string, enforce bool) (*Guard, error) {
	store, err := coreauthz.NewGrantStore(grantsPath)
	if err != nil {
		return nil, fmt.Errorf("authz: load control-plane grants: %w", err)
	}
	return NewGuard(store, enforce), nil
}

// LoadGuardWithInline builds a Guard from an optional grants file plus an
// optional inline JSON array of grants (env-driven deploys). Inline grants are
// merged on top of the file. It is used to wire guardiand from environment.
func LoadGuardWithInline(grantsPath, inlineJSON string, enforce bool) (*Guard, error) {
	store, err := coreauthz.NewGrantStore(grantsPath)
	if err != nil {
		return nil, fmt.Errorf("authz: load control-plane grants: %w", err)
	}
	if raw := strings.TrimSpace(inlineJSON); raw != "" {
		var inline []coreauthz.Grant
		if err := json.Unmarshal([]byte(raw), &inline); err != nil {
			return nil, fmt.Errorf("authz: parse inline grants JSON: %w", err)
		}
		for _, g := range inline {
			if err := store.Add(g); err != nil {
				return nil, fmt.Errorf("authz: add inline grant: %w", err)
			}
		}
	}
	return NewGuard(store, enforce), nil
}
