package bootstrap

import "testing"

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
