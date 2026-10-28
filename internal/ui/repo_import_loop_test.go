package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func TestSyncRepoBundlesImportsChangedPartitions(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	registryStore := registry.New(store)
	if err := registryStore.PutPusher(ctx, &registry.Pusher{
		APIVersion: "guardian/v1alpha1",
		Kind:       "Pusher",
		Name:       "k8s-main",
		Provider:   "kubernetes",
	}); err != nil {
		t.Fatalf("put pusher: %v", err)
	}
	if err := registryStore.PutRepo(ctx, &registry.Repo{
		APIVersion: "guardian/v1alpha1",
		Kind:       "Repo",
		Name:       "demo",
		Source:     "https://github.com/acme/demo.git",
		Pusher:     "k8s-main",
		Bindings:   []registry.RepoBinding{{Partition: "demo"}},
	}); err != nil {
		t.Fatalf("put repo: %v", err)
	}

	root := "/.repos/github.com/acme/demo/guardian/partitions/demo"
	partitionYAML := `apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: manual
`
	intentYAML := `apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: web
spec:
  intentType: standard
  targetPusher: k8s-main
  assets: []
`
	writeTestFiles(t, ctx, store, map[string]string{
		root + "/config.yaml":      partitionYAML,
		root + "/intents/web.yaml": intentYAML,
	})

	srv := &Server{store: store, principalID: "test-ui"}
	srv.syncRepoBundles(ctx)

	configPath := "/partitions/demo/config.yaml"
	if _, err := store.ReadFile(ctx, configPath); err != nil {
		t.Fatalf("partition config not imported: %v", err)
	}
	if _, err := store.ReadFile(ctx, "/partitions/demo/intents/web.yaml"); err != nil {
		t.Fatalf("intent not imported: %v", err)
	}

	// A second scan with unchanged content must be a no-op (and not write).
	srv.syncRepoBundles(ctx)

	// Change the authored partition config and rescan.
	updated := strings.Replace(partitionYAML, "deletionPolicy: orphan", "deletionPolicy: destroy", 1)
	writeTestFiles(t, ctx, store, map[string]string{root + "/config.yaml": updated})
	srv.syncRepoBundles(ctx)

	raw, err := store.ReadFile(ctx, configPath)
	if err != nil {
		t.Fatalf("read imported config: %v", err)
	}
	if !strings.Contains(string(raw), "deletionPolicy: destroy") {
		t.Fatalf("imported config was not updated: %s", raw)
	}
}

func writeTestFiles(t *testing.T, ctx context.Context, store guardianapi.Store, files map[string]string) {
	t.Helper()
	writes := make([]guardianapi.PathWrite, 0, len(files))
	for path, content := range files {
		writes = append(writes, guardianapi.PathWrite{LogicalPath: path, Content: []byte(content)})
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  writes,
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "write test files"},
	}); err != nil {
		t.Fatalf("upsert files: %v", err)
	}
}
