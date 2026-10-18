package sets

import (
	"context"
	"errors"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/store/memory"
)

func TestCreateNestedSetsAndMembership(t *testing.T) {
	ctx := context.Background()
	store := New(memory.New())
	parent, err := store.Create(ctx, &Set{Name: "Department X — AWS Production", Description: "prod"})
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	if parent.ID != "department-x-aws-production" {
		t.Fatalf("id = %q", parent.ID)
	}
	child, err := store.Create(ctx, &Set{Name: "Payments", ParentID: parent.ID})
	if err != nil {
		t.Fatalf("Create child: %v", err)
	}
	updated, err := store.AddPartitions(ctx, child.ID, []string{"demo-deploy", "demo-build"}, nil, true)
	if err != nil {
		t.Fatalf("AddPartitions: %v", err)
	}
	if len(updated.Partitions) != 2 {
		t.Fatalf("partitions = %v", updated.Partitions)
	}
	primary, err := store.PrimarySet(ctx, "demo-deploy")
	if err != nil || primary != child.ID {
		t.Fatalf("primary = %q, err = %v", primary, err)
	}
	if err := store.Delete(ctx, parent.ID); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("Delete parent err = %v, want ErrNotEmpty", err)
	}
	if err := store.Delete(ctx, child.ID); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("Delete child err = %v, want ErrNotEmpty", err)
	}
	if _, err := store.AddPartitions(ctx, child.ID, nil, []string{"demo-deploy", "demo-build"}, false); err != nil {
		t.Fatalf("remove partitions: %v", err)
	}
	if err := store.Delete(ctx, child.ID); err != nil {
		t.Fatalf("Delete empty child: %v", err)
	}
}

func TestMoveRejectsCycles(t *testing.T) {
	ctx := context.Background()
	store := New(memory.New())
	a, _ := store.Create(ctx, &Set{Name: "A"})
	b, _ := store.Create(ctx, &Set{Name: "B", ParentID: a.ID})
	if _, err := store.Move(ctx, a.ID, b.ID); !errors.Is(err, ErrCycle) {
		t.Fatalf("Move err = %v, want ErrCycle", err)
	}
}
