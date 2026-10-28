package main

import (
	"context"
	"io"
	"testing"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/paths"
	setstore "github.com/radryc/guardian/internal/sets"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func TestSetsEnsureIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	_, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{
			{LogicalPath: paths.PartitionConfig("monofs"), Content: []byte("apiVersion: guardian/v1alpha1\nkind: Partition\nmetadata:\n  name: monofs\n")},
			{LogicalPath: paths.PartitionConfig("demo-deploy"), Content: []byte("apiVersion: guardian/v1alpha1\nkind: Partition\nmetadata:\n  name: demo-deploy\n")},
		},
		Context: guardianapi.MutationContext{PrincipalID: "test"},
	})
	if err != nil {
		t.Fatalf("seed partitions: %v", err)
	}
	printer := &output.Printer{Writer: io.Discard}
	cmd := setsEnsureCommand(store, printer)
	args := []string{"--name", "Core", "--description", "control plane", "--label", "scope=core", "--partition", "monofs", "--partition", "demo-deploy", "--partition", "missing"}
	if err := cmd.Flags.Parse(args); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// A second run must update the same set, not create a duplicate.
	if err := cmd.Flags.Parse(args); err != nil {
		t.Fatalf("parse second: %v", err)
	}
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("ensure second: %v", err)
	}
	sets, err := setstore.New(store).List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("sets = %+v, want 1", sets)
	}
	if sets[0].Name != "Core" || sets[0].Description != "control plane" {
		t.Fatalf("set = %+v", sets[0])
	}
	if len(sets[0].Partitions) != 2 {
		t.Fatalf("partitions = %v, want monofs and demo-deploy", sets[0].Partitions)
	}
	if sets[0].Labels["scope"] != "core" {
		t.Fatalf("labels = %v", sets[0].Labels)
	}
	// Missing partitions are planning metadata: --allow-missing records them
	// now so the partition is grouped as soon as its config appears.
	allowArgs := append(append([]string{}, args...), "--allow-missing")
	if err := cmd.Flags.Parse(allowArgs); err != nil {
		t.Fatalf("parse allow-missing: %v", err)
	}
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("ensure allow-missing: %v", err)
	}
	sets, err = setstore.New(store).List(ctx)
	if err != nil {
		t.Fatalf("list after allow-missing: %v", err)
	}
	found := false
	for _, partition := range sets[0].Partitions {
		if partition == "missing" {
			found = true
		}
	}
	if !found || len(sets[0].Partitions) != 3 {
		t.Fatalf("partitions after allow-missing = %v, want missing recorded", sets[0].Partitions)
	}
}

func TestSetsCLILifecycle(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{
			{LogicalPath: paths.PartitionConfig("p1"), Content: []byte("apiVersion: guardian/v1alpha1\nkind: Partition\nmetadata:\n  name: p1\n")},
			{LogicalPath: paths.PartitionConfig("p2"), Content: []byte("apiVersion: guardian/v1alpha1\nkind: Partition\nmetadata:\n  name: p2\n")},
		},
		Context: guardianapi.MutationContext{PrincipalID: "test"},
	}); err != nil {
		t.Fatalf("seed partitions: %v", err)
	}
	storeSets := setstore.New(store)
	parent, err := storeSets.Create(ctx, &setstore.Set{Name: "Parent"})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := storeSets.Create(ctx, &setstore.Set{Name: "Child", ParentID: parent.ID, Labels: map[string]string{"team": "platform", "env": "demo"}})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	printer := &output.Printer{Writer: io.Discard}
	run := func(cmd *command.Command, args []string) error {
		if err := cmd.Flags.Parse(args); err != nil {
			t.Fatalf("parse %s: %v", cmd.Description, err)
		}
		return cmd.Run(ctx, cmd.Flags.Args())
	}
	if err := run(setsAssignCommand(store, printer), []string{"--set", child.ID, "--add", "p1", "--add", "p2"}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := run(setsUpdateCommand(store, printer), []string{"--set", child.ID, "--description", "updated", "--pinned", "true"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := storeSets.Get(ctx, child.ID)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if got.Description != "updated" || !got.Pinned || len(got.Partitions) != 2 {
		t.Fatalf("child after update = %+v", got)
	}
	if err := run(setsUpdateCommand(store, printer), []string{"--set", child.ID, "--remove-label", "team"}); err != nil {
		t.Fatalf("remove label: %v", err)
	}
	got, _ = storeSets.Get(ctx, child.ID)
	if _, ok := got.Labels["team"]; ok || got.Labels["env"] != "demo" {
		t.Fatalf("labels after remove = %+v", got.Labels)
	}
	if err := run(setsUpdateCommand(store, printer), []string{"--set", child.ID, "--clear-labels", "--clear-description"}); err != nil {
		t.Fatalf("clear metadata: %v", err)
	}
	got, _ = storeSets.Get(ctx, child.ID)
	if len(got.Labels) != 0 || got.Description != "" {
		t.Fatalf("metadata after clear = %+v", got)
	}
	if err := run(setsDeleteCommand(store, printer), []string{"--set", child.ID}); err == nil {
		t.Fatal("delete non-empty set should fail")
	}
	// Move by parent name, not only by id.
	if err := run(setsMoveCommand(store, printer), []string{"--set", child.ID, "--parent", "Parent"}); err != nil {
		t.Fatalf("move by name: %v", err)
	}
	got, _ = storeSets.Get(ctx, child.ID)
	if got.ParentID != parent.ID {
		t.Fatalf("parent = %q, want %q", got.ParentID, parent.ID)
	}
	if err := run(setsMoveCommand(store, printer), []string{"--set", child.ID, "--parent", ""}); err != nil {
		t.Fatalf("move to root: %v", err)
	}
	got, _ = storeSets.Get(ctx, child.ID)
	if got.ParentID != "" {
		t.Fatalf("parent = %q, want root", got.ParentID)
	}
	if err := run(setsAssignCommand(store, printer), []string{"--set", child.ID, "--remove", "p1", "--remove", "p2"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := run(setsDeleteCommand(store, printer), []string{"--set", child.ID}); err != nil {
		t.Fatalf("delete empty: %v", err)
	}
}
