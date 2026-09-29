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
	"time"

	"github.com/rydzu/ainfra/guardian/internal/awsgen"
	"github.com/rydzu/ainfra/guardian/internal/awsscan"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type CreateScanRequest struct {
	Pusher               string   `json:"pusher"`
	Account              string   `json:"account,omitempty"`
	Regions              []string `json:"regions,omitempty"`
	IncludeResourceTypes []string `json:"includeResourceTypes,omitempty"`
	ExcludeResourceTypes []string `json:"excludeResourceTypes,omitempty"`
	Inventory            bool     `json:"inventory,omitempty"`
	InventoryDetail      string   `json:"inventoryDetail,omitempty"`
}

type CreateScanResponse struct {
	ScanID  string `json:"scanID"`
	Pusher  string `json:"pusher"`
	Account string `json:"account"`
	Status  string `json:"status"`
}

type ScanListEntry struct {
	ScanID     string               `json:"scanID"`
	Pusher     string               `json:"pusher"`
	Account    string               `json:"account,omitempty"`
	Status     string               `json:"status"`
	Regions    []string             `json:"regions,omitempty"`
	CreatedAt  *time.Time           `json:"createdAt,omitempty"`
	FinishedAt *time.Time           `json:"finishedAt,omitempty"`
	Summary    *awsscan.ScanSummary `json:"summary,omitempty"`
}

type ScanListResponse struct {
	Pushers []string        `json:"pushers"`
	Scans   []ScanListEntry `json:"scans"`
}

type ScanBundleResponse struct {
	ScanID string            `json:"scanID"`
	Bundle SaveBundleRequest `json:"bundle"`
	Draft  *awsgen.Draft     `json:"draft"`
}

func (s *Server) scanPushers() []string {
	if len(s.pushers) > 0 {
		return s.pushers
	}
	entries, err := s.store.ListDir(context.Background(), paths.ScansRoot())
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir && !strings.HasPrefix(entry.Name, ".") {
			out = append(out, entry.Name)
		}
	}
	sort.Strings(out)
	return out
}

func accountFromPusher(pusher string) string {
	name := strings.TrimSpace(pusher)
	if rest, ok := strings.CutPrefix(name, "aws-"); ok && rest != "" {
		return rest
	}
	return ""
}

func (s *Server) handleScans(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleScanList(w, r)
	case http.MethodPost:
		s.handleScanCreate(w, r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleScanCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	pusher := strings.TrimSpace(req.Pusher)
	if pusher == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("pusher is required"))
		return
	}
	if len(s.pushers) > 0 {
		known := false
		for _, name := range s.pushers {
			if name == pusher {
				known = true
				break
			}
		}
		if !known {
			writeError(w, http.StatusBadRequest, fmt.Errorf("pusher %q is not configured", pusher))
			return
		}
	}
	account := strings.TrimSpace(req.Account)
	if account == "" {
		account = accountFromPusher(pusher)
	}
	if account == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("account is required for pusher %q", pusher))
		return
	}
	regions := req.Regions
	if len(regions) == 0 {
		regions = []string{awsscan.AllRegions}
	}
	inventoryDetail := strings.TrimSpace(req.InventoryDetail)
	if inventoryDetail == "" {
		inventoryDetail = awsscan.InventoryDetailSummary
	}
	if inventoryDetail != awsscan.InventoryDetailSummary && inventoryDetail != awsscan.InventoryDetailFull {
		writeError(w, http.StatusBadRequest, fmt.Errorf("inventoryDetail must be %q or %q", awsscan.InventoryDetailSummary, awsscan.InventoryDetailFull))
		return
	}

	scanID := "scan-" + newCorrelationID()
	request := awsscan.ScanRequest{
		APIVersion:           awsscan.APIVersion,
		Kind:                 awsscan.RequestKind,
		ScanID:               scanID,
		Account:              account,
		Regions:              regions,
		IncludeResourceTypes: req.IncludeResourceTypes,
		ExcludeResourceTypes: req.ExcludeResourceTypes,
		Inventory:            req.Inventory,
		InventoryDetail:      inventoryDetail,
		CreatedAt:            time.Now().UTC(),
		RequestedBy:          s.principalID,
	}
	content, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := s.store.UpsertFiles(r.Context(), guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: paths.ScanRequest(pusher, scanID),
			Content:     content,
		}},
		Context: guardianapi.MutationContext{
			PrincipalID:   s.principalID,
			Reason:        "create aws scan from guardian ui",
			CorrelationID: scanID,
		},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, CreateScanResponse{
		ScanID:  scanID,
		Pusher:  pusher,
		Account: account,
		Status:  string(awsscan.ScanStatusQueued),
	})
}

func (s *Server) handleScanList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pushers := s.scanPushers()
	entries := make([]ScanListEntry, 0)
	seen := make(map[string]struct{})
	for _, pusher := range pushers {
		known := make(map[string]struct{})
		if requests, err := s.store.ListDir(ctx, paths.ScanRequestsDir(pusher)); err == nil {
			for _, entry := range requests {
				if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
					continue
				}
				scanID := strings.TrimSuffix(entry.Name, ".json")
				known[scanID] = struct{}{}
				if _, duplicate := seen[pusher+"/"+scanID]; duplicate {
					continue
				}
				seen[pusher+"/"+scanID] = struct{}{}
				entries = append(entries, s.buildScanListEntry(ctx, pusher, scanID))
			}
		}
		if results, err := s.store.ListDir(ctx, paths.ScanResultsDir(pusher)); err == nil {
			for _, entry := range results {
				if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
					continue
				}
				scanID := strings.TrimSuffix(entry.Name, ".json")
				if _, covered := known[scanID]; covered {
					continue
				}
				if _, duplicate := seen[pusher+"/"+scanID]; duplicate {
					continue
				}
				seen[pusher+"/"+scanID] = struct{}{}
				entries = append(entries, s.buildScanListEntry(ctx, pusher, scanID))
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.CreatedAt != nil && b.CreatedAt != nil {
			return a.CreatedAt.After(*b.CreatedAt)
		}
		if a.CreatedAt != nil {
			return true
		}
		if b.CreatedAt != nil {
			return false
		}
		return a.ScanID < b.ScanID
	})
	writeJSON(w, http.StatusOK, ScanListResponse{Pushers: pushers, Scans: entries})
}

func (s *Server) buildScanListEntry(ctx context.Context, pusher, scanID string) ScanListEntry {
	listEntry := ScanListEntry{
		ScanID: scanID,
		Pusher: pusher,
		Status: string(awsscan.ScanStatusQueued),
	}
	if raw, err := s.store.ReadFile(ctx, paths.ScanRequest(pusher, scanID)); err == nil {
		var request awsscan.ScanRequest
		if err := json.Unmarshal(raw, &request); err == nil {
			listEntry.Account = request.Account
			listEntry.Regions = request.Regions
			createdAt := request.CreatedAt
			listEntry.CreatedAt = &createdAt
		}
	}
	if result, ok, _ := s.readScanResult(ctx, pusher, scanID); ok {
		listEntry.Status = string(result.Status)
		listEntry.Account = result.Account
		listEntry.Regions = result.Regions
		finishedAt := result.FinishedAt
		listEntry.FinishedAt = &finishedAt
		summary := result.Summary
		listEntry.Summary = &summary
	} else if claim, ok, _ := s.readScanClaim(ctx, pusher, scanID); ok {
		if !claim.ClaimedAt.IsZero() && time.Since(claim.ClaimedAt) < time.Duration(claim.LeaseSeconds)*time.Second {
			listEntry.Status = string(awsscan.ScanStatusRunning)
		}
	}
	return listEntry
}

func (s *Server) handleScanRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/api/scans"), "/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	scanID := parts[0]
	if len(parts) == 1 {
		s.handleScanGet(w, r, scanID)
		return
	}
	if len(parts) == 2 && parts[1] == "bundle" {
		s.handleScanBundle(w, r, scanID)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) readScanResult(ctx context.Context, pusher, scanID string) (*awsscan.ScanResult, bool, error) {
	raw, err := s.store.ReadFile(ctx, paths.ScanResult(pusher, scanID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var result awsscan.ScanResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, false, fmt.Errorf("decode scan result %s: %w", scanID, err)
	}
	return &result, true, nil
}

func (s *Server) readScanClaim(ctx context.Context, pusher, scanID string) (*awsscan.ScanClaim, bool, error) {
	raw, err := s.store.ReadFile(ctx, paths.ScanClaim(pusher, scanID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var claim awsscan.ScanClaim
	if err := json.Unmarshal(raw, &claim); err != nil {
		return nil, false, fmt.Errorf("decode scan claim %s: %w", scanID, err)
	}
	return &claim, true, nil
}

func (s *Server) locateScan(ctx context.Context, scanID string) (*awsscan.ScanResult, string, bool) {
	for _, pusher := range s.scanPushers() {
		if result, ok, err := s.readScanResult(ctx, pusher, scanID); err == nil && ok {
			return result, pusher, true
		}
	}
	return nil, "", false
}

func (s *Server) handleScanGet(w http.ResponseWriter, r *http.Request, scanID string) {
	result, _, ok := s.locateScan(r.Context(), scanID)
	if !ok {
		for _, pusher := range s.scanPushers() {
			if _, claimOK, _ := s.readScanClaim(r.Context(), pusher, scanID); claimOK {
				writeJSON(w, http.StatusOK, map[string]any{
					"scanID": scanID,
					"pusher": pusher,
					"status": string(awsscan.ScanStatusRunning),
				})
				return
			}
			if _, err := s.store.Stat(r.Context(), paths.ScanRequest(pusher, scanID)); err == nil {
				writeJSON(w, http.StatusOK, map[string]any{
					"scanID": scanID,
					"pusher": pusher,
					"status": string(awsscan.ScanStatusQueued),
				})
				return
			}
		}
		writeError(w, http.StatusNotFound, fmt.Errorf("scan %q not found", scanID))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleScanBundle(w http.ResponseWriter, r *http.Request, scanID string) {
	query := r.URL.Query()
	partition := strings.TrimSpace(query.Get("partition"))
	if partition == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("partition query parameter is required"))
		return
	}
	grouping := strings.TrimSpace(query.Get("grouping"))
	opts := awsgen.Options{
		PartitionName: partition,
		Grouping:      grouping,
		TargetPusher:  strings.TrimSpace(query.Get("pusher")),
	}
	if raw := strings.TrimSpace(query.Get("types")); raw != "" {
		for _, kind := range strings.Split(raw, ",") {
			if kind = strings.TrimSpace(kind); kind != "" {
				opts.IncludeTypes = append(opts.IncludeTypes, kind)
			}
		}
	}
	if raw := strings.TrimSpace(query.Get("regions")); raw != "" {
		for _, region := range strings.Split(raw, ",") {
			if region = strings.TrimSpace(region); region != "" {
				opts.Regions = append(opts.Regions, region)
			}
		}
	}
	if raw := strings.TrimSpace(query.Get("stacks")); raw != "" {
		for _, stack := range strings.Split(raw, ",") {
			if stack = strings.TrimSpace(stack); stack != "" {
				opts.Stacks = append(opts.Stacks, stack)
			}
		}
	}

	result, _, ok := s.locateScan(r.Context(), scanID)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("scan result %q not found", scanID))
		return
	}
	draft, err := awsgen.Generate(result, opts, s.store)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	bundle := SaveBundleRequest{
		Partition: draft.Partition,
		Intents:   make([]SaveIntentRequest, 0, len(draft.Intents)),
	}
	for _, intent := range draft.Intents {
		bundle.Intents = append(bundle.Intents, SaveIntentRequest{Manifest: intent})
	}
	writeJSON(w, http.StatusOK, ScanBundleResponse{
		ScanID: scanID,
		Bundle: bundle,
		Draft:  draft,
	})
}
