package sets

import (
	"context"
	"errors"
	"os"
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

func TestFindByName(t *testing.T) {
	ctx := context.Background()
	store := New(memory.New())
	created, err := store.Create(ctx, &Set{Name: "Core"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	found, err := store.FindByName(ctx, "Core")
	if err != nil || found.ID != created.ID {
		t.Fatalf("FindByName = %+v, err = %v", found, err)
	}
	if _, err := store.FindByName(ctx, "missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing err = %v, want os.ErrNotExist", err)
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

func TestSyncPartitionConfigAddsMembershipAndCreatesMissingSet(t *testing.T) {
	ctx := context.Background()
	store := New(memory.New())
	core, err := store.Create(ctx, &Set{Name: "Core"})
	if err != nil {
		t.Fatalf("create Core: %v", err)
	}
	manual, err := store.Create(ctx, &Set{Name: "Manual"})
	if err != nil {
		t.Fatalf("create Manual: %v", err)
	}
	if _, err := store.AddPartitions(ctx, manual.ID, []string{"agent"}, nil, false); err != nil {
		t.Fatalf("manual membership: %v", err)
	}

	// "Department X" is not pre-created: the partition config defines it.
	if err := store.SyncPartitionConfig(ctx, "agent", []string{"Core", "Department X"}, "Core"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	gotCore, _ := store.Get(ctx, core.ID)
	if !containsString(gotCore.Partitions, "agent") {
		t.Fatalf("Core partitions = %v, want agent", gotCore.Partitions)
	}
	gotManual, _ := store.Get(ctx, manual.ID)
	if !containsString(gotManual.Partitions, "agent") {
		t.Fatalf("manual membership was not preserved: %v", gotManual.Partitions)
	}
	department, err := store.FindByName(ctx, "Department X")
	if err != nil {
		t.Fatalf("Department X not created: %v", err)
	}
	if !containsString(department.Partitions, "agent") {
		t.Fatalf("Department X partitions = %v, want agent", department.Partitions)
	}
	primary, err := store.PrimarySet(ctx, "agent")
	if err != nil || primary != core.ID {
		t.Fatalf("primary = %q, err = %v, want %q", primary, err, core.ID)
	}

	// Sync is idempotent.
	if err := store.SyncPartitionConfig(ctx, "agent", []string{"Core", "Department X"}, "Core"); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	gotCore, _ = store.Get(ctx, core.ID)
	if len(gotCore.Partitions) != 1 {
		t.Fatalf("Core duplicated membership: %v", gotCore.Partitions)
	}
}
