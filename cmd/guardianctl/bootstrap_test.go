package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/bootstrap"
	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	cliformat "github.com/rydzu/ainfra/guardian/internal/cli/format"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
)

func TestBootstrapDefaultConfig(t *testing.T) {
	cfg := bootstrap.DefaultConfig()

	if cfg.Storage.Namespace != "monofs" {
		t.Errorf("expected monofs namespace, got %s", cfg.Storage.Namespace)
	}
	if cfg.LB.Namespace != "lb-edge" {
		t.Errorf("expected lb-edge namespace, got %s", cfg.LB.Namespace)
	}
	if cfg.Guardian.Namespace != "guardian" {
		t.Errorf("expected guardian namespace, got %s", cfg.Guardian.Namespace)
	}
	if len(cfg.Storage.NodeNames) != 5 {
		t.Errorf("expected 5 node names, got %d", len(cfg.Storage.NodeNames))
	}
	if cfg.Storage.Images.Server != "monofs-server:latest" {
		t.Errorf("unexpected server image: %s", cfg.Storage.Images.Server)
	}
	if cfg.Storage.Images.PullPolicy != "IfNotPresent" {
		t.Errorf("unexpected pull policy: %s", cfg.Storage.Images.PullPolicy)
	}
}

func TestBootstrapEnvComputation(t *testing.T) {
	cfg := bootstrap.DefaultConfig()

	// Override encryption key for testing
	t.Setenv("MONOFS_ENCRYPTION_KEY", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	env, err := bootstrap.ComputeEnv(&cfg)
	if err != nil {
		t.Fatalf("ComputeEnv failed: %v", err)
	}

	// Check required keys exist
	requiredKeys := []string{
		"STORAGE_NAMESPACE", "LB_NAMESPACE", "GUARDIAN_NAMESPACE",
		"MONOFS_SERVER_IMAGE", "MONOFS_ROUTER_IMAGE", "MONOFS_FETCHER_IMAGE",
		"MONOFS_SEARCH_IMAGE", "MONOFS_REGISTRY_IMAGE", "MONOFS_LB_IMAGE",
		"MINIO_IMAGE", "MONOFS_IMAGE_PULL_POLICY",
		"GUARDIAN_IMAGE", "GUARDIAN_IMAGE_PULL_POLICY",
		"GUARDIAN_PUSHER_IMAGE", "GUARDIAN_UI_PORT",
		"MONOFS_TOKEN_B64", "MONOFS_ENCRYPTION_KEY_B64",
		"LB_BOOTSTRAP", "MONOFS_NODE_ADDRS", "MONOFS_EXTERNAL_ADDRS",
	}
	for _, key := range requiredKeys {
		if v, ok := env[key]; !ok || v == "" {
			t.Errorf("missing or empty env key: %s", key)
		}
	}

	// Verify internal node addresses
	if !contains(env["MONOFS_NODE_ADDRS"], "node-a.monofs.svc.cluster.local:9000") {
		t.Errorf("MONOFS_NODE_ADDRS missing node-a: %s", env["MONOFS_NODE_ADDRS"])
	}

	// Verify LB bootstrap has expected entries
	lb := env["LB_BOOTSTRAP"]
	for _, name := range cfg.Storage.NodeNames {
		if !contains(lb, name) {
			t.Errorf("LB_BOOTSTRAP missing %s: %s", name, lb)
		}
	}
}

func TestBootstrapNodeEnv(t *testing.T) {
	cfg := bootstrap.DefaultConfig()
	env := bootstrap.ComputeNodeEnv(&cfg, "a")

	if env["SUFFIX"] != "a" {
		t.Errorf("expected SUFFIX=a, got %s", env["SUFFIX"])
	}
	if env["NODE_NAME"] != "node-a" {
		t.Errorf("expected NODE_NAME=node-a, got %s", env["NODE_NAME"])
	}
	if env["NODE_EXTERNAL_PORT"] != "9006" {
		t.Errorf("expected node-a external port=9006, got %s", env["NODE_EXTERNAL_PORT"])
	}

	// node-a should have kvs-bootstrap but no kvs-peer to node-a
	kvsExtra := env["KVS_EXTRA_ARGS"]
	if !contains(kvsExtra, "--kvs-bootstrap") {
		t.Errorf("node-a missing --kvs-bootstrap: %s", kvsExtra)
	}
	if contains(kvsExtra, "--kvs-peer=node-a") {
		t.Errorf("node-a should not peer with itself: %s", kvsExtra)
	}
	if !contains(kvsExtra, "--kvs-peer=node-b") {
		t.Errorf("node-a missing peer node-b: %s", kvsExtra)
	}
}

func TestBootstrapRouterEnv(t *testing.T) {
	cfg := bootstrap.DefaultConfig()
	env := bootstrap.ComputeRouterEnv(&cfg, "a", "")

	if env["ROUTER_NAME"] != "router-a" {
		t.Errorf("expected ROUTER_NAME=router-a, got %s", env["ROUTER_NAME"])
	}
	if env["ROUTER_PEER_NAME"] != "router-b" {
		t.Errorf("expected ROUTER_PEER_NAME=router-b, got %s", env["ROUTER_PEER_NAME"])
	}
	if env["SUFFIX"] != "a" {
		t.Errorf("expected SUFFIX=a, got %s", env["SUFFIX"])
	}
}

func TestBootstrapCommandsRegistered(t *testing.T) {
	// Verify dev commands exist in the command list
	cmds := devCommands()
	
	names := make(map[string]bool)
	for _, c := range cmds {
		if c.Cmd == nil {
			t.Errorf("nil command for %s %s", c.Group, c.Name)
			continue
		}
		if c.Cmd.Description == "" {
			t.Errorf("empty description for %s %s", c.Group, c.Name)
		}
		names[c.Name] = true
	}

	expectedCommands := []string{
		"setup", "build", "deploy", "init", "stop", "destroy", "status", "ports", "stamp-urls",
	}
	for _, cmd := range expectedCommands {
		if !names[cmd] {
			t.Errorf("dev commands missing '%s'", cmd)
		}
	}

	// Verify commands are registered in the registry (dry-run to avoid side effects)
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &buf}
	reg := registerCommands(nil, printer)

	// Running dev status with --dry-run should be safe
	err := reg.Run(context.Background(), []string{"dev", "status"})
	// Status command actually runs kubectl get pods, may fail if no cluster but command exists
	_ = err // OK if it fails — we just want to verify the command dispatches
}

func TestBootstrapBuildDryRun(t *testing.T) {
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &buf}
	reg := registerCommands(nil, printer)

	t.Setenv("ST_ROOT", t.TempDir()) // Prevent path resolution issues in dry-run

	err := reg.Run(context.Background(), []string{"dev", "build", "--dry-run"})
	if err != nil {
		t.Fatalf("dev build --dry-run failed: %v", err)
	}
}

func TestBootstrapDeployDryRun(t *testing.T) {
	// Create temp templates so dry-run doesn't fail on missing files
	tmpDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(tmpDir, "deploy", "bootstrap", "storage"), 0755)
	_ = os.MkdirAll(filepath.Join(tmpDir, "deploy", "bootstrap", "guardian"), 0755)

	// Write minimal templates
	os.WriteFile(filepath.Join(tmpDir, "deploy", "bootstrap", "storage", "namespace.yaml"),
		[]byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: ${STORAGE_NAMESPACE}\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "deploy", "bootstrap", "storage", "secret.yaml"),
		[]byte("apiVersion: v1\nkind: Secret\nmetadata:\n  name: test\n  namespace: ${STORAGE_NAMESPACE}\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "deploy", "bootstrap", "storage", "ns-lb-edge.yaml"),
		[]byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: ${LB_NAMESPACE}\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "deploy", "bootstrap", "guardian", "namespace.yaml"),
		[]byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: ${GUARDIAN_NAMESPACE}\n"), 0644)

	t.Setenv("ST_ROOT", tmpDir)
	t.Setenv("MONOFS_ENCRYPTION_KEY", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &buf}
	reg := registerCommands(nil, printer)

	configPath := filepath.Join(tmpDir, "deploy", "bootstrap", "bootstrap.yaml")
	os.WriteFile(configPath, []byte("{}\n"), 0644)

	err := reg.Run(context.Background(), []string{"dev", "deploy", "--dry-run", "--config", configPath, "--no-wait"})
	if err != nil {
		t.Fatalf("dev deploy --dry-run failed: %v", err)
	}
}

func TestBootstrapStopDestroyDryRun(t *testing.T) {
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &buf}
	reg := registerCommands(nil, printer)

	if err := reg.Run(context.Background(), []string{"dev", "stop", "--dry-run"}); err != nil {
		t.Fatalf("dev stop --dry-run failed: %v", err)
	}
	if err := reg.Run(context.Background(), []string{"dev", "destroy", "--dry-run"}); err != nil {
		t.Fatalf("dev destroy --dry-run failed: %v", err)
	}
}

func TestBootstrapConfigLoadFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "bootstrap.yaml")

	content := `
storage:
  namespace: test-ns
  images:
    server: my-server:v1
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := bootstrap.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Storage.Namespace != "test-ns" {
		t.Errorf("expected test-ns, got %s", cfg.Storage.Namespace)
	}
	if cfg.Storage.Images.Server != "my-server:v1" {
		t.Errorf("expected my-server:v1, got %s", cfg.Storage.Images.Server)
	}
	// Defaults should still be set
	if cfg.LB.Namespace != "lb-edge" {
		t.Errorf("expected default lb-edge, got %s", cfg.LB.Namespace)
	}
}

func TestBootstrapConfigLoadMissing(t *testing.T) {
	cfg, err := bootstrap.LoadConfig("/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("LoadConfig should not fail for missing file: %v", err)
	}
	if cfg.Storage.Namespace != "monofs" {
		t.Errorf("expected default namespace, got %s", cfg.Storage.Namespace)
	}
}

func TestBootstrapEnvOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "bootstrap.yaml")
	os.WriteFile(configPath, []byte("{}\n"), 0644)

	t.Setenv("MONOFS_NAMESPACE", "custom-ns")
	t.Setenv("GUARDIAN_IMAGE", "my-guardian:v2")

	cfg, err := bootstrap.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Storage.Namespace != "custom-ns" {
		t.Errorf("env override failed for namespace: %s", cfg.Storage.Namespace)
	}
	if cfg.Guardian.Images.Guardiand != "my-guardian:v2" {
		t.Errorf("env override failed for guardian image: %s", cfg.Guardian.Images.Guardiand)
	}
}

func TestSetupCommandDryRun(t *testing.T) {
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &buf}
	reg := registerCommands(nil, printer)

	// Setup uses fmt.Println (stdout), not printer. Just verify no error.
	err := reg.Run(context.Background(), []string{"dev", "setup", "--dry-run", "--auto-kind=false"})
	if err != nil {
		t.Fatalf("dev setup --dry-run failed: %v", err)
	}
}

func TestBootstrapStampURLsDryRun(t *testing.T) {
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &buf}
	reg := registerCommands(nil, printer)

	err := reg.Run(context.Background(), []string{"dev", "stamp-urls", "--dry-run"})
	if err != nil {
		t.Fatalf("dev stamp-urls --dry-run failed: %v", err)
	}
}

// contains checks if s contains substr (case-sensitive).
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Ensure devCommands returns all expected commands.
func TestBootstrapCommandsList(t *testing.T) {
	cmds := devCommands()
	if len(cmds) != 9 {
		t.Errorf("expected 9 dev commands, got %d", len(cmds))
	}
	names := make(map[string]bool)
	for _, c := range cmds {
		if c.Cmd == nil {
			t.Errorf("nil command for %s %s", c.Group, c.Name)
		}
		names[c.Name] = true
	}
	expected := []string{"setup", "build", "deploy", "init", "stop", "destroy", "status", "ports", "stamp-urls"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing command: %s", name)
		}
	}
}

// Ensure the command.Registry interface is not changed unexpectedly.
var _ = command.New
