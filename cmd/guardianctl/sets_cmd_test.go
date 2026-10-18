package main

import (
	"context"
	"io"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	setstore "github.com/rydzu/ainfra/guardian/internal/sets"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
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
}
