package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
)

func TestPushPartitionBundleWritesPushEvent(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "intents"), 0o755); err != nil {
		t.Fatalf("mkdir intents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "intents", "web.yaml"), []byte(`
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
`), 0o644); err != nil {
		t.Fatalf("write intent: %v", err)
	}

	bundle, err := loadLocalPartitionBundle(tmp, false)
	if err != nil {
		t.Fatalf("loadLocalPartitionBundle() error = %v", err)
	}

	if _, err := pushPartitionBundle(ctx, store, bundle, false); err != nil {
		t.Fatalf("pushPartitionBundle() error = %v", err)
	}

	entries, err := store.ListDir(ctx, paths.StateEventsDir("demo"))
	if err != nil {
		t.Fatalf("ListDir(events) error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("event count = %d, want 1", len(entries))
	}
	var event historydomain.EventRecord
	if err := readJSON(ctx, store, paths.EventState("demo", entries[0].Name[:len(entries[0].Name)-len(".json")]), &event); err != nil {
		t.Fatalf("read event error = %v", err)
	}
	if event.Type != "partition.pushed" {
		t.Fatalf("event type = %q, want partition.pushed", event.Type)
	}
	if event.Details["is_new"] != "true" {
		t.Fatalf("is_new = %q, want true", event.Details["is_new"])
	}
}

func TestPushPartitionBundleUpdateWritesPushEvent(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	seedFile(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`))

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "intents"), 0o755); err != nil {
		t.Fatalf("mkdir intents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "intents", "web.yaml"), []byte(`
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
`), 0o644); err != nil {
		t.Fatalf("write intent: %v", err)
	}

	bundle, err := loadLocalPartitionBundle(tmp, false)
	if err != nil {
		t.Fatalf("loadLocalPartitionBundle() error = %v", err)
	}

	if _, err := pushPartitionBundle(ctx, store, bundle, false); err != nil {
		t.Fatalf("pushPartitionBundle() error = %v", err)
	}

	entries, err := store.ListDir(ctx, paths.StateEventsDir("demo"))
	if err != nil {
		t.Fatalf("ListDir(events) error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("event count = %d, want 1", len(entries))
	}
	var event historydomain.EventRecord
	if err := readJSON(ctx, store, paths.EventState("demo", entries[0].Name[:len(entries[0].Name)-len(".json")]), &event); err != nil {
		t.Fatalf("read event error = %v", err)
	}
	if event.Type != "partition.pushed" {
		t.Fatalf("event type = %q, want partition.pushed", event.Type)
	}
	if event.Details["is_new"] != "false" {
		t.Fatalf("is_new = %q, want false", event.Details["is_new"])
	}
}
