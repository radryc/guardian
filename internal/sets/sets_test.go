package sets

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/radryc/guardian/internal/store/memory"
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

func TestRemovePartitionsClearsPrimary(t *testing.T) {
	ctx := context.Background()
	store := New(memory.New())
	core, err := store.Create(ctx, &Set{Name: "Core"})
	if err != nil {
		t.Fatalf("create Core: %v", err)
	}
	other, err := store.Create(ctx, &Set{Name: "Other"})
	if err != nil {
		t.Fatalf("create Other: %v", err)
	}
	if _, err := store.AddPartitions(ctx, core.ID, []string{"p1", "p2"}, nil, true); err != nil {
		t.Fatalf("add to Core: %v", err)
	}
	if _, err := store.AddPartitions(ctx, other.ID, []string{"p1", "p2"}, nil, false); err != nil {
		t.Fatalf("add to Other: %v", err)
	}

	updated, err := store.RemovePartitions(ctx, core.ID, []string{"p1"})
	if err != nil {
		t.Fatalf("RemovePartitions: %v", err)
	}
	if containsString(updated.Partitions, "p1") {
		t.Fatalf("partitions = %v, p1 should be gone", updated.Partitions)
	}
	if !containsString(updated.Partitions, "p2") {
		t.Fatalf("partitions = %v, p2 should remain", updated.Partitions)
	}
	if primary, err := store.PrimarySet(ctx, "p1"); err != nil || primary != "" {
		t.Fatalf("primary p1 = %q, err = %v, want cleared", primary, err)
	}
	if primary, err := store.PrimarySet(ctx, "p2"); err != nil || primary != core.ID {
		t.Fatalf("primary p2 = %q, err = %v, want %q", primary, err, core.ID)
	}

	// Removing a partition from a non-primary set must not clear the record.
	if _, err := store.RemovePartitions(ctx, other.ID, []string{"p2"}); err != nil {
		t.Fatalf("remove from Other: %v", err)
	}
	if primary, err := store.PrimarySet(ctx, "p2"); err != nil || primary != core.ID {
		t.Fatalf("primary p2 after Other removal = %q, err = %v, want %q", primary, err, core.ID)
	}

	// Removal is idempotent for partitions that are not members.
	if _, err := store.RemovePartitions(ctx, core.ID, []string{"missing"}); err != nil {
		t.Fatalf("RemovePartitions missing: %v", err)
	}
	if _, err := store.RemovePartitions(ctx, core.ID, []string{"p1"}); err != nil {
		t.Fatalf("RemovePartitions repeat: %v", err)
	}

	// The removal is sticky: config-driven membership must not re-add p1.
	if err := store.SyncPartitionConfig(ctx, "p1", []string{"Core"}, ""); err != nil {
		t.Fatalf("sync p1: %v", err)
	}
	got, err := store.Get(ctx, core.ID)
	if err != nil {
		t.Fatalf("get Core: %v", err)
	}
	if containsString(got.Partitions, "p1") {
		t.Fatalf("p1 was re-added despite exclusion: %v", got.Partitions)
	}
	if !containsString(got.ExcludedPartitions, "p1") {
		t.Fatalf("exclusions = %v, want p1", got.ExcludedPartitions)
	}

	// Manually assigning it again clears the exclusion.
	if _, err := store.AddPartitions(ctx, core.ID, []string{"p1"}, nil, false); err != nil {
		t.Fatalf("re-add p1: %v", err)
	}
	got, _ = store.Get(ctx, core.ID)
	if !containsString(got.Partitions, "p1") || containsString(got.ExcludedPartitions, "p1") {
		t.Fatalf("after re-add partitions=%v exclusions=%v", got.Partitions, got.ExcludedPartitions)
	}

	// Unassigning a non-member also records an exclusion so a later config
	// sync cannot introduce it.
	if _, err := store.RemovePartitions(ctx, core.ID, []string{"p3"}); err != nil {
		t.Fatalf("unassign non-member: %v", err)
	}
	if err := store.SyncPartitionConfig(ctx, "p3", []string{"Core"}, ""); err != nil {
		t.Fatalf("sync p3: %v", err)
	}
	got, _ = store.Get(ctx, core.ID)
	if containsString(got.Partitions, "p3") {
		t.Fatalf("p3 was re-added despite exclusion: %v", got.Partitions)
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
