package ui

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/radryc/guardian/internal/registry"
	coreauthz "github.com/radryc/monofs/pkg/authz"
)

type m2mPusher struct {
	registry.Pusher
	HasToken bool `json:"hasToken"`
}

type m2mPusherListResponse struct {
	Pushers    []m2mPusher                  `json:"pushers"`
	Total      int                          `json:"total,omitempty"`
	Matched    int                          `json:"matched,omitempty"`
	NextCursor string                       `json:"nextCursor,omitempty"`
	HasMore    bool                         `json:"hasMore,omitempty"`
	Summary    *registry.PusherFleetSummary `json:"summary,omitempty"`
}

func redactM2MPusher(p registry.Pusher, reveal bool) m2mPusher {
	hasToken := strings.TrimSpace(p.Token) != ""
	if !reveal {
		p.Token = ""
	}
	return m2mPusher{
		Pusher:   p,
		HasToken: hasToken,
	}
}

// requestHasInternalToken reports whether the request carries the configured
// Guardian internal machine token. Unlike requestCanReadAccessPolicy this does
// not accept admin identities or loopback: raw pusher tokens are secrets and
// are only disclosed to the internal-token fleet (Doctor federation controller
// and query-side lazy resolver). Callers must also opt in with
// includeTokens=1 so a generic machine listing never returns credentials.
func (s *Server) requestHasInternalToken(r *http.Request) bool {
	token := strings.TrimSpace(s.accessPolicyToken)
	if token == "" {
		return false
	}
	provided := strings.TrimSpace(r.Header.Get("X-Guardian-Internal-Token"))
	if provided == "" {
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
			provided = strings.TrimSpace(authorization[len("Bearer "):])
		}
	}
	return provided != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

// revealTokens reports whether this request may see raw pusher credentials.
func (s *Server) revealTokens(r *http.Request) bool {
	return strings.TrimSpace(r.URL.Query().Get("includeTokens")) == "1" && s.requestHasInternalToken(r)
}

// handleM2MPushers exposes the pusher registry to trusted machine callers
// (Doctor federation / the catalog controller). Raw bearer tokens are redacted
// from responses; hasToken indicates whether a token is configured.
func (s *Server) handleM2MPushers(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/m2m/pushers" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.requireM2MAccess(w, r) {
		return
	}
	limit := 500
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = min(parsed, 2000)
		}
	}
	page, err := registry.New(s.store).ListPushersPage(r.Context(), registry.PusherListOptions{
		Provider: r.URL.Query().Get("provider"),
		Region:   r.URL.Query().Get("region"),
		Account:  r.URL.Query().Get("account"),
		Cluster:  r.URL.Query().Get("cluster"),
		Status:   r.URL.Query().Get("status"),
		Version:  r.URL.Query().Get("version"),
		Query:    r.URL.Query().Get("q"),
		Limit:    limit,
		Cursor:   r.URL.Query().Get("cursor"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	redacted := make([]m2mPusher, 0, len(page.Pushers))
	reveal := s.revealTokens(r)
	for _, p := range page.Pushers {
		redacted = append(redacted, redactM2MPusher(p, reveal))
	}
	writeJSON(w, http.StatusOK, m2mPusherListResponse{
		Pushers:    redacted,
		Total:      page.Total,
		Matched:    page.Matched,
		NextCursor: page.NextCursor,
		HasMore:    page.HasMore,
	})
}

// handleM2MPusherRoute resolves one pusher by name with its token redacted.
func (s *Server) handleM2MPusherRoute(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/m2m/pushers/"), "/")
	if name == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.requireM2MAccess(w, r) {
		return
	}
	pusher, err := registry.New(s.store).GetPusher(r.Context(), name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, redactM2MPusher(*pusher, s.revealTokens(r)))
}

func (s *Server) requireM2MAccess(w http.ResponseWriter, r *http.Request) bool {
	if s.requestCanReadAccessPolicy(r) {
		return true
	}
	status := http.StatusForbidden
	if _, ok := coreauthz.IdentityFromContext(r.Context()); !ok {
		status = http.StatusUnauthorized
	}
	writeError(w, status, errors.New("machine pusher access requires the internal token, admin role, or loopback access"))
	return false
}
