package gcpread

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerRateLimitPerIP(t *testing.T) {
	h := NewHandler(&fakeReader{}, "").WithRateLimit(0.001, 1)

	req1 := httptest.NewRequest(http.MethodGet, "/v1/gcp/log-names", nil)
	req1.RemoteAddr = "198.51.100.10:1234"
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/v1/gcp/log-names", nil)
	req2.RemoteAddr = "198.51.100.10:5678"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q, want 1", rec2.Header().Get("Retry-After"))
	}

	// Different IP should have its own bucket.
	req3 := httptest.NewRequest(http.MethodGet, "/v1/gcp/log-names", nil)
	req3.RemoteAddr = "198.51.100.11:1234"
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("different IP status = %d, want 200", rec3.Code)
	}

	// /healthz should remain exempt from rate limiting.
	reqHealth := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	reqHealth.RemoteAddr = "198.51.100.10:9999"
	recHealth := httptest.NewRecorder()
	h.ServeHTTP(recHealth, reqHealth)
	if recHealth.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", recHealth.Code)
	}
}

func TestHandlerPostBodyLimit(t *testing.T) {
	h := NewHandler(&fakeReader{}, "")
	if got := h.requestBodyLimit(); got != defaultMaxRequestBodyBytes {
		t.Fatalf("default requestBodyLimit = %d, want %d", got, defaultMaxRequestBodyBytes)
	}

	h.WithMaxRequestBodyBytes(8)
	req := httptest.NewRequest(http.MethodPost, "/v1/gcp/observed/refresh", strings.NewReader(`{"stack":"too-long"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	_, err := io.ReadAll(req.Body)
	var maxBytesErr *http.MaxBytesError
	if !errors.As(err, &maxBytesErr) {
		t.Fatalf("expected *http.MaxBytesError when reading oversized POST body, got %v", err)
	}
}
