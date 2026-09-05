package paths

import (
	"strings"
	"testing"
)

func TestPartitionConfig(t *testing.T) {
	got := PartitionConfig("genomic-processing")
	want := "/partitions/genomic-processing/config.yaml"
	if got != want {
		t.Fatalf("PartitionConfig = %q, want %q", got, want)
	}
}

func TestIntentManifest(t *testing.T) {
	got := IntentManifest("demo", "core-storage")
	want := "/partitions/demo/intents/core-storage.yaml"
	if got != want {
		t.Fatalf("IntentManifest = %q, want %q", got, want)
	}
}

func TestIntentState(t *testing.T) {
	got := IntentState("demo", "worker-nodes")
	want := "/partitions/demo/.state/intents/worker-nodes.json"
	if got != want {
		t.Fatalf("IntentState = %q, want %q", got, want)
	}
}

func TestPartitionRuntime(t *testing.T) {
	got := PartitionRuntime("demo")
	want := "/partitions/demo/.state/runtime.json"
	if got != want {
		t.Fatalf("PartitionRuntime = %q, want %q", got, want)
	}
}

func TestAssetState(t *testing.T) {
	got := AssetState("demo", "workers", "processor")
	want := "/partitions/demo/.state/assets/workers--processor.json"
	if got != want {
		t.Fatalf("AssetState = %q, want %q", got, want)
	}
}

func TestQueuePaths(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"QueueDir", QueueDir("local"), "/.queues/local"},
		{"QueueTask", QueueTask("local", "task-1"), "/.queues/local/task-1.json"},
		{"QueueClaim", QueueClaim("local", "task-1"), "/.queues/local/.claims/task-1.json"},
		{"QueueResult", QueueResult("local", "task-1"), "/.queues/local/.results/task-1.json"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestArchivePaths(t *testing.T) {
	got := ArchiveState("demo", "workers", "deploy-001")
	want := "/.archive/demo/workers/deploy-001/state.json"
	if got != want {
		t.Fatalf("ArchiveState = %q, want %q", got, want)
	}

	got = ArchiveManifest("demo", "workers", "deploy-001")
	want = "/.archive/demo/workers/deploy-001/manifest.yaml"
	if got != want {
		t.Fatalf("ArchiveManifest = %q, want %q", got, want)
	}

	got = ArchiveIndex("demo", "workers")
	want = "/.archive/demo/workers/index.json"
	if got != want {
		t.Fatalf("ArchiveIndex = %q, want %q", got, want)
	}
}

func TestPartitionFromLogicalPath(t *testing.T) {
	cases := []struct {
		path string
		want string
		ok   bool
	}{
		{"/partitions/demo/config.yaml", "demo", true},
		{"/partitions/demo/intents/foo.yaml", "demo", true},
		{"/partitions/demo/.state/intents/foo.json", "demo", true},
		{"/.queues/local/task.json", "", false},
		{"/.archive/demo/workers/d/state.json", "", false},
		{"/partitions/", "", false},
	}
	for _, tc := range cases {
		got, ok := PartitionFromLogicalPath(tc.path)
		if ok != tc.ok || got != tc.want {
			t.Errorf("PartitionFromLogicalPath(%q) = (%q, %v), want (%q, %v)",
				tc.path, got, ok, tc.want, tc.ok)
		}
	}
}

func TestAllPathsStartWithSlash(t *testing.T) {
	fns := map[string]string{
		"PartitionsRoot":     PartitionsRoot(),
		"PartitionRoot":      PartitionRoot("p"),
		"PartitionConfig":    PartitionConfig("p"),
		"PartitionIntentDir": PartitionIntentsDir("p"),
		"IntentManifest":     IntentManifest("p", "i"),
		"PartitionState":     PartitionState("p"),
		"PartitionRuntime":   PartitionRuntime("p"),
		"StateRoot":          StateRoot("p"),
		"IntentState":        IntentState("p", "i"),
		"AssetState":         AssetState("p", "i", "a"),
		"QueueRoot":          QueueRoot(),
		"QueueDir":           QueueDir("x"),
		"QueueTask":          QueueTask("x", "t"),
		"QueueClaim":         QueueClaim("x", "t"),
		"QueueResult":        QueueResult("x", "t"),
		"ArchiveRoot":        ArchiveRoot(),
		"ArchiveIndex":       ArchiveIndex("p", "i"),
		"ArchiveManifest":    ArchiveManifest("p", "i", "d"),
		"ArchiveState":       ArchiveState("p", "i", "d"),
	}
	for name, path := range fns {
		if !strings.HasPrefix(path, "/") {
			t.Errorf("%s = %q: must start with /", name, path)
		}
	}
}
