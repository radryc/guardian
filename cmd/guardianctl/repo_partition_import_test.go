package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/internal/repoimport"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
)

func writeRepoPartitionFixture(t *testing.T, sourceDir, intentYAML string) {
	t.Helper()
	dir := filepath.Join(sourceDir, "guardian", "partitions", "demo")
	if err := os.MkdirAll(filepath.Join(dir, "intents"), 0o755); err != nil {
		t.Fatalf("mkdir intents: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "payloads"), 0o755); err != nil {
		t.Fatalf("mkdir payloads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "partition.yaml"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`), 0o644); err != nil {
		t.Fatalf("write partition.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "intents", "web.yaml"), []byte(intentYAML), 0o644); err != nil {
		t.Fatalf("write intent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "payloads", "config.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
}

func TestImportRepoPartition(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	sourceDir := t.TempDir()
	writeRepoPartitionFixture(t, sourceDir, `
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: web
spec:
  targetPusher: local
  target:
    cluster: local
  assets:
    - type: Config
      name: config
      version: v1
      properties:
        content: "key: value"
`)

	imported, err := importRepoPartition(ctx, store, sourceDir, "demo")
	if err != nil {
		t.Fatalf("importRepoPartition() error = %v", err)
	}
	if !imported {
		t.Fatalf("importRepoPartition() = false, want true")
	}
	if _, err := store.ReadFile(ctx, paths.PartitionConfig("demo")); err != nil {
		t.Fatalf("partition config missing: %v", err)
	}
	if _, err := store.ReadFile(ctx, paths.IntentManifest("demo", "web")); err != nil {
		t.Fatalf("intent missing: %v", err)
	}
	if _, err := store.ReadFile(ctx, paths.PartitionRoot("demo")+"/payloads/config.txt"); err != nil {
		t.Fatalf("payload missing: %v", err)
	}
}

func TestImportRepoPartitionRejectsInlineSecret(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	sourceDir := t.TempDir()
	writeRepoPartitionFixture(t, sourceDir, `
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: web
spec:
  targetPusher: local
  target:
    cluster: local
  assets:
    - type: Secret
      name: creds
      version: v1
      properties:
        value: "super-secret"
`)

	_, err := importRepoPartition(ctx, store, sourceDir, "demo")
	if err == nil || !strings.Contains(err.Error(), "inline Secret value") {
		t.Fatalf("error = %v, want inline secret rejection", err)
	}
	if _, readErr := store.ReadFile(ctx, paths.PartitionConfig("demo")); readErr == nil {
		t.Fatalf("rejected partition must not be written")
	}
}

func TestImportRepoPartitionMissing(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	imported, err := importRepoPartition(ctx, store, t.TempDir(), "demo")
	if err != nil {
		t.Fatalf("importRepoPartition() error = %v", err)
	}
	if imported {
		t.Fatalf("importRepoPartition() = true, want false for a repo without the partition")
	}
}

func TestRepoDisplayLogicalPath(t *testing.T) {
	cases := map[string]string{
		"https://github.com/example/strata.git":      "/.repos/github.com/example/strata",
		"https://github.com/example/strata":          "/.repos/github.com/example/strata",
		"git@github.com:example/strata.git":          "/.repos/github.com/example/strata",
		"https://user@gitlab.com/group/project.git":  "/.repos/gitlab.com/group/project",
		"ssh://git@gitlab.com/group/sub/project.git": "/.repos/gitlab.com/group/sub/project",
		"not-a-url": "",
		"":          "",
	}
	for input, want := range cases {
		if got := repoDisplayLogicalPath(input); got != want {
			t.Errorf("repoDisplayLogicalPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRepoSourcesPrefersLocalCheckout(t *testing.T) {
	dir := t.TempDir()
	store := memory.New()
	repo := &registry.Repo{Name: "demo", Source: dir}
	source, err := repoSources(store, repo, "")
	if err != nil {
		t.Fatalf("repoSources: %v", err)
	}
	if _, ok := source.(*repoimport.LocalSource); !ok {
		t.Fatalf("source = %T, want LocalSource", source)
	}
}

func TestRepoSourcesUsesStoreMappingForGitURL(t *testing.T) {
	store := memory.New()
	repo := &registry.Repo{Name: "demo", Source: "https://github.com/example/strata.git"}
	source, err := repoSources(store, repo, "")
	if err != nil {
		t.Fatalf("repoSources: %v", err)
	}
	storeSource, ok := source.(*repoimport.StoreSource)
	if !ok {
		t.Fatalf("source = %T, want StoreSource", source)
	}
	if storeSource.Root != "/.repos/github.com/example/strata" {
		t.Fatalf("root = %q", storeSource.Root)
	}
}
