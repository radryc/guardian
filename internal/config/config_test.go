package config

import "testing"

func TestMonoFSConfigDiscoveryUseExternalAddressesDefaultsToStoreSetting(t *testing.T) {
	cfg := MonoFSConfig{UseExternalAddresses: true}
	if !cfg.DiscoveryUseExternalAddresses() {
		t.Fatalf("expected discovery to inherit store useExternalAddresses when client override is unset")
	}
}

func TestMonoFSConfigDiscoveryUseExternalAddressesUsesClientOverride(t *testing.T) {
	override := true
	cfg := MonoFSConfig{
		UseExternalAddresses:       false,
		ClientUseExternalAddresses: &override,
	}
	if !cfg.DiscoveryUseExternalAddresses() {
		t.Fatalf("expected discovery override to win over store useExternalAddresses")
	}
}

func TestDefaultEnablesAdaptiveReconcilePacing(t *testing.T) {
	cfg := Default()
	if !cfg.Guardian.ReconcileAdaptive {
		t.Fatal("adaptive reconcile pacing should be enabled by default")
	}
}
