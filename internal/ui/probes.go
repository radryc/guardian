package ui

import (
	"net/http"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/pusher/probes"
)

func (s *Server) handleProbes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	pusherFilter := strings.TrimSpace(r.URL.Query().Get("pusher"))
	clusterFilter := strings.TrimSpace(r.URL.Query().Get("cluster"))
	partitionFilter := strings.TrimSpace(r.URL.Query().Get("partition"))
	serviceFilter := strings.TrimSpace(r.URL.Query().Get("service"))
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))

	all, err := s.projectionCache.getAllProbes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]probes.Result, 0, len(all))
	for _, result := range all {
		if pusherFilter != "" && result.Pusher != pusherFilter {
			continue
		}
		if partitionFilter != "" && result.Partition != partitionFilter {
			continue
		}
		if clusterFilter != "" && result.Cluster != clusterFilter {
			continue
		}
		if serviceFilter != "" && result.Service != serviceFilter {
			continue
		}
		if statusFilter != "" && result.Status != statusFilter {
			continue
		}
		out = append(out, result)
	}
	probes.SortResults(out)
	writeJSONWithETag(w, r, http.StatusOK, map[string]any{"probes": out})
}

func (s *Server) handleProbeRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	probeID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/probes/"), "/")
	if probeID == "" {
		writeError(w, http.StatusBadRequest, errString("probe id is required"))
		return
	}
	all, err := s.projectionCache.getAllProbes(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, errString("probe not found"))
		return
	}
	for _, result := range all {
		if result.ProbeID == probeID {
			writeJSONWithETag(w, r, http.StatusOK, result)
			return
		}
	}
	writeError(w, http.StatusNotFound, errString("probe not found"))
}

type errString string

func (e errString) Error() string { return string(e) }
