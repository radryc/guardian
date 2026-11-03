package ui

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	coreauthz "github.com/radryc/monofs/pkg/authz"
)

const defaultCSP = "default-src 'self'; " +
	"base-uri 'self'; " +
	"connect-src 'self'; " +
	"font-src 'self' data:; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'; " +
	"frame-src 'self' http://localhost:9191 http://127.0.0.1:9191 http://*:9191 https://*:9191; " +
	"img-src 'self' data:; " +
	"object-src 'none'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline'"

// applySecurityHeaders sets a conservative browser baseline for every UI and
// API response. HSTS is only emitted when TLS is present or explicitly enabled.
func (s *Server) applySecurityHeaders(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", defaultCSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	if s != nil && (s.enableHSTS || (r != nil && r.TLS != nil)) {
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
}

// checkUIAuth rejects anonymous mutations when authentication is required.
// Loopback requests may be exempted for local development, but only when the
// deployment explicitly configures that escape hatch.
func (s *Server) checkUIAuth(w http.ResponseWriter, r *http.Request) bool {
	if s == nil || !s.requireAuthentication || isSafeMethod(r.Method) {
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/api/m2m/") || strings.HasPrefix(r.URL.Path, "/api/access/") {
		// Machine-to-machine endpoints perform their own token checks.
		return true
	}
	identity, _ := coreauthz.IdentityFromContext(r.Context())
	if !identity.IsAnonymous() {
		return true
	}
	if s.allowLoopbackAnonymous && isLoopbackRemote(r.RemoteAddr) {
		return true
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, http.StatusUnauthorized, errString("authentication is required for UI mutations"))
	return false
}

// checkMutationOrigin rejects cross-origin browser mutations and cookie-backed
// requests that do not carry an Origin or Referer. Bearer-authenticated API
// clients and loopback development requests are allowed.
func (s *Server) checkMutationOrigin(w http.ResponseWriter, r *http.Request) bool {
	if s == nil || !s.requireAuthentication || isSafeMethod(r.Method) {
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/api/m2m/") || strings.HasPrefix(r.URL.Path, "/api/access/") {
		return true
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		if s.originAllowed(origin, r.Host) {
			return true
		}
		writeError(w, http.StatusForbidden, errString("cross-origin mutation is not allowed"))
		return false
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		if s.originAllowed(referer, r.Host) {
			return true
		}
		writeError(w, http.StatusForbidden, errString("cross-origin mutation is not allowed"))
		return false
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Authorization"))), "bearer ") {
		return true
	}
	if s.allowLoopbackAnonymous && isLoopbackRemote(r.RemoteAddr) {
		return true
	}
	if len(r.Cookies()) > 0 {
		writeError(w, http.StatusForbidden, errString("mutation requires an Origin or Referer header"))
		return false
	}
	// Non-browser clients without cookies are expected to use bearer auth or
	// an internal token; leave the per-endpoint authorization to decide.
	return true
}

func (s *Server) originAllowed(raw, requestHost string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return false
	}
	if strings.EqualFold(parsed.Host, requestHost) {
		return true
	}
	for _, trusted := range s.trustedOrigins {
		trusted = strings.TrimRight(strings.TrimSpace(trusted), "/")
		if trusted == "" {
			continue
		}
		candidate := parsed.Scheme + "://" + parsed.Host
		if strings.EqualFold(trusted, candidate) || strings.EqualFold(trusted, parsed.Host) {
			return true
		}
	}
	return false
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
