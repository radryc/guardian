package ui

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	coreauthz "github.com/radryc/monofs/pkg/authz"
	"github.com/radryc/guardian/internal/registry"
)

// handleM2MPushers exposes the pusher registry to trusted machine callers
// (Doctor federation / the catalog controller). It returns bearer tokens, so it
// is gated by the same internal token as the access-policy endpoint.
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
	writeJSON(w, http.StatusOK, pusherListResponse{
		Pushers:    page.Pushers,
		Total:      page.Total,
		Matched:    page.Matched,
		NextCursor: page.NextCursor,
		HasMore:    page.HasMore,
	})
}

// handleM2MPusherRoute resolves one pusher by name, including its token.
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
	writeJSON(w, http.StatusOK, pusher)
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
