package awsread

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandlerEnforcesScopes(t *testing.T) {
	h := NewHandler(&fakeReader{}, "secret").WithScopes("read")

	req := httptest.NewRequest(http.MethodPost, "/v1/aws/observed/diff", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("scope status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/aws/log-groups", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerObservedDiffDisabledByDefault(t *testing.T) {
	h := NewHandler(&fakeReader{}, "").WithScopes("read", "diff")
	req := httptest.NewRequest(http.MethodPost, "/v1/aws/observed/diff", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("diff status = %d, want 404", rec.Code)
	}
}

func TestHandlerTokenFileRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(&fakeReader{}, "").WithTokenFile(path)

	req := httptest.NewRequest(http.MethodGet, "/v1/aws/log-groups", nil)
	req.Header.Set("Authorization", "Bearer first")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first token status = %d", rec.Code)
	}

	if err := os.WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/aws/log-groups", nil)
	req.Header.Set("Authorization", "Bearer second")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotated token status = %d", rec.Code)
	}
}

func TestHandlerRateLimit(t *testing.T) {
	h := NewHandler(&fakeReader{}, "").WithRateLimit(0.001, 1)
	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/v1/aws/log-groups", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	second := httptest.NewRecorder()
	h.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/v1/aws/log-groups", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", second.Code)
	}
}

func TestHandlerBodyLimit(t *testing.T) {
	h := NewHandler(&fakeReader{}, "").WithMaxRequestBodyBytes(8)
	body := strings.NewReader(`{"log_groups":["/ecs/strata"]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/aws/logs/query", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerMissingTokenFileFailsClosed(t *testing.T) {
	h := NewHandler(&fakeReader{}, "").WithTokenFile(filepath.Join(t.TempDir(), "missing"))
	req := httptest.NewRequest(http.MethodGet, "/v1/aws/log-groups", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
