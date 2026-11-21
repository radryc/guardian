package plan

import (
	"context"
	"path/filepath"
	"testing"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	"github.com/radryc/guardian/internal/forge/author"
)

func TestBuildDiskPlan(t *testing.T) {
	root := t.TempDir()
	configPath, err := author.CreatePartition(author.PartitionOptions{Root: root, Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(configPath)
	if _, err := author.CreateIntent(author.IntentOptions{
		PartitionDir: dir, Name: "api", Pusher: "k8s-main", Cluster: "k8s-main", Namespace: "demo",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := author.AddAsset(dir, "api", assetdomain.Spec{
		Type: "Compute", Name: "api", Properties: map[string]any{"image": "x:1"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := author.AddProbe(dir, "api", assetdomain.Spec{
		Type: "Probe", Name: "api-probe", Properties: map[string]any{"type": "http", "target": "api"},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := Build(context.Background(), Options{
		Root: root, PartitionDir: dir, Partition: "demo", Changed: false,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !result.Success || result.Against != "disk" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Intents) != 1 || result.Intents[0] != "api" {
		t.Fatalf("intents = %v", result.Intents)
	}
	if result.Assets != 2 {
		t.Fatalf("assets = %d", result.Assets)
	}
	if len(result.Operations) < 2 {
		t.Fatalf("operations = %+v", result.Operations)
	}
	for _, op := range result.Operations {
		if op.Kind != "upsert" {
			t.Fatalf("unexpected op %+v", op)
		}
	}
}
