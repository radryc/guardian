package ui

import (
	"net/http"

	"github.com/rydzu/ainfra/guardian/internal/observability"
)

func (s *Server) handleObservabilityIdentities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	records, err := observability.Identities(r.Context(), s.store)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"identities": records})
}
