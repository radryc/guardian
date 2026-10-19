package probes

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	memorystore "github.com/rydzu/ainfra/guardian/internal/store/memory"
)

func TestWriteSnapshotsMergesLatestState(t *testing.T) {
	store := memorystore.New()
	runner := New(Config{
		Pusher:     "k8s-main",
		Cluster:    "main",
		WorkerID:   "worker-1",
		ShardCount: 1,
		Store:      store,
	})
	runner.previous["b"] = Result{ProbeID: "b", Service: "demo.api.b", Partition: "demo", Intent: "api", Asset: "b", Name: "auto-http", Status: "up"}
	runner.previous["a"] = Result{ProbeID: "a", Service: "demo.api.a", Partition: "demo", Intent: "api", Asset: "a", Name: "auto-http", Status: "up"}
	runner.writeSnapshots(context.Background())

	snapshot := readSnapshot(t, store)
	if len(snapshot.Probes) != 2 || snapshot.Probes[0].Service != "demo.api.a" {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	runner.previous["c"] = Result{ProbeID: "c", Service: "demo.api.c", Partition: "demo", Intent: "api", Asset: "c", Name: "auto-http", Status: "down"}
	runner.writeSnapshots(context.Background())

	snapshot = readSnapshot(t, store)
	if len(snapshot.Probes) != 3 {
		t.Fatalf("snapshot after new probe = %+v", snapshot.Probes)
	}
	if snapshot.WorkerID != "worker-1" || snapshot.ShardCount != 1 {
		t.Fatalf("snapshot metadata = %+v", snapshot)
	}
}

func readSnapshot(t *testing.T, store interface {
	ReadFile(context.Context, string) ([]byte, error)
}) Snapshot {
	t.Helper()
	raw, err := store.ReadFile(context.Background(), paths.ProbeSnapshot("k8s-main", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}
