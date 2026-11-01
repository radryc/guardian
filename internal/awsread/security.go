package awsread

import (
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const defaultMaxRequestBodyBytes = 1 << 20

// WithScopes limits the legacy bearer token to the listed scopes. An empty
// scope set preserves the pre-scope behaviour (all endpoints) for compatibility.
func (h *Handler) WithScopes(scopes ...string) *Handler {
	h.Scopes = normalizeScopes(scopes)
	return h
}

// WithTokenFile rotates the bearer token from a file. The file is re-read on
// each request, so a mounted secret can be rotated without a restart.
func (h *Handler) WithTokenFile(path string) *Handler {
	h.TokenFile = strings.TrimSpace(path)
	return h
}

// WithObservedDiff enables the privileged source-upload/diff endpoint. It is
// disabled by default because the endpoint runs npm/cdk on caller input.
func (h *Handler) WithObservedDiff(enabled bool) *Handler {
	h.EnableObservedDiff = enabled
	return h
}

// WithMaxRequestBodyBytes bounds request bodies that are not already limited by
// a handler-specific decoder.
func (h *Handler) WithMaxRequestBodyBytes(limit int64) *Handler {
	if limit > 0 {
		h.MaxRequestBodyBytes = limit
	}
	return h
}

// WithRateLimit configures a per-caller token bucket. Non-positive values
// disable rate limiting.
func (h *Handler) WithRateLimit(requestsPerSecond float64, burst int) *Handler {
	if requestsPerSecond <= 0 {
		return h
	}
	if burst <= 0 {
		burst = 1
	}
	h.rateLimit = rate.Limit(requestsPerSecond)
	h.rateBurst = burst
	h.rateBuckets = make(map[string]*rate.Limiter)
	return h
}

func normalizeScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func (h *Handler) currentToken() (string, bool) {
	if h.TokenFile != "" {
		content, err := os.ReadFile(h.TokenFile)
		if err != nil {
			return "", false
		}
		token := strings.TrimSpace(string(content))
		return token, token != ""
	}
	token := strings.TrimSpace(h.Token)
	if token == "" {
		return "", true
	}
	return token, true
}

func (h *Handler) scopeFor(path string) string {
	switch {
	case path == "/v1/aws/observed/diff":
		return "diff"
	case path == "/v1/aws/observed/refresh":
		return "diff"
	default:
		return "read"
	}
}

func (h *Handler) tokenValid(r *http.Request) bool {
	token, ok := h.currentToken()
	if !ok {
		return false
	}
	if token == "" {
		return true
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")) == token
}

func (h *Handler) scopeAllowed(scope string) bool {
	if len(h.Scopes) == 0 {
		return true
	}
	for _, allowed := range h.Scopes {
		if allowed == scope || allowed == "admin" || allowed == "*" {
			return true
		}
	}
	return false
}

func (h *Handler) allowRate(r *http.Request) bool {
	if h.rateLimit <= 0 {
		return true
	}
	key := strings.TrimSpace(r.Header.Get("Authorization"))
	if key == "" {
		host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
		if err != nil {
			host = strings.TrimSpace(r.RemoteAddr)
		}
		key = "ip:" + host
	}
	now := time.Now()
	h.rateMu.Lock()
	defer h.rateMu.Unlock()
	limiter, ok := h.rateBuckets[key]
	if !ok {
		limiter = rate.NewLimiter(h.rateLimit, h.rateBurst)
		h.rateBuckets[key] = limiter
		if len(h.rateBuckets) > 4096 {
			for candidate, bucket := range h.rateBuckets {
				if candidate != key && bucket.TokensAt(now) >= float64(h.rateBurst) {
					delete(h.rateBuckets, candidate)
				}
			}
		}
	}
	return limiter.AllowN(now, 1)
}

func (h *Handler) requestBodyLimit() int64 {
	if h.MaxRequestBodyBytes > 0 {
		return h.MaxRequestBodyBytes
	}
	return defaultMaxRequestBodyBytes
}

// rateState is embedded in Handler to keep the public type readable.
type rateState struct {
	rateMu      sync.Mutex
	rateLimit   rate.Limit
	rateBurst   int
	rateBuckets map[string]*rate.Limiter
}
