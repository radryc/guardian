package main

import (
	"strings"
	"testing"
)

func TestValidateReadAPIConfig(t *testing.T) {
	t.Run("loopback without token or TLS allowed", func(t *testing.T) {
		err := validateReadAPIConfig(readAPIConfig{Address: "127.0.0.1:19091"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("non-loopback without token rejected", func(t *testing.T) {
		err := validateReadAPIConfig(readAPIConfig{Address: "0.0.0.0:19091"})
		if err == nil || !strings.Contains(err.Error(), "bearer token") {
			t.Fatalf("expected bearer token error, got %v", err)
		}
	})

	t.Run("non-loopback plaintext with token rejected", func(t *testing.T) {
		err := validateReadAPIConfig(readAPIConfig{
			Address: "0.0.0.0:19091",
			Token:   "secret-token",
		})
		if err == nil || !strings.Contains(err.Error(), "without TLS") {
			t.Fatalf("expected TLS required error even with token, got %v", err)
		}
	})

	t.Run("non-loopback plaintext with token and allow-plaintext allowed", func(t *testing.T) {
		err := validateReadAPIConfig(readAPIConfig{
			Address:        "0.0.0.0:19091",
			Token:          "secret-token",
			AllowPlaintext: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("non-loopback with STRATA TLS enabled allowed", func(t *testing.T) {
		err := validateReadAPIConfig(readAPIConfig{
			Address:    "0.0.0.0:19091",
			Token:      "secret-token",
			TLSEnabled: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("mismatched TLS flags rejected", func(t *testing.T) {
		err := validateReadAPIConfig(readAPIConfig{
			Address: "0.0.0.0:19091",
			Token:   "secret-token",
			TLSCert: "/tmp/cert.pem",
		})
		if err == nil || !strings.Contains(err.Error(), "both --read-tls-cert and --read-tls-key") {
			t.Fatalf("expected mismatched TLS error, got %v", err)
		}
	})
}
