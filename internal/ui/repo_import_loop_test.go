package ui

import (
	"context"
	"testing"

	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
)

const discoveryPartitionYAML = `apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: prod
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 1m
`

const discoveryIntentYAML = `apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  targetPusher: k8s-main
  assets:
    - type: Compute
      name: api
      properties:
        image: nginx
`

func seedRepoAuthoredPartition(t *testing.T, store guardianapi.Store, root string) {
	t.Helper()
	ctx := context.Background()
	files := map[string]string{
		root + "/guardian/partitions/prod/config.yaml":      discoveryPartitionYAML,
		root + "/guardian/partitions/prod/intents/api.yaml": discoveryIntentYAML,
	}
	writes := make([]guardianapi.PathWrite, 0, len(files))
	for path, content := range files {
		writes = append(writes, guardianapi.PathWrite{LogicalPath: path, Content: []byte(content)})
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{Writes: writes}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
}

func TestRepoImportDiscoversIngestedRepo(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	seedRepoAuthoredPartition(t, store, "/.repos/github.com/acme/app")

	server := &Server{store: store, principalID: "test"}
	server.syncRepoBundlesShard(ctx, 0, 1)

	config, err := store.ReadFile(ctx, paths.PartitionConfig("prod"))
	if err != nil {
		t.Fatalf("discovered partition was not imported: %v", err)
	}
	if len(config) == 0 {
		t.Fatal("imported partition config is empty")
	}
}

func TestRepoImportSkipsRepoWithoutPartitions(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{Writes: []guardianapi.PathWrite{
		{LogicalPath: "/.repos/github.com/acme/plain/README.md", Content: []byte("hello")},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	server := &Server{store: store, principalID: "test"}
	server.syncRepoBundlesShard(ctx, 0, 1)

	if _, err := store.ReadFile(ctx, paths.PartitionConfig("prod")); err == nil {
		t.Fatal("repo without guardian/partitions must not create a partition")
	}
}

func writeTestFiles(t *testing.T, ctx context.Context, store guardianapi.Store, files map[string]string) {
	t.Helper()
	writes := make([]guardianapi.PathWrite, 0, len(files))
	for path, content := range files {
		writes = append(writes, guardianapi.PathWrite{LogicalPath: path, Content: []byte(content)})
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{Writes: writes}); err != nil {
		t.Fatalf("write test files: %v", err)
	}
}
