package memory

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestStoreCRUDAndWatch(t *testing.T) {
	store := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := store.Watch(ctx, []string{"/partitions"})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	batch, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: "/partitions/demo/config.yaml",
			Content:     []byte("test"),
		}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed"},
	})
	if err != nil {
		t.Fatalf("UpsertFiles() error = %v", err)
	}

	select {
	case event := <-events:
		if event.Type != guardianapi.ChangeAdded {
			t.Fatalf("event.Type = %q, want %q", event.Type, guardianapi.ChangeAdded)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for watch event")
	}

	info, err := store.Stat(ctx, "/partitions/demo/config.yaml")
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.VersionID == "" {
		t.Fatalf("Stat().VersionID is empty")
	}

	content, err := store.ReadFile(ctx, "/partitions/demo/config.yaml")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "test" {
		t.Fatalf("ReadFile() = %q, want test", string(content))
	}

	versions, err := store.ListVersions(ctx, "/partitions/demo/config.yaml")
	if err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("len(versions) = %d, want 1", len(versions))
	}

	versioned, err := store.GetVersion(ctx, "/partitions/demo/config.yaml", batch.Files[0].VersionID)
	if err != nil {
		t.Fatalf("GetVersion() error = %v", err)
	}
	if string(versioned.Content) != "test" {
		t.Fatalf("GetVersion().Content = %q, want test", string(versioned.Content))
	}

	if _, err := store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: "/partitions/demo/config.yaml"}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "delete"},
	}); err != nil {
		t.Fatalf("DeletePaths() error = %v", err)
	}

	versions, err = store.ListVersions(ctx, "/partitions/demo/config.yaml")
	if err != nil {
		t.Fatalf("ListVersions() after delete error = %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("len(versions) after delete = %d, want 2", len(versions))
	}
	if !versions[0].Tombstone {
		t.Fatalf("latest version Tombstone = false, want true")
	}
}

// TestStoreTombstoneHidesContent verifies that ReadFile and Stat return
// ErrNotExist after a path is deleted (tombstoned), while GetVersion still
// returns the pre-deletion content by version ID.
func TestStoreTombstoneHidesContent(t *testing.T) {
	ctx := context.Background()
	store := New()

	batch, _ := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: "/x/y.yaml", Content: []byte("alive")}},
		Context: guardianapi.MutationContext{PrincipalID: "t"},
	})
	versionID := batch.Files[0].VersionID

	store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: "/x/y.yaml"}},
		Context: guardianapi.MutationContext{PrincipalID: "t"},
	})

	if _, err := store.ReadFile(ctx, "/x/y.yaml"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadFile after delete: want ErrNotExist, got %v", err)
	}
	if _, err := store.Stat(ctx, "/x/y.yaml"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat after delete: want ErrNotExist, got %v", err)
	}

	vf, err := store.GetVersion(ctx, "/x/y.yaml", versionID)
	if err != nil {
		t.Fatalf("GetVersion pre-tombstone: %v", err)
	}
	if string(vf.Content) != "alive" {
		t.Fatalf("GetVersion content = %q, want alive", string(vf.Content))
	}
}

// TestStoreCASAbsent verifies that ExpectedVersionID="absent" enforces
// exclusive create: the first write succeeds, the second conflicts.
func TestStoreCASAbsent(t *testing.T) {
	ctx := context.Background()
	store := New()

	write := func() error {
		_, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{
				LogicalPath:       "/.queues/local/.claims/task-1.json",
				Content:           []byte(`{"taskID":"task-1"}`),
				ExpectedVersionID: "absent",
			}},
			Context: guardianapi.MutationContext{PrincipalID: "worker"},
		})
		return err
	}

	if err := write(); err != nil {
		t.Fatalf("first absent write: %v", err)
	}
	if err := write(); !errors.Is(err, guardianapi.ErrConflict) {
		t.Fatalf("second absent write: want ErrConflict, got %v", err)
	}
}

// TestStoreListDir verifies that ListDir returns direct children only,
// correctly distinguishing files from directory prefixes, and excludes
// tombstoned paths.
func TestStoreListDir(t *testing.T) {
	ctx := context.Background()
	store := New()

	seed := func(p string) {
		store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes:  []guardianapi.PathWrite{{LogicalPath: p, Content: []byte("x")}},
			Context: guardianapi.MutationContext{PrincipalID: "t"},
		})
	}
	seed("/partitions/a/config.yaml")
	seed("/partitions/a/intents/foo.yaml")
	seed("/partitions/b/config.yaml")
	// tombstone one file – it must not appear in listing
	seed("/partitions/a/intents/gone.yaml")
	store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: "/partitions/a/intents/gone.yaml"}},
		Context: guardianapi.MutationContext{PrincipalID: "t"},
	})

	// Top-level listing should give a and b only.
	entries, err := store.ListDir(ctx, "/partitions")
	if err != nil {
		t.Fatalf("ListDir /partitions: %v", err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
		if !e.IsDir {
			t.Errorf("entry %q: IsDir=false, want true", e.Name)
		}
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("ListDir /partitions = %v, want [a b]", names)
	}

	// Listing /partitions/a/intents should return only foo.yaml (not gone.yaml).
	inner, err := store.ListDir(ctx, "/partitions/a/intents")
	if err != nil {
		t.Fatalf("ListDir intents: %v", err)
	}
	if len(inner) != 1 || inner[0].Name != "foo.yaml" {
		t.Fatalf("ListDir intents = %v, want [foo.yaml]", inner)
	}
}

// TestStoreWatchPrefixFilter verifies that a watcher only receives events
// whose paths fall under the subscribed prefix.
func TestStoreWatchPrefixFilter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := New()

	// Watch only /partitions prefix.
	ch, err := store.Watch(ctx, []string{"/partitions"})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Write under watched prefix.
	store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: "/partitions/x/config.yaml", Content: []byte("y")}},
		Context: guardianapi.MutationContext{PrincipalID: "t"},
	})
	// Write outside watched prefix – must NOT arrive on the channel.
	store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: "/.queues/local/task-1.json", Content: []byte("z")}},
		Context: guardianapi.MutationContext{PrincipalID: "t"},
	})

	select {
	case ev := <-ch:
		if ev.LogicalPath != "/partitions/x/config.yaml" {
			t.Fatalf("received unexpected event path %q", ev.LogicalPath)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for watch event")
	}

	// Channel should be empty – the queue write must not have been delivered.
	select {
	case ev := <-ch:
		t.Fatalf("got unexpected extra event: %q", ev.LogicalPath)
	default:
	}
}

// TestStoreModifiedEvent verifies that a second write to the same path
// emits a Modified event (not Added).
func TestStoreModifiedEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := New()

	ch, _ := store.Watch(ctx, []string{"/"})

	upsert := func(content string) {
		store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes:  []guardianapi.PathWrite{{LogicalPath: "/a/b.yaml", Content: []byte(content)}},
			Context: guardianapi.MutationContext{PrincipalID: "t"},
		})
	}

	upsert("v1")
	drain(t, ch, guardianapi.ChangeAdded, time.Second)

	upsert("v2")
	drain(t, ch, guardianapi.ChangeModified, time.Second)
}

func drain(t *testing.T, ch <-chan guardianapi.ChangeEvent, want guardianapi.ChangeType, timeout time.Duration) {
	t.Helper()
	select {
	case ev := <-ch:
		if ev.Type != want {
			t.Fatalf("event type = %q, want %q", ev.Type, want)
		}
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %q event", want)
	}
}
