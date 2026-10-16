package awssetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testCertPaths(t *testing.T) CertPaths {
	t.Helper()
	dir := t.TempDir()
	return CertPaths{
		CADir:     dir,
		CA:        filepath.Join(dir, "ca.pem"),
		Client:    filepath.Join(dir, "client.pem"),
		ClientKey: filepath.Join(dir, "client-key.pem"),
	}
}

func TestDefaultCertPathsUseClientKeyPem(t *testing.T) {
	paths := DefaultCertPaths()
	if !strings.HasSuffix(paths.ClientKey, "client-key.pem") {
		t.Fatalf("ClientKey = %q, want client-key.pem", paths.ClientKey)
	}
	if paths.clientKeyPath() != paths.ClientKey {
		t.Fatalf("clientKeyPath() = %q, want %q", paths.clientKeyPath(), paths.ClientKey)
	}
}

func TestEnsureCertsGeneratesThenReuses(t *testing.T) {
	paths := testCertPaths(t)

	ca1, client1, key1, generated, err := EnsureCerts(paths, false)
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}
	if !generated {
		t.Fatal("expected generation on an empty cert dir")
	}
	if ca1 == "" || client1 == "" || key1 == "" {
		t.Fatal("expected non-empty PEM values")
	}
	for _, path := range []string{paths.CA, paths.Client, paths.ClientKey} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}

	ca2, client2, key2, generated, err := EnsureCerts(paths, false)
	if err != nil {
		t.Fatalf("second EnsureCerts: %v", err)
	}
	if generated {
		t.Fatal("expected reuse of valid certificates")
	}
	if ca2 != ca1 || client2 != client1 || key2 != key1 {
		t.Fatal("expected reused certificates to be identical")
	}
}

func TestEnsureCertsRegeneratesWhenClientKeyMissing(t *testing.T) {
	paths := testCertPaths(t)
	ca1, _, _, _, err := EnsureCerts(paths, false)
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}

	if err := os.Remove(paths.ClientKey); err != nil {
		t.Fatal(err)
	}
	ca2, client2, key2, generated, err := EnsureCerts(paths, false)
	if err != nil {
		t.Fatalf("EnsureCerts after key removal: %v", err)
	}
	if !generated {
		t.Fatal("expected regeneration when the client key is missing")
	}
	if ca2 == ca1 {
		t.Fatal("expected a fresh CA when regenerating")
	}
	if client2 == "" || key2 == "" {
		t.Fatal("expected non-empty client certificate and key")
	}
	if !certsUsable(paths) {
		t.Fatal("regenerated certificate set is not usable")
	}
}

func TestEnsureCertsRegeneratesWhenCorrupt(t *testing.T) {
	paths := testCertPaths(t)
	ca1, _, _, _, err := EnsureCerts(paths, false)
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}

	if err := os.WriteFile(paths.Client, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	ca2, _, _, generated, err := EnsureCerts(paths, false)
	if err != nil {
		t.Fatalf("EnsureCerts after corruption: %v", err)
	}
	if !generated {
		t.Fatal("expected regeneration for a corrupt client certificate")
	}
	if ca2 == ca1 {
		t.Fatal("expected a fresh CA when regenerating")
	}
	if !certsUsable(paths) {
		t.Fatal("regenerated certificate set is not usable")
	}
}

func TestEnsureCertsForceNew(t *testing.T) {
	paths := testCertPaths(t)
	if _, _, _, _, err := EnsureCerts(paths, false); err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}
	_, _, _, generated, err := EnsureCerts(paths, true)
	if err != nil {
		t.Fatalf("EnsureCerts(forceNew): %v", err)
	}
	if !generated {
		t.Fatal("expected generation with forceNew")
	}
}
