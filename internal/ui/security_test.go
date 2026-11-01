package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/store/memory"
	coreauthz "github.com/radryc/monofs/pkg/authz"
)

func newSecurityTestServer(t *testing.T, opts Options) *Server {
	t.Helper()
	opts.Store = memory.New()
	opts.PrincipalID = "security-test"
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	return srv
}

func TestSecurityHeadersArePresent(t *testing.T) {
	srv := newSecurityTestServer(t, Options{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatal("missing Content-Security-Policy")
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got == "" {
		t.Fatal("missing Referrer-Policy")
	}
}

func TestSecurityHeadersHSTSOnlyWhenEnabledOrTLS(t *testing.T) {
	srv := newSecurityTestServer(t, Options{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("unexpected HSTS without TLS: %q", got)
	}

	hsts := newSecurityTestServer(t, Options{EnableHSTS: true})
	rec = httptest.NewRecorder()
	hsts.ServeHTTP(rec, req)
	if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("missing HSTS when explicitly enabled")
	}
}

func TestAnonymousMutationRejectedWhenAuthRequired(t *testing.T) {
	srv := newSecurityTestServer(t, Options{RequireAuthentication: true})
	req := httptest.NewRequest(http.MethodPut, "/api/partitions/demo/bundle", strings.NewReader(`{}`))
	req.RemoteAddr = "203.0.113.10:1234"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestLoopbackAnonymousMutationAllowedOnlyWhenConfigured(t *testing.T) {
	srv := newSecurityTestServer(t, Options{RequireAuthentication: true, AllowLoopbackAnonymous: true})
	req := httptest.NewRequest(http.MethodPut, "/api/partitions/demo/bundle", strings.NewReader(`{}`))
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("loopback anonymous mutation should be allowed by the explicit escape hatch")
	}

	strict := newSecurityTestServer(t, Options{RequireAuthentication: true})
	rec = httptest.NewRecorder()
	strict.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("strict status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestCrossOriginMutationRejected(t *testing.T) {
	srv := newSecurityTestServer(t, Options{RequireAuthentication: true})
	req := httptest.NewRequest(http.MethodPut, "/api/partitions/demo/bundle", strings.NewReader(`{}`))
	req.RemoteAddr = "203.0.113.10:1234"
	req.Host = "guardian.example.com"
	req.Header.Set("Origin", "https://evil.example.net")
	req = req.WithContext(coreauthz.ContextWithIdentity(context.Background(), coreauthz.Identity{Subject: "user-1"}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestSameOriginMutationAllowed(t *testing.T) {
	srv := newSecurityTestServer(t, Options{RequireAuthentication: true})
	req := httptest.NewRequest(http.MethodPut, "/api/partitions/demo/bundle", strings.NewReader(`{}`))
	req.RemoteAddr = "203.0.113.10:1234"
	req.Host = "guardian.example.com"
	req.Header.Set("Origin", "https://guardian.example.com")
	req = req.WithContext(coreauthz.ContextWithIdentity(context.Background(), coreauthz.Identity{Subject: "user-1"}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("same-origin mutation was rejected: %d %s", rec.Code, rec.Body.String())
	}
}

func TestBearerMutationWithoutOriginAllowed(t *testing.T) {
	srv := newSecurityTestServer(t, Options{RequireAuthentication: true})
	req := httptest.NewRequest(http.MethodPut, "/api/partitions/demo/bundle", strings.NewReader(`{}`))
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("Authorization", "Bearer token")
	req = req.WithContext(coreauthz.ContextWithIdentity(context.Background(), coreauthz.Identity{Subject: "user-1"}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("bearer mutation was rejected: %d %s", rec.Code, rec.Body.String())
	}
}
