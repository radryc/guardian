package ui

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/paths"
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

	entries, err := s.store.ListDir(r.Context(), paths.ProbesRoot())
	if err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]probes.Result, 0)
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		if pusherFilter != "" && entry.Name != pusherFilter {
			continue
		}
		snapshots, err := s.store.ListDir(r.Context(), paths.ProbesDir(entry.Name))
		if err != nil {
			continue
		}
		for _, snapshotEntry := range snapshots {
			if snapshotEntry.IsDir || !strings.HasSuffix(snapshotEntry.Name, ".json") {
				continue
			}
			partition := strings.TrimSuffix(snapshotEntry.Name, ".json")
			if partitionFilter != "" && partition != partitionFilter {
				continue
			}
			raw, err := s.store.ReadFile(r.Context(), paths.ProbeSnapshot(entry.Name, partition))
			if err != nil {
				continue
			}
			var snapshot probes.Snapshot
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				continue
			}
			for _, result := range snapshot.Probes {
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
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"probes": out})
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
	entries, err := s.store.ListDir(r.Context(), paths.ProbesRoot())
	if err != nil {
		writeError(w, http.StatusNotFound, errString("probe not found"))
		return
	}
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		snapshots, err := s.store.ListDir(r.Context(), paths.ProbesDir(entry.Name))
		if err != nil {
			continue
		}
		for _, snapshotEntry := range snapshots {
			if snapshotEntry.IsDir || !strings.HasSuffix(snapshotEntry.Name, ".json") {
				continue
			}
			partition := strings.TrimSuffix(snapshotEntry.Name, ".json")
			raw, err := s.store.ReadFile(r.Context(), paths.ProbeSnapshot(entry.Name, partition))
			if err != nil {
				continue
			}
			var snapshot probes.Snapshot
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				continue
			}
			for _, result := range snapshot.Probes {
				if result.ProbeID == probeID {
					writeJSON(w, http.StatusOK, result)
					return
				}
			}
		}
	}
	writeError(w, http.StatusNotFound, errString("probe not found"))
}

type errString string

func (e errString) Error() string { return string(e) }
