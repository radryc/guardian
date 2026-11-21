package ui

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	coreauthz "github.com/radryc/monofs/pkg/authz"
	"github.com/radryc/monofs/pkg/display"
)

func subjectsFromRequest(r *http.Request) []string {
	id, _ := coreauthz.IdentityFromContext(r.Context())
	who := strings.TrimSpace(id.Email)
	if who == "" {
		who = strings.TrimSpace(id.Subject)
	}
	if who == "" {
		who = strings.TrimSpace(id.ClientID)
	}
	if who == "" {
		return nil
	}
	out := []string{"user:" + who}
	for _, group := range id.Groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		out = append(out, group, "team:"+group)
	}
	return out
}

func primaryDisplaySubject(subjects []string) string {
	for _, subject := range subjects {
		if strings.TrimSpace(subject) != "" {
			return subject
		}
	}
	return ""
}

func (s *Server) displayAdmin(r *http.Request) bool {
	return s.access != nil && s.access.IsAdmin(r.Context())
}

func (s *Server) handleDisplayProfiles(w http.ResponseWriter, r *http.Request) {
	if s.displayProfiles == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("display profiles are not configured"))
		return
	}
	subjects := subjectsFromRequest(r)
	if len(subjects) == 0 {
		writeError(w, http.StatusForbidden, fmt.Errorf("authentication required"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		profiles, err := s.displayProfiles.List(r.Context(), subjects, s.displayAdmin(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
	case http.MethodPost:
		var profile display.Profile
		if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		created, err := s.displayProfiles.Create(r.Context(), subjects, profile, s.displayAdmin(r))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleDisplayProfileRoute(w http.ResponseWriter, r *http.Request) {
	if s.displayProfiles == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("display profiles are not configured"))
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/display-profiles/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	if rest == "bundle" {
		s.handleDisplayBundle(w, r)
		return
	}
	subjects := subjectsFromRequest(r)
	if len(subjects) == 0 {
		writeError(w, http.StatusForbidden, fmt.Errorf("authentication required"))
		return
	}
	switch rest {
	case "effective":
		s.handleDisplayEffective(w, r, subjects)
		return
	}
	id := strings.Trim(strings.Split(rest, "/")[0], "/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		profile, err := s.displayProfiles.Get(r.Context(), subjects, id, s.displayAdmin(r))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, profile)
	case http.MethodPatch, http.MethodPut:
		var profile display.Profile
		if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		updated, err := s.displayProfiles.Update(r.Context(), subjects, id, profile, s.displayAdmin(r))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.displayProfiles.Delete(r.Context(), subjects, id, s.displayAdmin(r)); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
	}
}

func (s *Server) handleDisplayEffective(w http.ResponseWriter, r *http.Request, subjects []string) {
	switch r.Method {
	case http.MethodGet:
		id, err := s.displayProfiles.Default(r.Context(), subjects)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"profile": id, "subject": primaryDisplaySubject(subjects)})
	case http.MethodPut:
		var body struct {
			Profile string `json:"profile"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.displayProfiles.SetDefault(r.Context(), subjects, body.Profile); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"profile": body.Profile})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPut)
	}
}

func (s *Server) handleDisplayBundle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.canReadDisplayBundle(r) {
		writeError(w, http.StatusForbidden, fmt.Errorf("display bundle requires a service token, loopback access, or an admin session"))
		return
	}
	bundle, version, err := s.displayProfiles.Bundle(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set(display.VersionHeader, version)
	w.Header().Set("ETag", `"`+version+`"`)
	writeJSON(w, http.StatusOK, bundle)
}

// CanReadDisplayBundle reports whether the request carries the service token,
// loopback identity, or an admin session allowed to read the bundle.
func (s *Server) CanReadDisplayBundle(r *http.Request) bool {
	return s.canReadDisplayBundle(r)
}

func (s *Server) canReadDisplayBundle(r *http.Request) bool {
	token := strings.TrimSpace(s.accessPolicyToken)
	if token != "" {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(header, "Bearer ") && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(header, "Bearer ")), []byte(token)) == 1 {
			return true
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && (host == "127.0.0.1" || host == "::1") {
		return true
	}
	return s.displayAdmin(r)
}
