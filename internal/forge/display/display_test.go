package display

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/forge/author"
	"github.com/radryc/guardian/internal/forge/repoinit"
)

func setup(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := repoinit.Init(repoinit.Options{
		Root: root, Name: "demo", Source: "github.com/acme/demo", Partition: "checkout",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := author.CreatePartition(author.PartitionOptions{Root: root, Name: "checkout"}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBindShowLint(t *testing.T) {
	root := setup(t)
	if err := Bind(root, "checkout", "asset", "service", "engineering"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := Bind(root, "checkout", "intent", "deployment", ""); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	keys, err := Show(root, "checkout")
	if err != nil {
		t.Fatal(err)
	}
	if keys["asset"] != "service" || keys["intent"] != "deployment" {
		t.Fatalf("keys = %v", keys)
	}
	list, err := Lint(root)
	if err != nil {
		t.Fatal(err)
	}
	if list.HasErrors() {
		t.Fatalf("lint = %+v", list)
	}

	config, err := os.ReadFile(filepath.Join(root, ".guardian", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "asset: service") {
		t.Fatalf("binding not written:\n%s", config)
	}
	// The partition manifest must stay canonical.
	partition, err := os.ReadFile(filepath.Join(root, "partitions", "checkout", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(partition), "service:") {
		t.Fatal("display alias leaked into a manifest")
	}
}

func TestBindRejectsReservedKey(t *testing.T) {
	root := setup(t)
	if err := Bind(root, "checkout", "asset", "canonical", ""); err == nil {
		t.Fatal("expected reserved-key rejection")
	}
}

func TestBindRequiresAssignment(t *testing.T) {
	root := setup(t)
	if err := Bind(root, "other", "asset", "service", ""); err == nil {
		t.Fatal("expected unbound partition rejection")
	}
}
