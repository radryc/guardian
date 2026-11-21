// Package tlsxutil adapts the shared STRATA_TLS_* environment contract from
// github.com/radryc/tlsx to Guardian's servers and clients.
//
// The helper keeps every pre-existing TLS flag working. Legacy settings win
// when STRATA_TLS_MODE is off (the default); when a STRATA_TLS_* mode is
// selected, the shared variables take precedence and mutual TLS with SPIFFE
// verification becomes available.
package tlsxutil

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/radryc/tlsx"
)

// LegacyServer describes certificates configured with service-specific flags or
// environment variables (for example GUARDIAN_UI_TLS_CERT_FILE).
type LegacyServer struct {
	CertFile          string
	KeyFile           string
	CAFile            string
	RequireClientCert bool
}

// LegacyClient describes a service-specific TLS client configuration (for
// example GUARDIAN_MONOFS_TLS_*).
type LegacyClient struct {
	CertFile   string
	KeyFile    string
	CAFile     string
	ServerName string
}

// Env loads STRATA_TLS_*.
func Env() (tlsx.EnvConfig, error) {
	return tlsx.LoadEnv()
}

// ServerConfig resolves the server TLS settings, preferring STRATA_TLS_* over
// legacy flags whenever a non-off mode is selected.
//
// It returns enabled=false when the caller should keep serving plaintext: the
// mode is off and no legacy certificate was supplied, or the mode is optional
// and no certificate is configured.
func ServerConfig(legacy LegacyServer) (cfg tlsx.ServerConfig, enabled bool, err error) {
	env, err := Env()
	if err != nil {
		return tlsx.ServerConfig{}, false, err
	}

	legacyCert := strings.TrimSpace(legacy.CertFile)
	legacyKey := strings.TrimSpace(legacy.KeyFile)
	legacyCA := strings.TrimSpace(legacy.CAFile)

	if env.Mode == tlsx.ModeOff {
		if legacyCert == "" && legacyKey == "" {
			return tlsx.ServerConfig{}, false, nil
		}
		if (legacyCert == "") != (legacyKey == "") {
			return tlsx.ServerConfig{}, false, fmt.Errorf("tlsx: server TLS requires both a certificate and a key")
		}
		return tlsx.ServerConfig{
			CertFile:          legacyCert,
			KeyFile:           legacyKey,
			CAFile:            legacyCA,
			RequireClientCert: legacy.RequireClientCert || legacyCA != "",
		}, true, nil
	}

	certFile := firstNonEmpty(strings.TrimSpace(os.Getenv("STRATA_TLS_CERT_FILE")), legacyCert, env.CertFile)
	keyFile := firstNonEmpty(strings.TrimSpace(os.Getenv("STRATA_TLS_KEY_FILE")), legacyKey, env.KeyFile)
	caFile := firstNonEmpty(strings.TrimSpace(os.Getenv("STRATA_TLS_CA_FILE")), legacyCA, env.CAFile)

	if env.Mode == tlsx.ModeOptional && strings.TrimSpace(os.Getenv("STRATA_TLS_CERT_FILE")) == "" && legacyCert == "" {
		if _, statErr := os.Stat(certFile); statErr != nil {
			// Optional mode keeps the plaintext listener when no certificate
			// has been provisioned.
			return tlsx.ServerConfig{}, false, nil
		}
	}
	if certFile == "" || keyFile == "" {
		return tlsx.ServerConfig{}, false, fmt.Errorf("tlsx: STRATA_TLS_MODE=%s requires STRATA_TLS_CERT_FILE and STRATA_TLS_KEY_FILE", env.Mode)
	}

	cfg = env.ServerConfig()
	cfg.CertFile = certFile
	cfg.KeyFile = keyFile
	cfg.CAFile = caFile
	cfg.RequireClientCert = env.RequireClientCert || legacy.RequireClientCert
	return cfg, true, nil
}

// ClientConfig resolves a TLS client configuration, preferring STRATA_TLS_*
// over legacy settings whenever a non-off mode is selected.
//
// It returns enabled=false only when the mode is off and no legacy TLS settings
// exist, meaning callers should keep their plaintext behavior.
func ClientConfig(legacy LegacyClient) (cfg tlsx.ClientConfig, enabled bool, err error) {
	env, err := Env()
	if err != nil {
		return tlsx.ClientConfig{}, false, err
	}

	legacyCert := strings.TrimSpace(legacy.CertFile)
	legacyKey := strings.TrimSpace(legacy.KeyFile)
	legacyCA := strings.TrimSpace(legacy.CAFile)
	legacyName := strings.TrimSpace(legacy.ServerName)

	if env.Mode == tlsx.ModeOff {
		if legacyCert == "" && legacyKey == "" && legacyCA == "" && legacyName == "" {
			return tlsx.ClientConfig{}, false, nil
		}
		if (legacyCert == "") != (legacyKey == "") {
			return tlsx.ClientConfig{}, false, fmt.Errorf("tlsx: client TLS requires both a certificate and a key")
		}
		return tlsx.ClientConfig{
			CertFile:   legacyCert,
			KeyFile:    legacyKey,
			CAFile:     legacyCA,
			ServerName: legacyName,
		}, true, nil
	}

	certFile := firstNonEmpty(strings.TrimSpace(os.Getenv("STRATA_TLS_CERT_FILE")), legacyCert, env.CertFile)
	keyFile := firstNonEmpty(strings.TrimSpace(os.Getenv("STRATA_TLS_KEY_FILE")), legacyKey, env.KeyFile)
	caFile := firstNonEmpty(strings.TrimSpace(os.Getenv("STRATA_TLS_CA_FILE")), legacyCA, env.CAFile)
	serverName := firstNonEmpty(env.ServerName, legacyName)

	explicitCert := strings.TrimSpace(os.Getenv("STRATA_TLS_CERT_FILE")) != "" || legacyCert != ""
	if !explicitCert {
		// Without an explicitly configured workload certificate, fall back to
		// one-way TLS rather than failing on a missing default mount.
		if _, statErr := os.Stat(certFile); statErr != nil {
			certFile, keyFile = "", ""
		}
	}
	if (certFile == "") != (keyFile == "") {
		return tlsx.ClientConfig{}, false, fmt.Errorf("tlsx: client TLS requires both a certificate and a key")
	}

	explicitCA := strings.TrimSpace(os.Getenv("STRATA_TLS_CA_FILE")) != "" || legacyCA != ""
	if !explicitCA {
		if _, statErr := os.Stat(caFile); statErr != nil {
			// No local CA bundle: verify servers against the system roots.
			caFile = ""
		}
	}

	cfg = env.ClientConfig()
	cfg.CertFile = certFile
	cfg.KeyFile = keyFile
	cfg.CAFile = caFile
	cfg.ServerName = serverName
	return cfg, true, nil
}

// ServerTLSConfigHTTP builds a *tls.Config for a Guardian HTTP server.
//
// Optional mode keeps HTTP plaintext so local UIs, port-forwards and probes
// keep working while gRPC uses mTLS; required mode enables TLS for HTTP too.
// An explicit legacy certificate is honored as an opt-in.
func ServerTLSConfigHTTP(legacy LegacyServer) (*tls.Config, bool, error) {
	env, err := Env()
	if err != nil {
		return nil, false, err
	}
	if env.Mode == tlsx.ModeOptional && strings.TrimSpace(legacy.CertFile) == "" {
		return nil, false, nil
	}
	return ServerTLSConfig(legacy)
}

// ServerTLSConfig builds a *tls.Config for a Guardian server.
func ServerTLSConfig(legacy LegacyServer) (*tls.Config, bool, error) {
	cfg, enabled, err := ServerConfig(legacy)
	if err != nil || !enabled {
		return nil, enabled, err
	}
	tlsCfg, err := tlsx.NewServerTLSConfig(cfg)
	if err != nil {
		return nil, false, err
	}
	return tlsCfg, true, nil
}

// ClientTLSConfig builds a *tls.Config for a Guardian client.
func ClientTLSConfig(legacy LegacyClient) (*tls.Config, bool, error) {
	cfg, enabled, err := ClientConfig(legacy)
	if err != nil || !enabled {
		return nil, enabled, err
	}
	tlsCfg, err := tlsx.NewClientTLSConfig(cfg)
	if err != nil {
		return nil, false, err
	}
	return tlsCfg, true, nil
}

// HTTPClient builds an *http.Client for outbound connections to Strata
// services. enabled=false means the caller should keep the default HTTP
// client (STRATA_TLS_MODE=off without a CA).
func HTTPClient(legacy LegacyClient, timeout time.Duration) (*http.Client, bool, error) {
	tlsCfg, enabled, err := ClientTLSConfig(legacy)
	if err != nil || !enabled {
		return nil, enabled, err
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       tlsCfg,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{Transport: transport, Timeout: timeout}, true, nil
}

// HTTPSRequired reports whether STRATA_TLS_MODE=required, which means outbound
// URLs must be upgraded from http:// to https://.
func HTTPSRequired() bool {
	env, err := Env()
	return err == nil && env.Mode == tlsx.ModeRequired
}

// UpgradeURL rewrites an http:// URL to https:// when TLS is required.
func UpgradeURL(raw string, tlsRequired bool) string {
	if !tlsRequired {
		return raw
	}
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(trimmed), "http://") {
		return "https://" + trimmed[len("http://"):]
	}
	return raw
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
