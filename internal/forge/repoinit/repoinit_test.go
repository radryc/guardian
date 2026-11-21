package repoinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWritesRepositoryFiles(t *testing.T) {
	root := t.TempDir()
	result, err := Init(Options{
		Root: root, Name: "acme-platform",
		Source: "github.com/acme/platform", Branch: "main", Partition: "checkout",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, relative := range []string{
		".guardian/config.yaml",
		".monofs/pipelines/partition-pipeline.yml",
		".forge/policy.yaml",
		".gitignore",
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Errorf("missing %s: %v", relative, err)
		}
	}
	if len(result.Files) < 4 {
		t.Fatalf("files = %v", result.Files)
	}

	config, err := os.ReadFile(filepath.Join(root, ".guardian", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: acme-platform", "source: github.com/acme/platform", "partition: checkout"} {
		if !strings.Contains(string(config), want) {
			t.Errorf("config missing %q:\n%s", want, config)
		}
	}
	pipeline, err := os.ReadFile(filepath.Join(root, ".monofs", "pipelines", "partition-pipeline.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pipeline), "name: partition-pipeline") {
		t.Errorf("pipeline name missing:\n%s", pipeline)
	}
	gitignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gitignore), ".forge/") {
		t.Errorf("gitignore missing .forge/:\n%s", gitignore)
	}
}

func TestInitMergesExistingConfig(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(Options{Root: root, Name: "demo", Source: "github.com/acme/demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(Options{Root: root, Name: "demo", Source: "github.com/acme/demo", Partition: "checkout"}); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".guardian", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: demo", "source: github.com/acme/demo", "partition: checkout", "partitionPath: partitions/checkout"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("merged config missing %q:\n%s", want, data)
		}
	}
}

func TestEnsureBindingCreatesConfig(t *testing.T) {
	root := t.TempDir()
	path, err := EnsureBinding(BindingOptions{Root: root, Partition: "payments"})
	if err != nil {
		t.Fatalf("EnsureBinding: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "partition: payments") || !strings.Contains(string(data), "partitionPath: partitions/payments") {
		t.Fatalf("binding missing:\n%s", data)
	}
}

func TestInitIsIdempotentForGitignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".forge/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(Options{Root: root, Name: "demo"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), ".forge/") != 1 {
		t.Fatalf("gitignore duplicated:\n%s", data)
	}
}
