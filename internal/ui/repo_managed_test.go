package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/registry"
	fsstore "github.com/radryc/guardian/internal/store/fs"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func seedRepoBinding(t *testing.T, store guardianapi.Store, name, partition string) {
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

func TestSaveBundleRefusesRepoManagedPartition(t *testing.T) {
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seedRepoBinding(t, store, "stratatools", "doctor")
	srv := &Server{store: store}

	managed, repo, err := srv.partitionRepoManaged(context.Background(), "doctor")
	if err != nil {
		t.Fatal(err)
	}
	if !managed || repo != "stratatools" {
		t.Fatalf("managed=%v repo=%q", managed, repo)
	}

	if _, err := srv.saveBundle(context.Background(), "doctor", &SaveBundleRequest{}); err == nil {
		t.Fatal("expected saveBundle to refuse a repo-managed partition")
	}

	t.Setenv("GUARDIAN_ALLOW_REPO_MANAGED_WRITES", "true")
	if _, err := srv.saveBundle(context.Background(), "doctor", &SaveBundleRequest{}); err != nil {
		if strings.Contains(err.Error(), "managed by repository") {
			t.Fatalf("override did not bypass the guard: %v", err)
		}
	}
}
