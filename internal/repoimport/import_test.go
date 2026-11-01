package repoimport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
)

const partitionYAML = `apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: prod
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 1m
`

const intentYAML = `apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  targetPusher: aws-123456789012
  target:
    account: "123456789012"
    region: eu-west-1
  assets:
    - type: Compute
      name: api
      properties:
        image: nginx
`

func writeRepoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func seedRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeRepoFile(t, root, "guardian/partitions/prod/partition.yaml", partitionYAML)
	writeRepoFile(t, root, "guardian/partitions/prod/intents/api.yaml", intentYAML)
	writeRepoFile(t, root, "guardian/partitions/prod/payloads/nginx/index.html", "<h1>hi</h1>")
	return root
}

func TestImportLocalRepoWritesPartition(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	source := NewLocalSource(seedRepo(t))

	result, err := Import(ctx, source, store, Options{RepoName: "strata", Principal: "tester"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !result.Changed || result.FilesWritten != 3 {
		t.Fatalf("result = %+v, want 3 changed files", result)
	}
	if got, want := strings.Join(result.Imported, ","), "prod"; got != want {
		t.Fatalf("imported = %q, want %q", got, want)
	}

	config, err := store.ReadFile(ctx, paths.PartitionConfig("prod"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(config), "name: prod") {
		t.Fatalf("config content = %s", config)
	}
	intent, err := store.ReadFile(ctx, paths.PartitionIntentsDir("prod")+"/api.yaml")
	if err != nil {
		t.Fatalf("read intent: %v", err)
	}
	if !strings.Contains(string(intent), "targetPusher: aws-123456789012") {
		t.Fatalf("intent content = %s", intent)
	}
	payload, err := store.ReadFile(ctx, paths.PartitionRoot("prod")+"/payloads/nginx/index.html")
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	if string(payload) != "<h1>hi</h1>" {
		t.Fatalf("payload content = %q", payload)
	}
}

func TestImportSkipsUnchangedFiles(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	root := seedRepo(t)
	source := NewLocalSource(root)

	if _, err := Import(ctx, source, store, Options{RepoName: "strata"}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	before, err := store.Stat(ctx, paths.PartitionConfig("prod"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	result, err := Import(ctx, source, store, Options{RepoName: "strata"})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if result.Changed || result.FilesWritten != 0 {
		t.Fatalf("second import = %+v, want unchanged", result)
	}
	if got, want := strings.Join(result.Skipped, ","), "prod"; got != want {
		t.Fatalf("skipped = %q, want %q", got, want)
	}
	after, err := store.Stat(ctx, paths.PartitionConfig("prod"))
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if before.VersionID != after.VersionID {
		t.Fatalf("unchanged import bumped version: %s -> %s", before.VersionID, after.VersionID)
	}
}

func TestImportWritesChangedFiles(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	root := seedRepo(t)
	source := NewLocalSource(root)

	if _, err := Import(ctx, source, store, Options{RepoName: "strata"}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	writeRepoFile(t, root, "guardian/partitions/prod/payloads/nginx/index.html", "<h1>changed</h1>")

	result, err := Import(ctx, source, store, Options{RepoName: "strata"})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if !result.Changed || result.FilesWritten != 1 {
		t.Fatalf("result = %+v, want 1 changed file", result)
	}
	payload, err := store.ReadFile(ctx, paths.PartitionRoot("prod")+"/payloads/nginx/index.html")
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	if string(payload) != "<h1>changed</h1>" {
		t.Fatalf("payload content = %q", payload)
	}
}

func TestImportRejectsInlineSecret(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	root := t.TempDir()
	writeRepoFile(t, root, "guardian/partitions/prod/partition.yaml", partitionYAML)
	writeRepoFile(t, root, "guardian/partitions/prod/intents/api.yaml", `apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  assets:
    - type: Secret
      name: token
      properties:
        value: super-secret
`)
	_, err := Import(ctx, NewLocalSource(root), store, Options{RepoName: "strata"})
	if err == nil || !strings.Contains(err.Error(), "inline Secret value is not allowed") {
		t.Fatalf("expected inline secret rejection, got %v", err)
	}
}

func TestImportRejectsPartitionNameMismatch(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	root := t.TempDir()
	writeRepoFile(t, root, "guardian/partitions/prod/partition.yaml", `apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: staging
`)
	_, err := Import(ctx, NewLocalSource(root), store, Options{RepoName: "strata"})
	if err == nil || !strings.Contains(err.Error(), "does not match directory") {
		t.Fatalf("expected name mismatch error, got %v", err)
	}
}

func TestImportIgnoresSecretsDir(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	root := seedRepo(t)
	writeRepoFile(t, root, "guardian/partitions/prod/secrets/db.txt", "secret-material")

	result, err := Import(ctx, NewLocalSource(root), store, Options{RepoName: "strata"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Warnings) == 0 || !strings.Contains(strings.Join(result.Warnings, ","), "secrets/**") {
		t.Fatalf("expected secrets warning, got %v", result.Warnings)
	}
	if _, err := store.ReadFile(ctx, paths.PartitionRoot("prod")+"/secrets/db.txt"); err == nil {
		t.Fatal("secrets must never be imported")
	}
}

func TestImportFiltersSinglePartition(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	root := seedRepo(t)
	writeRepoFile(t, root, "guardian/partitions/staging/partition.yaml", strings.ReplaceAll(partitionYAML, "name: prod", "name: staging"))

	result, err := Import(ctx, NewLocalSource(root), store, Options{RepoName: "strata", Partition: "staging"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if got, want := strings.Join(result.Imported, ","), "staging"; got != want {
		t.Fatalf("imported = %q, want %q", got, want)
	}
	if _, err := store.ReadFile(ctx, paths.PartitionConfig("prod")); err == nil {
		t.Fatal("prod must not be imported when filtered")
	}
}

func TestImportMissingPartitionsDir(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	_, err := Import(ctx, NewLocalSource(t.TempDir()), store, Options{RepoName: "strata"})
	if err == nil || !strings.Contains(err.Error(), "no guardian/partitions") {
		t.Fatalf("expected missing partitions error, got %v", err)
	}
}

func TestImportStoreSource(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	root := "/.repos/github.com/example/strata"
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{
			{LogicalPath: root + "/guardian/partitions/prod/partition.yaml", Content: []byte(partitionYAML)},
			{LogicalPath: root + "/guardian/partitions/prod/intents/api.yaml", Content: []byte(intentYAML)},
		},
	}); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	source := NewStoreSource(store, root)
	result, err := Import(ctx, source, store, Options{RepoName: "strata"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if got, want := strings.Join(result.Imported, ","), "prod"; got != want {
		t.Fatalf("imported = %q, want %q", got, want)
	}
	if _, err := store.ReadFile(ctx, paths.PartitionIntentsDir("prod")+"/api.yaml"); err != nil {
		t.Fatalf("imported intent missing: %v", err)
	}
}

func TestImportPrunesRemovedIntent(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	root := seedRepo(t)
	writeRepoFile(t, root, "guardian/partitions/prod/intents/worker.yaml", strings.Replace(intentYAML, "name: api", "name: worker", 1))
	source := NewLocalSource(root)
	opts := Options{RepoName: "strata", Principal: "tester", Source: "https://example/repo", Prune: true}

	if _, err := Import(ctx, source, store, opts); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if _, err := store.ReadFile(ctx, paths.IntentManifest("prod", "worker")); err != nil {
		t.Fatalf("worker intent missing after first import: %v", err)
	}
	if _, err := store.ReadFile(ctx, paths.RepoImportManifest("prod")); err != nil {
		t.Fatalf("ownership manifest missing: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "guardian/partitions/prod/intents/worker.yaml")); err != nil {
		t.Fatalf("remove worker: %v", err)
	}
	result, err := Import(ctx, source, store, opts)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if result.FilesDeleted != 1 {
		t.Fatalf("FilesDeleted = %d, want 1", result.FilesDeleted)
	}
	if _, err := store.ReadFile(ctx, paths.IntentManifest("prod", "worker")); err == nil {
		t.Fatal("worker intent was not pruned")
	}
	if _, err := store.ReadFile(ctx, paths.IntentManifest("prod", "api")); err != nil {
		t.Fatalf("api intent was pruned: %v", err)
	}
}

func TestImportDoesNotPruneAnotherOwnersFiles(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	source := NewLocalSource(seedRepo(t))

	if _, err := Import(ctx, source, store, Options{RepoName: "a", Principal: "tester", Source: "source-a", Prune: true}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	// A second owner for the same partition must not prune source-a's files.
	// Use a repo with only a config so the intent is absent from its desired set.
	rootB := t.TempDir()
	writeRepoFile(t, rootB, "guardian/partitions/prod/partition.yaml", partitionYAML)
	result, err := Import(ctx, NewLocalSource(rootB), store, Options{RepoName: "b", Principal: "tester", Source: "source-b", Prune: true})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if result.FilesDeleted != 0 {
		t.Fatalf("FilesDeleted = %d, want 0", result.FilesDeleted)
	}
	if _, err := store.ReadFile(ctx, paths.IntentManifest("prod", "api")); err != nil {
		t.Fatalf("api intent owned by source-a was pruned: %v", err)
	}
}

func TestImportReportsOrphanedPartitionWithoutDeletingIt(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	if _, err := Import(ctx, NewLocalSource(seedRepo(t)), store, Options{RepoName: "strata", Principal: "tester", Prune: true}); err != nil {
		t.Fatalf("first import: %v", err)
	}

	root := t.TempDir()
	writeRepoFile(t, root, "guardian/partitions/stage/partition.yaml", strings.ReplaceAll(partitionYAML, "name: prod", "name: stage"))
	writeRepoFile(t, root, "guardian/partitions/stage/intents/api.yaml", intentYAML)
	result, err := Import(ctx, NewLocalSource(root), store, Options{RepoName: "strata", Principal: "tester", Prune: true})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if len(result.Orphaned) != 1 || result.Orphaned[0] != "prod" {
		t.Fatalf("orphaned = %#v, want [prod]", result.Orphaned)
	}
	if _, err := store.ReadFile(ctx, paths.PartitionConfig("prod")); err != nil {
		t.Fatalf("orphaned partition was deleted: %v", err)
	}
}
