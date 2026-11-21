package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/registry"
	fsstore "github.com/radryc/guardian/internal/store/fs"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func writeRepoBinding(t *testing.T, store guardianapi.Store, name, partition string) {
	t.Helper()
	repo := registry.Repo{
		APIVersion: "guardian/v1alpha1",
		Kind:       "Repo",
		Name:       name,
		Bindings:   []registry.RepoBinding{{Partition: partition}},
	}
	data, err := json.Marshal(repo)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpsertFiles(context.Background(), guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: paths.RepoRecord(name), Content: data}},
		Context: guardianapi.MutationContext{
			PrincipalID: "test", Reason: "seed repo",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGuardPartitionWrite(t *testing.T) {
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeRepoBinding(t, store, "stratatools", "doctor")

	managed, repo, err := repoManagedPartition(context.Background(), store, "doctor")
	if err != nil {
		t.Fatal(err)
	}
	if !managed || repo != "stratatools" {
		t.Fatalf("managed=%v repo=%q", managed, repo)
	}

	if err := guardPartitionWrite(context.Background(), store, "doctor", false); err == nil {
		t.Fatal("expected a refusal without --break-glass")
	}
	if err := guardPartitionWrite(context.Background(), store, "doctor", true); err != nil {
		t.Fatalf("break-glass write: %v", err)
	}

	if managed, _, err := repoManagedPartition(context.Background(), store, "unbound"); err != nil || managed {
		t.Fatalf("unbound managed=%v err=%v", managed, err)
	}
}
