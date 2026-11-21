package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverPartitionsDirectory(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "partitions", "b", "config.yaml"), "kind: Partition\n")
	write(t, filepath.Join(root, "partitions", "a", "config.yaml"), "kind: Partition\n")
	mkdir(t, filepath.Join(root, "partitions", "_bootstrap"))
	write(t, filepath.Join(root, "partitions", "_bootstrap", "config.yaml"), "kind: Partition\n")
	mkdir(t, filepath.Join(root, "partitions", "empty"))

	refs, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].Name != "a" || refs[1].Name != "b" {
		t.Fatalf("refs = %+v, want a,b", refs)
	}
}

func TestDiscoverSinglePartition(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "config.yaml"), "kind: Partition\n")

	refs, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Dir != root {
		t.Fatalf("refs = %+v", refs)
	}
}

func TestDiscoverImmediateChildren(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "one", "partition.yaml"), "kind: Partition\n")
	write(t, filepath.Join(root, "two", "config.yaml"), "kind: Partition\n")

	refs, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].Name != "one" || refs[1].Name != "two" {
		t.Fatalf("refs = %+v", refs)
	}
}

func TestDiscoverNone(t *testing.T) {
	if _, err := Discover(t.TempDir()); err == nil {
		t.Fatal("expected an error when no partition exists")
	}
}
