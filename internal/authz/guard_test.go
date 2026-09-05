package authz

import (
	"context"
	"testing"

	coreauthz "github.com/radryc/monofs/pkg/authz"
)

func newEvalWith(grants ...coreauthz.Grant) coreauthz.GrantEvaluator {
	store, _ := coreauthz.NewGrantStore("")
	for _, g := range grants {
		_ = store.Add(g)
	}
	return store
}

func TestGuardAuthorizeModify(t *testing.T) {
	eval := newEvalWith(
		coreauthz.Grant{Subject: "alice", Partition: "doctor", Role: coreauthz.RoleMaintainer},
	)
	g := NewGuard(eval, true)
	ctx := coreauthz.ContextWithIdentity(context.Background(), coreauthz.Identity{Subject: "alice"})

	if err := g.AuthorizeModify(ctx, "doctor"); err != nil {
		t.Fatalf("maintainer should modify doctor: %v", err)
	}
	if err := g.AuthorizeModify(ctx, "monitoring"); err == nil {
		t.Fatal("alice should not modify monitoring")
	}
	// Anonymous denied.
	if err := g.AuthorizeModify(context.Background(), "doctor"); err == nil {
		t.Fatal("anonymous should be denied")
	}
	// Empty partition errors.
	if err := g.AuthorizeModify(ctx, ""); err == nil {
		t.Fatal("empty partition should error")
	}
}

func TestGuardObserveModeIsNoop(t *testing.T) {
	g := NewGuard(newEvalWith(), false) // enforce=false
	if err := g.AuthorizeModify(context.Background(), "doctor"); err != nil {
		t.Fatalf("observe mode should not deny: %v", err)
	}
	// nil guard is also a no-op.
	var ng *Guard
	if err := ng.AuthorizeModify(context.Background(), "doctor"); err != nil {
		t.Fatalf("nil guard should be a no-op: %v", err)
	}
}

func TestServicePrincipalScoping(t *testing.T) {
	// Inner evaluator would grant admin everywhere...
	inner := newEvalWith(
		coreauthz.Grant{Subject: "reconciler", Partition: coreauthz.WildcardPartition, Role: coreauthz.RoleAdmin},
	)
	sp := NewServicePrincipal("reconciler", []string{"doctor", "monitoring"})

	if !sp.Manages("doctor") || sp.Manages("k8s-top") {
		t.Fatal("Manages scoping incorrect")
	}

	scoped := sp.ScopedEvaluator(inner)
	ctx := context.Background()
	id := coreauthz.Identity{ClientID: "reconciler"}

	// Managed partitions: allowed (inner grants admin).
	if !scoped.Can(ctx, id, "doctor", coreauthz.ActionModify) {
		t.Error("reconciler should modify managed partition doctor")
	}
	// Unmanaged partition: denied even though inner would allow.
	if scoped.Can(ctx, id, "k8s-top", coreauthz.ActionModify) {
		t.Error("reconciler must NOT act outside its managed partition set")
	}
}

func TestLoadGuard(t *testing.T) {
	g, err := LoadGuard("", true)
	if err != nil {
		t.Fatalf("LoadGuard: %v", err)
	}
	// Empty store -> everything denied under enforcement.
	ctx := coreauthz.ContextWithIdentity(context.Background(), coreauthz.Identity{Subject: "x"})
	if err := g.AuthorizeModify(ctx, "doctor"); err == nil {
		t.Fatal("empty grant store should deny under enforcement")
	}
}
