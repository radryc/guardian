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
	opts.AllowUnpolicedMutations = true
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
	} else if !strings.Contains(got, "frame-src") || !strings.Contains(got, "http://*:9191") {
		t.Fatalf("CSP does not allow the Ask Strata embed: %q", got)
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

func TestLoopbackAnonymousRejectedWithForwardingHeaders(t *testing.T) {
	srv := newSecurityTestServer(t, Options{RequireAuthentication: true, AllowLoopbackAnonymous: true})

	for _, hdr := range []struct {
		name  string
		value string
	}{
		{name: "X-Forwarded-For", value: "203.0.113.10"},
		{name: "Forwarded", value: "for=203.0.113.10"},
	} {
		t.Run(hdr.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/partitions/demo/bundle", strings.NewReader(`{}`))
			req.RemoteAddr = "127.0.0.1:1234"
			req.Header.Set(hdr.name, hdr.value)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d when %s is set", rec.Code, http.StatusUnauthorized, hdr.name)
			}
		})
	}
}

func TestRequestIsLoopback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/client-config", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	if !requestIsLoopback(req) {
		t.Fatal("expected 127.0.0.1:1234 without forwarding headers to be loopback")
	}

	req.Header.Set("X-Forwarded-For", "198.51.100.1")
	if requestIsLoopback(req) {
		t.Fatal("expected request with X-Forwarded-For not to be treated as loopback")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/client-config", nil)
	req2.RemoteAddr = "[::1]:1234"
	if !requestIsLoopback(req2) {
		t.Fatal("expected [::1]:1234 without forwarding headers to be loopback")
	}
	req2.Header.Set("Forwarded", "for=198.51.100.1")
	if requestIsLoopback(req2) {
		t.Fatal("expected request with Forwarded not to be treated as loopback")
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
