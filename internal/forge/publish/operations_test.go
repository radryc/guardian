package publish

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/workspacepub"
)

func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
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

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	gitAvailable(t)
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "forge@test")
	git(t, dir, "config", "user.name", "Forge Test")
	return dir
}

func TestBuildOperationsChanged(t *testing.T) {
	repo := initRepo(t)
	write(t, filepath.Join(repo, "partitions", "demo", "config.yaml"), "kind: Partition\n")
	write(t, filepath.Join(repo, "partitions", "demo", "payloads", "app.yaml"), "kind: ConfigMap\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "initial")

	write(t, filepath.Join(repo, "partitions", "demo", "config.yaml"), "kind: Partition\nmetadata:\n  name: demo\n")
	if err := os.Remove(filepath.Join(repo, "partitions", "demo", "payloads", "app.yaml")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repo, "partitions", "demo", "alerts", "health.yaml"), "version: 1\n")

	ops, err := BuildOperations(context.Background(), OperationOptions{
		Changed:      true,
		BaseRef:      "HEAD",
		Root:         repo,
		PartitionDir: filepath.Join(repo, "partitions", "demo"),
	})
	if err != nil {
		t.Fatalf("BuildOperations: %v", err)
	}
	got := map[string]string{}
	for _, op := range ops {
		got[op.Path] = op.Kind
	}
	if got["partitions/demo/config.yaml"] != workspacepub.OperationUpsert {
		t.Errorf("config op = %q (%v)", got["partitions/demo/config.yaml"], got)
	}
	if got["partitions/demo/payloads/app.yaml"] != workspacepub.OperationDelete {
		t.Errorf("payload op = %q (%v)", got["partitions/demo/payloads/app.yaml"], got)
	}
	if got["partitions/demo/alerts/health.yaml"] != workspacepub.OperationUpsert {
		t.Errorf("alert op = %q (%v)", got["partitions/demo/alerts/health.yaml"], got)
	}
}

func TestBuildOperationsAllSkipsStateAndSecrets(t *testing.T) {
	repo := initRepo(t)
	dir := filepath.Join(repo, "partitions", "demo")
	write(t, filepath.Join(dir, "config.yaml"), "kind: Partition\n")
	write(t, filepath.Join(dir, ".state", "images.json"), "{}")
	write(t, filepath.Join(dir, "secrets", "tls.yaml"), "value: x\n")
	write(t, filepath.Join(dir, "alerts", "health.yaml"), "version: 1\n")

	ops, err := BuildOperations(context.Background(), OperationOptions{
		Changed:      false,
		Root:         repo,
		PartitionDir: dir,
	})
	if err != nil {
		t.Fatalf("BuildOperations: %v", err)
	}
	paths := map[string]bool{}
	for _, op := range ops {
		paths[op.Path] = true
	}
	if !paths["partitions/demo/config.yaml"] || !paths["partitions/demo/alerts/health.yaml"] {
		t.Fatalf("missing expected operations: %v", paths)
	}
	if paths["partitions/demo/.state/images.json"] {
		t.Error(".state must be skipped")
	}
	if paths["partitions/demo/secrets/tls.yaml"] {
		t.Error("secrets must be skipped by default")
	}

	ops, err = BuildOperations(context.Background(), OperationOptions{
		Changed:        false,
		Root:           repo,
		PartitionDir:   dir,
		IncludeSecrets: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	foundSecret := false
	for _, op := range ops {
		if op.Path == "partitions/demo/secrets/tls.yaml" {
			foundSecret = true
		}
	}
	if !foundSecret {
		t.Error("secrets must be included when requested")
	}
}

func TestParseNameStatusRename(t *testing.T) {
	entries := parseNameStatus("R100\x00old.yaml\x00new.yaml\x00M\x00other.yaml\x00")
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].status != "R" || entries[0].oldPath != "old.yaml" || entries[0].path != "new.yaml" {
		t.Fatalf("rename = %+v", entries[0])
	}
	if entries[1].status != "M" || entries[1].path != "other.yaml" {
		t.Fatalf("modify = %+v", entries[1])
	}
}

func TestDisplayPathAndStorageID(t *testing.T) {
	cases := []struct {
		url     string
		display string
	}{
		{"https://github.com/org/repo.git", "github.com/org/repo"},
		{"git@github.com:org/repo.git", "github.com/org/repo"},
		{"https://gitlab.example.com/team/service", "gitlab.example.com/team/service"},
	}
	for _, tc := range cases {
		if got := DisplayPathFromURL(tc.url); got != tc.display {
			t.Errorf("DisplayPathFromURL(%q) = %q, want %q", tc.url, got, tc.display)
		}
	}
	first := StorageIDForURL("https://github.com/org/repo.git")
	second := StorageIDForURL("git@github.com:org/repo.git")
	if first == "" || first != second {
		t.Fatalf("storage ids must be stable: %q vs %q", first, second)
	}
	if strings.ContainsAny(first, "/. ") {
		t.Fatalf("storage id must be branch-safe: %q", first)
	}
}
