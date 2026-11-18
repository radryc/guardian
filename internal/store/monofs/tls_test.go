package monofs

import (
	"os"
	"path/filepath"
	"testing"
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
