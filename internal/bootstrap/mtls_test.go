package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureMTLSDryRunGeneratesBundle(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureMTLS(context.Background(), dir, false, false, true); err != nil {
		t.Fatalf("EnsureMTLS: %v", err)
	}
	for _, name := range []string{"ca.crt", "ca.key", "bundle.json", "guardianctl.env"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	for _, name := range []string{"monofs-router", "monofs-server", "monofs-search", "monofs-fetcher", "guardiand", "doctor-query"} {
		for _, file := range []string{"tls.crt", "tls.key", "ca.crt"} {
			if _, err := os.Stat(filepath.Join(dir, name, file)); err != nil {
				t.Fatalf("missing %s/%s: %v", name, file, err)
			}
		}
	}
	env, err := os.ReadFile(filepath.Join(dir, "guardianctl.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "STRATA_TLS_MODE=optional") ||
		!strings.Contains(string(env), "/guardiand/tls.crt") {
		t.Fatalf("unexpected env file:\n%s", env)
	}
}

func TestInjectMTLSDryRun(t *testing.T) {
	cfg := &Config{}
	cfg.Storage.Namespace = "monofs"
	cfg.Guardian.Namespace = "guardian"
	if err := InjectMTLS(context.Background(), cfg, true); err != nil {
		t.Fatalf("InjectMTLS dry-run: %v", err)
	}
}

func TestEnsureMTLSReusesCA(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureMTLS(context.Background(), dir, false, false, true); err != nil {
		t.Fatal(err)
	}
	caBefore, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureMTLS(context.Background(), dir, false, false, true); err != nil {
		t.Fatal(err)
	}
	caAfter, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(caBefore) != string(caAfter) {
		t.Fatal("CA changed without new-ca")
	}
}
