package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/unmanaged"
)

// UnmanagedService detects stacks without a repository and prepares repo
// associations. It is optional; when nil the API reports 503.
type UnmanagedService interface {
	Detect(ctx context.Context) (*unmanaged.Result, error)
	Associate(ctx context.Context, opts unmanaged.AssociateOptions) (*unmanaged.Association, error)
}

// handleUnmanaged serves GET /api/aws/unmanaged.
func (s *Server) handleUnmanaged(w http.ResponseWriter, r *http.Request) {
	if s.unmanaged == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("unmanaged stacks service is not configured"))
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	result, err := s.unmanaged.Detect(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type associateStackRequest struct {
	RepoName  string `json:"repoName"`
	RepoURL   string `json:"repoUrl"`
	Branch    string `json:"branch"`
	Partition string `json:"partition"`
	Path      string `json:"path"`
	Pusher    string `json:"pusher"`
	Account   string `json:"account"`
	Region    string `json:"region"`
	Template  string `json:"template"`
	Interval  string `json:"interval"`
}

// handleUnmanagedRoute serves POST /api/aws/unmanaged/{stack}/associate.
func (s *Server) handleUnmanagedRoute(w http.ResponseWriter, r *http.Request) {
	if s.unmanaged == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("unmanaged stacks service is not configured"))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/aws/unmanaged/")
	stackName := ""
	if strings.HasSuffix(path, "/associate") {
		stackName = strings.TrimSuffix(path, "/associate")
	}
	if stackName == "" {
		writeError(w, http.StatusNotFound, fmt.Errorf("not found"))
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var req associateStackRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode associate request: %w", err))
		return
	}
	association, err := s.unmanaged.Associate(r.Context(), unmanaged.AssociateOptions{
		RepoName:   req.RepoName,
		RepoURL:    req.RepoURL,
		BaseBranch: req.Branch,
		Partition:  req.Partition,
		Path:       req.Path,
		Pusher:     req.Pusher,
		Account:    req.Account,
		Region:     req.Region,
		StackName:  stackName,
		Template:   req.Template,
		Interval:   req.Interval,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, association)
}
