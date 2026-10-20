package bootstrap

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultRouterAutoRefreshSettings(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Storage.Router.AutoRefresh != "true" {
		t.Fatalf("autoRefresh = %q, want true", cfg.Storage.Router.AutoRefresh)
	}
	if cfg.Storage.Router.AutoRefreshShards != "2" {
		t.Fatalf("shards = %q, want 2", cfg.Storage.Router.AutoRefreshShards)
	}
	if cfg.Storage.Router.AutoRefreshLease != "true" {
		t.Fatalf("lease = %q, want true", cfg.Storage.Router.AutoRefreshLease)
	}
}

func TestComputeRouterEnvAssignsShardBySuffix(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Storage.RouterSuffixes = []string{"a", "b"}

	first := ComputeRouterEnv(&cfg, "a", "")
	second := ComputeRouterEnv(&cfg, "b", "")
	if first["MONOFS_AUTO_REFRESH_SHARD"] != "0" {
		t.Fatalf("router-a shard = %q, want 0", first["MONOFS_AUTO_REFRESH_SHARD"])
	}
	if second["MONOFS_AUTO_REFRESH_SHARD"] != "1" {
		t.Fatalf("router-b shard = %q, want 1", second["MONOFS_AUTO_REFRESH_SHARD"])
	}
}

func TestRenderRouterWatcherWiring(t *testing.T) {
	if _, err := exec.LookPath("envsubst"); err != nil {
		t.Skip("envsubst is not installed")
	}
	cfg := DefaultConfig()
	env, err := ComputeEnv(&cfg)
	if err != nil {
		t.Fatalf("ComputeEnv: %v", err)
	}
	storageDir, _ := TemplateDirs()
	rendered, err := RenderTemplate(
		filepath.Join(storageDir, "deploy-router.yaml"),
		env,
		ComputeRouterEnv(&cfg, "a", ""),
	)
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	wants := []string{
		"--auto-refresh-shards=2",
		"--auto-refresh-shard=0",
		"--auto-refresh-batch-size=32",
		"--auto-refresh-lease=true",
		"--auto-refresh-state-dir=/var/lib/monofs/repowatch",
		"name: POD_NAME",
		"name: POD_NAMESPACE",
		"name: repowatch",
	}
	for _, want := range wants {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered router manifest is missing %q", want)
		}
	}
}

func TestRenderGuardianRepoImportWiring(t *testing.T) {
	if _, err := exec.LookPath("envsubst"); err != nil {
		t.Skip("envsubst is not installed")
	}
	cfg := DefaultConfig()
	env, err := ComputeEnv(&cfg)
	if err != nil {
		t.Fatalf("ComputeEnv: %v", err)
	}
	_, guardianDir := TemplateDirs()
	rendered, err := RenderTemplate(filepath.Join(guardianDir, "deploy-guardiand.yaml"), env, nil)
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	wants := []string{
		"GUARDIAN_REPO_IMPORT_SHARDS",
		"GUARDIAN_REPO_IMPORT_LEASE",
		"GUARDIAN_REPO_IMPORT_LEASE_NAME",
		"name: POD_NAME",
	}
	for _, want := range wants {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered guardiand manifest is missing %q", want)
		}
	}
}
