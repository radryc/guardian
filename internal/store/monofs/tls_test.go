package monofs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/radryc/tlsx/pki"
)

// clearMonoFSTLSEnv pins every transport-related variable so the developer's
// shell cannot influence credential selection.
func clearMonoFSTLSEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GUARDIAN_MONOFS_TLS",
		"GUARDIAN_MONOFS_ALLOW_PLAINTEXT",
		"GUARDIAN_MONOFS_TLS_CA_FILE",
		"GUARDIAN_MONOFS_CA_FILE",
		"GUARDIAN_MONOFS_TLS_CLIENT_CERT_FILE",
		"GUARDIAN_MONOFS_CLIENT_CERT_FILE",
		"CLIENT_CERT_FILE",
		"GUARDIAN_MONOFS_TLS_CLIENT_KEY_FILE",
		"GUARDIAN_MONOFS_CLIENT_KEY_FILE",
		"CLIENT_KEY_FILE",
		"GUARDIAN_MONOFS_TLS_SERVER_NAME",
		"GUARDIAN_MONOFS_SERVER_NAME",
		"SERVER_NAME",
		"STRATA_TLS_MODE",
		"STRATA_TLS_CERT_FILE",
		"STRATA_TLS_KEY_FILE",
		"STRATA_TLS_CA_FILE",
		"STRATA_TLS_SERVER_NAME",
		"STRATA_TLS_REQUIRE_CLIENT_CERT",
		"STRATA_TLS_ALLOWED_SPIFFE_IDS",
		"STRATA_TLS_EXPECTED_SPIFFE_IDS",
	} {
		t.Setenv(key, "")
	}
}

func TestMonoFSTransportCredentialsRequiresTLSByDefault(t *testing.T) {
	clearMonoFSTLSEnv(t)

	if _, err := monoFSTransportCredentials(); err == nil {
		t.Fatal("plaintext transport must be refused while a token is sent")
	}
}

func TestMonoFSTransportCredentialsAllowsExplicitPlaintext(t *testing.T) {
	clearMonoFSTLSEnv(t)
	t.Setenv("GUARDIAN_MONOFS_ALLOW_PLAINTEXT", "true")

	creds, err := monoFSTransportCredentials()
	if err != nil {
		t.Fatalf("explicit plaintext opt-in failed: %v", err)
	}
	if got := creds.Info().SecurityProtocol; got != "insecure" {
		t.Fatalf("security protocol = %q, want insecure", got)
	}
}

func TestMonoFSTransportCredentialsTLSWithSystemRoots(t *testing.T) {
	clearMonoFSTLSEnv(t)
	t.Setenv("GUARDIAN_MONOFS_TLS", "true")
	t.Setenv("GUARDIAN_MONOFS_TLS_SERVER_NAME", "monofs.internal")

	creds, err := monoFSTransportCredentials()
	if err != nil {
		t.Fatalf("TLS credentials: %v", err)
	}
	info := creds.Info()
	if info.SecurityProtocol != "tls" {
		t.Fatalf("security protocol = %q, want tls", info.SecurityProtocol)
	}
	if info.ServerName != "monofs.internal" {
		t.Fatalf("server name = %q, want monofs.internal", info.ServerName)
	}
}

func TestMonoFSTransportCredentialsInvalidCA(t *testing.T) {
	clearMonoFSTLSEnv(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caPath, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}
	t.Setenv("GUARDIAN_MONOFS_TLS_CA_FILE", caPath)

	if _, err := monoFSTransportCredentials(); err == nil {
		t.Fatal("invalid CA bundle must be rejected")
	}
}

func TestMonoFSTransportCredentialsRequiresCertAndKeyTogether(t *testing.T) {
	clearMonoFSTLSEnv(t)
	t.Setenv("GUARDIAN_MONOFS_TLS", "true")
	t.Setenv("CLIENT_CERT_FILE", filepath.Join(t.TempDir(), "client.pem"))

	if _, err := monoFSTransportCredentials(); err == nil {
		t.Fatal("client certificate without key must be rejected")
	}
}

func TestMonoFSTransportCredentialsSTRATAFallback(t *testing.T) {
	clearMonoFSTLSEnv(t)
	bundle, err := pki.Generate(pki.Options{
		Dir: t.TempDir(),
		Services: []pki.Service{{
			Name:      "monofs",
			Namespace: "guardian",
			SPIFFEID:  "spiffe://strata.local/ns/guardian/sa/monofs",
		}},
	})
	if err != nil {
		t.Fatalf("pki.Generate: %v", err)
	}
	t.Setenv("STRATA_TLS_MODE", "required")
	t.Setenv("STRATA_TLS_CA_FILE", bundle.CAFile)
	t.Setenv("STRATA_TLS_SERVER_NAME", "monofs.internal")

	creds, err := monoFSTransportCredentials()
	if err != nil {
		t.Fatalf("STRATA fallback credentials: %v", err)
	}
	info := creds.Info()
	if info.SecurityProtocol != "tls" {
		t.Fatalf("security protocol = %q, want tls", info.SecurityProtocol)
	}
	if info.ServerName != "monofs.internal" {
		t.Fatalf("server name = %q, want monofs.internal", info.ServerName)
	}
}

func TestMonoFSTransportCredentialsSTRATARequiredIgnoresPlaintextOptIn(t *testing.T) {
	clearMonoFSTLSEnv(t)
	bundle, err := pki.Generate(pki.Options{
		Dir: t.TempDir(),
		Services: []pki.Service{{
			Name:      "monofs",
			Namespace: "guardian",
		}},
	})
	if err != nil {
		t.Fatalf("pki.Generate: %v", err)
	}
	t.Setenv("STRATA_TLS_MODE", "required")
	t.Setenv("STRATA_TLS_CA_FILE", bundle.CAFile)
	t.Setenv("GUARDIAN_MONOFS_ALLOW_PLAINTEXT", "true")

	creds, err := monoFSTransportCredentials()
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	if got := creds.Info().SecurityProtocol; got == "insecure" {
		t.Fatal("STRATA_TLS_MODE=required must never fall back to plaintext")
	}
}
