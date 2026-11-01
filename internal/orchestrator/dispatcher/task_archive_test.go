package dispatcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/store/memory"
)

func archiveTask(id string) *taskdomain.Task {
	return &taskdomain.Task{
		TaskID:       id,
		Partition:    "demo",
		Intent:       "web",
		TargetPusher: "local",
		CreatedAt:    time.Now().UTC(),
	}
}

func TestTaskArchiveRetentionByCount(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	d := NewDispatcher(store, "test")
	d.SetTaskArchiveRetention(2, 0)

	for _, id := range []string{"task_0001", "task_0002", "task_0003", "task_0004"} {
		if err := d.QueueTask(ctx, archiveTask(id)); err != nil {
			t.Fatalf("QueueTask(%s): %v", id, err)
		}
	}

	entries, err := store.ListDir(ctx, paths.TaskSpecDir("demo"))
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		names[entry.Name] = true
	}
	if names["task_0001.json"] || names["task_0002.json"] {
		t.Fatalf("old task specs were not pruned: %#v", names)
	}
	if !names["task_0003.json"] || !names["task_0004.json"] || !names[".chain.json"] {
		t.Fatalf("retained archive is incomplete: %#v", names)
	}
}

func TestTaskArchiveChainVerifies(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	d := NewDispatcher(store, "test")

	for _, id := range []string{"task_0001", "task_0002"} {
		if err := d.QueueTask(ctx, archiveTask(id)); err != nil {
			t.Fatalf("QueueTask(%s): %v", id, err)
		}
	}
	raw, err := store.ReadFile(ctx, paths.TaskArchiveChain("demo"))
	if err != nil {
		t.Fatal(err)
	}
	var chain taskArchiveChain
	if err := json.Unmarshal(raw, &chain); err != nil {
		t.Fatal(err)
	}
	if len(chain.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(chain.Entries))
	}
	prev := ""
	for _, entry := range chain.Entries {
		content, err := store.ReadFile(ctx, paths.TaskSpec("demo", entry.TaskID))
		if err != nil {
			t.Fatalf("read %s: %v", entry.TaskID, err)
		}
		sum := sha256.Sum256(append([]byte(prev), content...))
		if got := hex.EncodeToString(sum[:]); got != entry.SHA256 {
			t.Fatalf("chain hash mismatch for %s: got %s want %s", entry.TaskID, entry.SHA256, got)
		}
		if entry.PrevSHA256 != prev {
			t.Fatalf("prev hash for %s = %q, want %q", entry.TaskID, entry.PrevSHA256, prev)
		}
		prev = entry.SHA256
	}
}

func TestTaskArchiveRetentionByAge(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	d := NewDispatcher(store, "test")

	if err := d.QueueTask(ctx, archiveTask("task_0001")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	d.SetTaskArchiveRetention(0, time.Millisecond)
	if err := d.QueueTask(ctx, archiveTask("task_0002")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadFile(ctx, paths.TaskSpec("demo", "task_0001")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old task spec still exists: %v", err)
	}
	if _, err := store.ReadFile(ctx, paths.TaskSpec("demo", "task_0002")); err != nil {
		t.Fatalf("new task spec missing: %v", err)
	}
}

func TestTaskArchiveRetentionKeepsQueueFiles(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	d := NewDispatcher(store, "test")
	d.SetTaskArchiveRetention(1, 0)

	for _, id := range []string{"task_0001", "task_0002"} {
		if err := d.QueueTask(ctx, archiveTask(id)); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"task_0001", "task_0002"} {
		if _, err := store.ReadFile(ctx, paths.QueueTask("local", id)); err != nil {
			t.Fatalf("queue file %s was pruned: %v", id, err)
		}
	}
}
