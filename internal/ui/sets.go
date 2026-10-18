package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	setstore "github.com/rydzu/ainfra/guardian/internal/sets"
)

type SetsResponse struct {
	GeneratedAt string    `json:"generatedAt"`
	Sets        []SetView `json:"sets"`
	Ungrouped   int       `json:"ungrouped"`
	Warnings    []string  `json:"warnings,omitempty"`
}

type SetView struct {
	setstore.Set
	PartitionCount int `json:"partitionCount"`
	ChildCount     int `json:"childCount"`
}

type setWriteRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	ParentID    string            `json:"parentId"`
	Labels      map[string]string `json:"labels"`
	Pinned      *bool             `json:"pinned"`
}

type setPartitionsRequest struct {
	Add     []string `json:"add"`
	Remove  []string `json:"remove"`
	Primary bool     `json:"primary"`
}

type setMoveRequest struct {
	ParentID string `json:"parentId"`
}

func (s *Server) handleSets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		all, err := setstore.New(s.store).List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		visible, err := s.visiblePartitionSet(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		children := map[string]int{}
		member := map[string]bool{}
		for _, set := range all {
			if set.ParentID != "" {
				children[set.ParentID]++
			}
			for _, partition := range set.Partitions {
				member[partition] = true
			}
		}
		out := make([]SetView, 0, len(all))
		for _, set := range all {
			filtered := filterVisiblePartitions(set.Partitions, visible)
			set.Partitions = filtered
			out = append(out, SetView{Set: set, PartitionCount: len(filtered), ChildCount: children[set.ID]})
		}
		ungrouped := 0
		for partition := range visible {
			if !member[partition] {
				ungrouped++
			}
		}
		writeJSON(w, http.StatusOK, SetsResponse{Sets: out, Ungrouped: ungrouped})
	case http.MethodPost:
		var req setWriteRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode set: %w", err))
			return
		}
		set := &setstore.Set{Name: req.Name, Description: req.Description, ParentID: req.ParentID, Labels: req.Labels}
		if req.Pinned != nil {
			set.Pinned = *req.Pinned
		}
		created, err := setstore.New(s.store).Create(r.Context(), set)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, setstore.ErrParentNotFound) || errors.Is(err, setstore.ErrCycle) {
				status = http.StatusConflict
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, created)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleSetRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/sets/"), "/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	id := rest
	action := ""
	if idx := strings.Index(rest, "/"); idx >= 0 {
		id = rest[:idx]
		action = rest[idx+1:]
	}
	store := setstore.New(s.store)
	switch {
	case action == "" && r.Method == http.MethodGet:
		set, err := store.Get(r.Context(), id)
		if err != nil {
			writeSetError(w, err)
			return
		}
		visible, err := s.visiblePartitionSet(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		set.Partitions = filterVisiblePartitions(set.Partitions, visible)
		writeJSON(w, http.StatusOK, SetView{Set: *set, PartitionCount: len(set.Partitions)})
	case action == "" && r.Method == http.MethodPatch:
		existing, err := store.Get(r.Context(), id)
		if err != nil {
			writeSetError(w, err)
			return
		}
		var req setWriteRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode set: %w", err))
			return
		}
		if strings.TrimSpace(req.Name) != "" {
			existing.Name = req.Name
		}
		existing.Description = req.Description
		existing.ParentID = req.ParentID
		if req.Labels != nil {
			existing.Labels = req.Labels
		}
		if req.Pinned != nil {
			existing.Pinned = *req.Pinned
		}
		updated, err := store.Update(r.Context(), existing)
		if err != nil {
			writeSetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case action == "" && r.Method == http.MethodDelete:
		if err := store.Delete(r.Context(), id); err != nil {
			writeSetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
	case action == "move" && r.Method == http.MethodPost:
		var req setMoveRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode move: %w", err))
			return
		}
		updated, err := store.Move(r.Context(), id, req.ParentID)
		if err != nil {
			writeSetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case action == "partitions" && r.Method == http.MethodPost:
		var req setPartitionsRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode partitions: %w", err))
			return
		}
		for _, partition := range req.Add {
			if _, err := s.store.Stat(r.Context(), paths.PartitionConfig(partition)); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					writeError(w, http.StatusBadRequest, fmt.Errorf("partition %q not found", partition))
					return
				}
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		updated, err := store.AddPartitions(r.Context(), id, req.Add, req.Remove, req.Primary)
		if err != nil {
			writeSetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete, http.MethodPost)
	}
}

func writeSetError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, os.ErrNotExist):
		status = http.StatusNotFound
	case errors.Is(err, setstore.ErrNotEmpty):
		status = http.StatusConflict
	case errors.Is(err, setstore.ErrCycle), errors.Is(err, setstore.ErrParentNotFound):
		status = http.StatusConflict
	}
	writeError(w, status, err)
}

func (s *Server) visiblePartitionSet(ctx context.Context) (map[string]bool, error) {
	names, err := s.partitionNames(ctx)
	if err != nil {
		return nil, err
	}
	if s.access != nil && s.access.Enabled() {
		names = s.access.FilterPartitions(ctx, names)
	}
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out, nil
}

func filterVisiblePartitions(partitions []string, visible map[string]bool) []string {
	out := make([]string, 0, len(partitions))
	for _, partition := range partitions {
		if visible[partition] {
			out = append(out, partition)
		}
	}
	sort.Strings(out)
	return out
}
