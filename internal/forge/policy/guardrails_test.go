package policy

import (
	"os"
	"path/filepath"
	"testing"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	"github.com/radryc/guardian/internal/forge/author"
)

func setup(t *testing.T, policyText string) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	configPath, err := author.CreatePartition(author.PartitionOptions{Root: root, Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Dir(configPath)
	if _, err := author.CreateIntent(author.IntentOptions{
		PartitionDir: dir, Name: "api", Pusher: "k8s-main", Cluster: "k8s-main", Namespace: "demo",
	}); err != nil {
		t.Fatal(err)
	}
	if policyText != "" {
		path := filepath.Join(root, ".forge", "policy.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(policyText), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, dir
}

func TestCheckViolations(t *testing.T) {
	root, dir := setup(t, `version: 1
allowedPushers: [k8s-main]
require:
  labels: [role]
  probesFor: [Compute]
limits:
  replicas: 20
deny:
  images: ["*:latest"]
`)
	if _, err := author.AddAsset(dir, "api", assetdomain.Spec{
		Type: "Compute", Name: "api",
		Properties: map[string]any{"image": "ghcr.io/acme/api:latest", "replicas": 50},
	}); err != nil {
		t.Fatal(err)
	}

	list, err := Check([]string{dir}, PathFor(root))
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]int{}
	for _, d := range list {
		codes[d.Code]++
	}
	if codes["FGE4001"] < 4 {
		t.Fatalf("expected at least 4 policy violations, got %+v (%v)", codes, list)
	}
}

func TestCheckSecretRefs(t *testing.T) {
	root, dir := setup(t, `version: 1
require:
  secretRefs: true
`)
	if _, err := author.AddAsset(dir, "api", assetdomain.Spec{
		Type: "Compute", Name: "api",
		Properties: map[string]any{
			"image": "ghcr.io/acme/api:1",
			"env":   map[string]any{"API_TOKEN": "plaintext"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	list, err := Check([]string{dir}, PathFor(root))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range list {
		if d.Code == "FGE6001" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected FGE6001, got %+v", list)
	}
}

func TestCheckMissingPolicyIsClean(t *testing.T) {
	root, dir := setup(t, "")
	list, err := Check([]string{dir}, PathFor(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no diagnostics, got %+v", list)
	}
}
