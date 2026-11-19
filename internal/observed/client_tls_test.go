package observed

import (
	"context"
	"testing"
)

func clearObservedTLSEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
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

func TestNewClientUpgradesSchemeWhenTLSRequired(t *testing.T) {
	clearObservedTLSEnv(t)
	t.Setenv("STRATA_TLS_MODE", "required")

	client := NewClient("http://pusher.internal:19090/", "token", 0)
	if client.BaseURL != "https://pusher.internal:19090" {
		t.Fatalf("BaseURL = %q, want https endpoint", client.BaseURL)
	}
	if client.initErr != nil {
		t.Fatalf("unexpected TLS initialization error: %v", client.initErr)
	}
}

func TestNewClientKeepsHTTPWhenTLSOff(t *testing.T) {
	clearObservedTLSEnv(t)

	client := NewClient("http://pusher.internal:19090/", "token", 0)
	if client.BaseURL != "http://pusher.internal:19090" {
		t.Fatalf("BaseURL = %q, want unchanged http endpoint", client.BaseURL)
	}
}

func TestNewClientFailsClosedOnInvalidTLSMode(t *testing.T) {
	clearObservedTLSEnv(t)
	t.Setenv("STRATA_TLS_MODE", "bogus")

	client := NewClient("https://pusher.internal:19090", "token", 0)
	if _, err := client.do(context.Background(), "GET", "/", nil, nil, nil); err == nil {
		t.Fatal("invalid STRATA_TLS_MODE must fail closed")
	}
}
