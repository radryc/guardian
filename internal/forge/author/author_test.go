package author

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
)

func setupPartition(t *testing.T) (root, dir, intentPath string) {
	t.Helper()
	root = t.TempDir()
	configPath, err := CreatePartition(PartitionOptions{Root: root, Name: "demo"})
	if err != nil {
		t.Fatalf("CreatePartition: %v", err)
	}
	dir = filepath.Dir(configPath)
	intentPath, err = CreateIntent(IntentOptions{
		PartitionDir: dir, Name: "api", Pusher: "k8s-main",
		Cluster: "k8s-main", Namespace: "demo",
	})
	if err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}
	return root, dir, intentPath
}

func TestAuthoringPreservesComments(t *testing.T) {
	_, dir, intentPath := setupPartition(t)

	// A comment that must survive every node edit.
	original, err := os.ReadFile(intentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(intentPath, append([]byte("# keep this comment\n"), original...), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := AddAsset(dir, "api", assetdomain.Spec{
		Type: "Compute", Name: "api",
		Properties: map[string]any{
			"image": "ghcr.io/example/api:1",
			"ports": []map[string]any{{"name": "http", "containerPort": 8080, "protocol": "TCP"}},
		},
	}); err != nil {
		t.Fatalf("AddAsset Compute: %v", err)
	}
	if _, err := AddAsset(dir, "api", assetdomain.Spec{
		Type: "Config", Name: "settings",
		Properties: map[string]any{"content": "logLevel: info\n"},
	}); err != nil {
		t.Fatalf("AddAsset Config: %v", err)
	}
	if _, err := ConnectAssets(dir, "api", "api", "settings"); err != nil {
		t.Fatalf("ConnectAssets: %v", err)
	}
	if _, err := AddSecretRef(dir, "api", "api", "DATABASE_URL", "monofs-secret://demo/db"); err != nil {
		t.Fatalf("AddSecretRef: %v", err)
	}
	if _, err := AddProbe(dir, "api", assetdomain.Spec{
		Type: "Probe", Name: "api-probe",
		Properties: map[string]any{"type": "http", "target": "api", "path": "/healthz", "port": 8080},
	}); err != nil {
		t.Fatalf("AddProbe: %v", err)
	}

	data, err := os.ReadFile(intentPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"# keep this comment",
		"name: api",
		"dependsOn:",
		"settings",
		"secret_ref: monofs-secret://demo/db",
		"type: Probe",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("intent missing %q:\n%s", want, text)
		}
	}
}

func TestConnectAssetsRejectsCycle(t *testing.T) {
	_, dir, _ := setupPartition(t)
	for _, name := range []string{"a", "b"} {
		if _, err := AddAsset(dir, "api", assetdomain.Spec{
			Type: "Config", Name: name,
			Properties: map[string]any{"content": "x: y\n"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ConnectAssets(dir, "api", "a", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := ConnectAssets(dir, "api", "b", "a"); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestAddAssetRejectsDuplicate(t *testing.T) {
	_, dir, _ := setupPartition(t)
	asset := assetdomain.Spec{Type: "Config", Name: "cfg", Properties: map[string]any{"content": "a: b\n"}}
	if _, err := AddAsset(dir, "api", asset); err != nil {
		t.Fatal(err)
	}
	if _, err := AddAsset(dir, "api", asset); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestSuggestAlerts(t *testing.T) {
	_, dir, _ := setupPartition(t)
	if _, err := AddAsset(dir, "api", assetdomain.Spec{
		Type: "Compute", Name: "api", Properties: map[string]any{"image": "x:1"},
	}); err != nil {
		t.Fatal(err)
	}
	data, err := SuggestAlerts(dir)
	if err != nil {
		t.Fatalf("SuggestAlerts: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"version: 1",
		"partition: demo",
		"DemoProbeDown",
		`guardian_partition="demo"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("alerts missing %q:\n%s", want, text)
		}
	}
}
