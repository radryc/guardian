package tlsxutil

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/radryc/tlsx"
	"github.com/radryc/tlsx/pki"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	serverSPIFFEID = "spiffe://strata.local/ns/guardian/sa/guardian"
	clientSPIFFEID = "spiffe://strata.local/ns/guardian/sa/client"
)

var tlsEnvKeys = []string{
	"STRATA_TLS_MODE",
	"STRATA_TLS_CERT_FILE",
	"STRATA_TLS_KEY_FILE",
	"STRATA_TLS_CA_FILE",
	"STRATA_TLS_SERVER_NAME",
	"STRATA_TLS_REQUIRE_CLIENT_CERT",
	"STRATA_TLS_ALLOWED_SPIFFE_IDS",
	"STRATA_TLS_EXPECTED_SPIFFE_IDS",
}

func clearTLSEnv(t *testing.T) {
	t.Helper()
	for _, key := range tlsEnvKeys {
		t.Setenv(key, "")
	}
}

func generatePKI(t *testing.T) *pki.Bundle {
	t.Helper()
	bundle, err := pki.Generate(pki.Options{
		Dir: t.TempDir(),
		Services: []pki.Service{
			{
				Name:      "guardian",
				Namespace: "guardian",
				DNSNames:  []string{"localhost"},
				IPs:       []string{"127.0.0.1"},
				SPIFFEID:  serverSPIFFEID,
			},
			{
				Name:      "client",
				Namespace: "guardian",
				DNSNames:  []string{"localhost"},
				IPs:       []string{"127.0.0.1"},
				SPIFFEID:  clientSPIFFEID,
			},
		},
	})
	if err != nil {
		t.Fatalf("pki.Generate: %v", err)
	}
	return bundle
}

func applyRequiredEnv(t *testing.T, bundle *pki.Bundle) {
	t.Helper()
	clearTLSEnv(t)
	t.Setenv("STRATA_TLS_MODE", "required")
	t.Setenv("STRATA_TLS_CA_FILE", bundle.CAFile)
	t.Setenv("STRATA_TLS_SERVER_NAME", "localhost")
	t.Setenv("STRATA_TLS_REQUIRE_CLIENT_CERT", "true")
}

func listenTLS(t *testing.T, handler http.Handler, tlsConfig *tls.Config) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		_ = server.Serve(tls.NewListener(listener, tlsConfig))
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	return listener.Addr().String()
}

func TestHTTPMTLSRoundTrip(t *testing.T) {
	bundle := generatePKI(t)
	applyRequiredEnv(t, bundle)
	server := bundle.Services["guardian"]
	client := bundle.Services["client"]

	serverTLS, enabled, err := ServerTLSConfig(LegacyServer{
		CertFile:          server.CertFile,
		KeyFile:           server.KeyFile,
		CAFile:            bundle.CAFile,
		RequireClientCert: true,
	})
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}
	if !enabled {
		t.Fatal("server TLS should be enabled in required mode")
	}
	addr := listenTLS(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}), serverTLS)

	httpClient, clientEnabled, err := HTTPClient(LegacyClient{
		CertFile:   client.CertFile,
		KeyFile:    client.KeyFile,
		CAFile:     bundle.CAFile,
		ServerName: "localhost",
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("HTTPClient: %v", err)
	}
	if !clientEnabled {
		t.Fatal("client TLS should be enabled in required mode")
	}

	resp, err := httpClient.Get("https://" + addr + "/")
	if err != nil {
		t.Fatalf("mTLS GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}
}

func TestRequiredModeRejectsPlaintextAndUntrustedPeers(t *testing.T) {
	bundle := generatePKI(t)
	applyRequiredEnv(t, bundle)
	server := bundle.Services["guardian"]
	client := bundle.Services["client"]

	serverTLS, _, err := ServerTLSConfig(LegacyServer{
		CertFile:          server.CertFile,
		KeyFile:           server.KeyFile,
		CAFile:            bundle.CAFile,
		RequireClientCert: true,
	})
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}
	addr := listenTLS(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}), serverTLS)

	t.Run("plaintext HTTP against TLS listener", func(t *testing.T) {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode == http.StatusOK || string(body) == "ok" {
				t.Fatalf("plaintext request reached the TLS handler: status=%d body=%q", resp.StatusCode, body)
			}
		}
	})

	t.Run("client without certificate", func(t *testing.T) {
		unauthenticated, _, err := HTTPClient(LegacyClient{
			CAFile:     bundle.CAFile,
			ServerName: "localhost",
		}, 5*time.Second)
		if err != nil {
			t.Fatalf("HTTPClient: %v", err)
		}
		if _, err := unauthenticated.Get("https://" + addr + "/"); err == nil {
			t.Fatal("request without a client certificate unexpectedly succeeded")
		}
	})

	t.Run("client trusting another CA", func(t *testing.T) {
		other := generatePKI(t)
		t.Setenv("STRATA_TLS_CA_FILE", other.CAFile)
		untrusted, _, err := HTTPClient(LegacyClient{
			CertFile:   client.CertFile,
			KeyFile:    client.KeyFile,
			CAFile:     other.CAFile,
			ServerName: "localhost",
		}, 5*time.Second)
		if err != nil {
			t.Fatalf("HTTPClient: %v", err)
		}
		if _, err := untrusted.Get("https://" + addr + "/"); err == nil {
			t.Fatal("request with an untrusted CA unexpectedly succeeded")
		}
	})
}

func TestModeOffKeepsLegacyBehavior(t *testing.T) {
	clearTLSEnv(t)

	cfg, enabled, err := ServerTLSConfig(LegacyServer{})
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}
	if enabled || cfg != nil {
		t.Fatal("TLS must stay disabled when STRATA_TLS_MODE=off and no legacy certificate is supplied")
	}

	clientCfg, clientEnabled, err := ClientConfig(LegacyClient{})
	if err != nil {
		t.Fatalf("ClientConfig: %v", err)
	}
	if clientEnabled || clientCfg.CertFile != "" || clientCfg.CAFile != "" {
		t.Fatal("client TLS must stay disabled when STRATA_TLS_MODE=off")
	}

	bundle := generatePKI(t)
	legacy := LegacyServer{
		CertFile: bundle.Services["guardian"].CertFile,
		KeyFile:  bundle.Services["guardian"].KeyFile,
	}
	if _, enabled, err := ServerTLSConfig(legacy); err != nil || !enabled {
		t.Fatalf("legacy certificate must still enable TLS: enabled=%v err=%v", enabled, err)
	}
}

func TestUpgradeURL(t *testing.T) {
	if got := UpgradeURL("http://pusher:19090", true); got != "https://pusher:19090" {
		t.Fatalf("UpgradeURL = %q", got)
	}
	if got := UpgradeURL("http://pusher:19090", false); got != "http://pusher:19090" {
		t.Fatalf("UpgradeURL = %q", got)
	}
	if got := UpgradeURL("https://pusher:19090", true); got != "https://pusher:19090" {
		t.Fatalf("UpgradeURL = %q", got)
	}
}

func TestGRPCMTLSRoundTrip(t *testing.T) {
	bundle := generatePKI(t)
	applyRequiredEnv(t, bundle)
	server := bundle.Services["guardian"]
	client := bundle.Services["client"]

	serverTLS, _, err := ServerTLSConfig(LegacyServer{
		CertFile:          server.CertFile,
		KeyFile:           server.KeyFile,
		CAFile:            bundle.CAFile,
		RequireClientCert: true,
	})
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	healthpb.RegisterHealthServer(grpcServer, health.NewServer())
	go func() {
		_ = grpcServer.Serve(listener)
	}()
	t.Cleanup(grpcServer.Stop)

	clientTLS, err := tlsx.NewClientTLSConfig(tlsx.ClientConfig{
		CertFile:   client.CertFile,
		KeyFile:    client.KeyFile,
		CAFile:     bundle.CAFile,
		ServerName: "localhost",
	})
	if err != nil {
		t.Fatalf("NewClientTLSConfig: %v", err)
	}
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatalf("gRPC health check over mTLS: %v", err)
	}

	t.Run("untrusted client is rejected", func(t *testing.T) {
		other := generatePKI(t)
		badTLS, err := tlsx.NewClientTLSConfig(tlsx.ClientConfig{
			CertFile:   client.CertFile,
			KeyFile:    client.KeyFile,
			CAFile:     other.CAFile,
			ServerName: "localhost",
		})
		if err != nil {
			t.Fatalf("NewClientTLSConfig: %v", err)
		}
		badConn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(badTLS)))
		if err != nil {
			t.Fatalf("grpc.NewClient: %v", err)
		}
		defer badConn.Close()
		if _, err := healthpb.NewHealthClient(badConn).Check(ctx, &healthpb.HealthCheckRequest{}); err == nil {
			t.Fatal("gRPC call with an untrusted CA unexpectedly succeeded")
		}
	})
}

func TestExpectedSPIFFEIDEnforced(t *testing.T) {
	bundle := generatePKI(t)
	applyRequiredEnv(t, bundle)
	server := bundle.Services["guardian"]

	serverTLS, _, err := ServerTLSConfig(LegacyServer{
		CertFile:          server.CertFile,
		KeyFile:           server.KeyFile,
		CAFile:            bundle.CAFile,
		RequireClientCert: true,
	})
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}
	addr := listenTLS(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}), serverTLS)

	t.Setenv("STRATA_TLS_EXPECTED_SPIFFE_IDS", serverSPIFFEID)
	good, _, err := HTTPClient(LegacyClient{
		CertFile:   bundle.Services["client"].CertFile,
		KeyFile:    bundle.Services["client"].KeyFile,
		CAFile:     bundle.CAFile,
		ServerName: "localhost",
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("HTTPClient: %v", err)
	}
	if _, err := good.Get("https://" + addr + "/"); err != nil {
		t.Fatalf("matching SPIFFE ID rejected: %v", err)
	}

	t.Setenv("STRATA_TLS_EXPECTED_SPIFFE_IDS", "spiffe://strata.local/ns/guardian/sa/other")
	bad, _, err := HTTPClient(LegacyClient{
		CertFile:   bundle.Services["client"].CertFile,
		KeyFile:    bundle.Services["client"].KeyFile,
		CAFile:     bundle.CAFile,
		ServerName: "localhost",
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("HTTPClient: %v", err)
	}
	_, err = bad.Get("https://" + addr + "/")
	if err == nil || !strings.Contains(err.Error(), "SPIFFE") {
		t.Fatalf("expected SPIFFE allowlist rejection, got %v", err)
	}
}
