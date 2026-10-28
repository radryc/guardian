package ui

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/radryc/guardian/internal/paths"
)

// federationPartition is the normalized partition metadata consumed by the
// Doctor federation catalog controller. It deliberately contains only routing
// metadata, not health or asset detail, so a fleet feed stays cheap.
type federationPartition struct {
	Name         string            `json:"name"`
	Account      string            `json:"account,omitempty"`
	Region       string            `json:"region,omitempty"`
	Provider     string            `json:"provider,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	TargetPusher string            `json:"targetPusher,omitempty"`
	Warnings     []string          `json:"warnings,omitempty"`
}

type federationPartitionPage struct {
	Partitions []federationPartition `json:"partitions"`
	Total      int                   `json:"total"`
	NextCursor string                `json:"nextCursor,omitempty"`
	HasMore    bool                  `json:"hasMore"`
}

// handleM2MFederationPartitions serves the paginated partition feed used by
// the Doctor federation controller. The feed is gated by the same internal
// machine token as the pusher registry because it exposes fleet topology.
func (s *Server) handleM2MFederationPartitions(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/m2m/federation/partitions" {
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

	names, err := s.partitionNames(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	sort.Strings(names)

	limit := 500
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			limit = min(parsed, 2000)
		}
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	start := 0
	if cursor != "" {
		start = sort.SearchStrings(names, cursor)
		if start < len(names) && names[start] == cursor {
			start++
		}
	}
	if start > len(names) {
		start = len(names)
	}

	// Load only the page window. For the controller (no filters) this is a
	// single bounded pass over at most `limit` partitions.
	windowEnd := len(names)
	if limit > 0 && start+limit < windowEnd {
		windowEnd = start + limit
	}
	window := names[start:windowEnd]

	partitions := make([]federationPartition, len(window))
	errs := make([]error, len(window))
	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := 8
	if workers > len(window) {
		workers = len(window)
	}
	if workers < 1 {
		workers = 1
	}
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				partitions[index], errs[index] = s.loadFederationPartition(r.Context(), window[index])
			}
		}()
	}
	for index := range window {
		jobs <- index
	}
	close(jobs)
	wg.Wait()

	out := make([]federationPartition, 0, len(window))
	for index, err := range errs {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if federationPartitionMatches(partitions[index], r) {
			out = append(out, partitions[index])
		}
	}

	page := federationPartitionPage{
		Partitions: out,
		Total:      len(names),
		HasMore:    windowEnd < len(names),
	}
	if page.HasMore && len(window) > 0 {
		page.NextCursor = window[len(window)-1]
	}
	writeJSON(w, http.StatusOK, page)
}

func federationPartitionMatches(partition federationPartition, r *http.Request) bool {
	// Filters are applied only within the loaded page. The controller uses the
	// unfiltered feed; filtered views are for debugging and small scopes.
	q := r.URL.Query()
	if account := strings.TrimSpace(q.Get("account")); account != "" && !strings.EqualFold(account, partition.Account) {
		return false
	}
	if region := strings.TrimSpace(q.Get("region")); region != "" && !strings.EqualFold(region, partition.Region) {
		return false
	}
	if provider := strings.TrimSpace(q.Get("provider")); provider != "" && !strings.EqualFold(provider, partition.Provider) {
		return false
	}
	if search := strings.ToLower(strings.TrimSpace(q.Get("q"))); search != "" && !strings.Contains(strings.ToLower(partition.Name), search) {
		return false
	}
	return true
}

func (s *Server) loadFederationPartition(ctx context.Context, name string) (federationPartition, error) {
	raw, err := s.store.ReadFile(ctx, paths.PartitionConfig(name))
	if err != nil {
		return federationPartition{}, err
	}
	manifest, err := parsePartition(raw)
	if err != nil {
		return federationPartition{}, err
	}
	out := federationPartition{
		Name:   strings.TrimSpace(manifest.Metadata.Name),
		Labels: mergeStringMaps(manifest.Metadata.Labels, manifest.Spec.Labels),
	}
	if out.Name == "" {
		out.Name = name
	}

	sources, err := s.intentManifestSources(ctx, name)
	if err != nil {
		return federationPartition{}, err
	}
	for _, source := range sources {
		if source.Manifest == nil {
			continue
		}
		placement := source.Manifest.Spec.Target
		account := strings.TrimSpace(placement.Account)
		region := strings.TrimSpace(placement.Region)
		if account == "" && region == "" {
			continue
		}
		if out.Account != "" && account != "" && !strings.EqualFold(out.Account, account) {
			out.Warnings = append(out.Warnings, "conflicting account in intent "+source.Name)
			continue
		}
		if out.Region != "" && region != "" && !strings.EqualFold(out.Region, region) {
			out.Warnings = append(out.Warnings, "conflicting region in intent "+source.Name)
			continue
		}
		if out.Account == "" {
			out.Account = account
		}
		if out.Region == "" {
			out.Region = region
		}
		if out.TargetPusher == "" {
			out.TargetPusher = strings.TrimSpace(source.Manifest.Spec.TargetPusher)
		}
	}
	if out.Provider == "" {
		out.Provider = strings.TrimSpace(out.Labels["provider"])
	}
	if out.Provider == "" && out.Account != "" && out.Region != "" {
		out.Provider = "aws"
	}
	return out, nil
}
