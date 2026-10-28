package ui

import (
	"bytes"
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	coreauthz "github.com/radryc/monofs/pkg/authz"
	"github.com/radryc/guardian/internal/access"
	"github.com/radryc/guardian/internal/buildinfo"
	historydomain "github.com/radryc/guardian/internal/domain/history"
	"github.com/radryc/guardian/internal/historyquery"
	"github.com/radryc/guardian/internal/orchestrator/dispatcher"
	"github.com/radryc/guardian/internal/orchestrator/reconciler"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/versioning/revisions"
	"github.com/radryc/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*.css static/*.js static/*.png
var staticFS embed.FS

type Options struct {
	Store              guardianapi.Store
	Dispatcher         *dispatcher.Dispatcher
	PrincipalID        string
	Pushers            []string
	StaleTaskAfter     time.Duration
	DataCacheTTL       time.Duration
	EnableFlowTopology bool
	FlowEvaluator      FlowStateEvaluator
	ClientConfig       ClientConfig
	ClientConfigToken  string
	// DisableAutoDeployOnIngest turns off the reconcile that normally follows a
	// successful repo ingest. Auto-deploy is on unless this is set.
	DisableAutoDeployOnIngest bool
	// Unmanaged detects CloudFormation stacks without a repository and
	// prepares repo associations. Optional.
	Unmanaged UnmanagedService
	// Access is the repo-managed access policy service. Nil (or disabled)
	// keeps every read visible.
	Access *access.Service
	// AccessPolicyToken authenticates /api/access/policy pollers. When empty
	// the endpoint is open to loopback callers only.
	AccessPolicyToken string
}

type ClientConfig struct {
	MonoFS *MonoFSClientConfig `json:"monofs,omitempty"`
}

type MonoFSClientConfig struct {
	RouterAddr           string `json:"routerAddr,omitempty"`
	Token                string `json:"token,omitempty"`
	UseExternalAddresses bool   `json:"useExternalAddresses"`
}

type Server struct {
	store              guardianapi.Store
	dispatcher         *dispatcher.Dispatcher
	principalID        string
	pushers            []string
	staleTaskAfter     time.Duration
	enableFlowTopology bool
	flowEvaluator      FlowStateEvaluator
	partitionData      *partitionDataCache
	rolloutCache       *rolloutCache
	projectionCache    *uiProjectionCache
	clientConfig       ClientConfig
	clientConfigToken  string
	autoDeployOnIngest bool
	repoImportMu       sync.Mutex
	unmanaged          UnmanagedService
	access             *access.Service
	accessPolicyToken  string
	index              *template.Template
	static             http.Handler
	mux                http.Handler
}

const defaultUIDataCacheTTL = 3 * time.Second
const defaultRolloutWindow = 12 * time.Hour
const defaultRolloutTTL = 1 * time.Minute

type partitionDataCache struct {
	ttl     time.Duration
	mu      sync.RWMutex
	entries map[string]partitionDataCacheEntry
}

type partitionDataCacheEntry struct {
	expiresAt time.Time
	payload   *PartitionDetailResponse
}

func newPartitionDataCache(ttl time.Duration) *partitionDataCache {
	if ttl <= 0 {
		return nil
	}
	return &partitionDataCache{
		ttl:     ttl,
		entries: make(map[string]partitionDataCacheEntry),
	}
}

func (c *partitionDataCache) get(key string, now time.Time) (*PartitionDetailResponse, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if now.After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return nil, false
	}
	return entry.payload, true
}

func (c *partitionDataCache) set(key string, now time.Time, payload *PartitionDetailResponse) {
	if c == nil || payload == nil {
		return
	}
	c.mu.Lock()
	c.entries[key] = partitionDataCacheEntry{
		expiresAt: now.Add(c.ttl),
		payload:   payload,
	}
	c.mu.Unlock()
}

func (c *partitionDataCache) invalidatePartition(partitionName string) {
	if c == nil {
		return
	}
	prefix := partitionName + "|"
	c.mu.Lock()
	for key := range c.entries {
		if strings.HasPrefix(key, prefix) {
			delete(c.entries, key)
		}
	}
	c.mu.Unlock()
}

type rolloutCache struct {
	ttl     time.Duration
	mu      sync.RWMutex
	entries map[string]rolloutCacheEntry
}

type rolloutCacheEntry struct {
	expiresAt time.Time
	payload   *PartitionRolloutsResponse
}

func newRolloutCache(ttl time.Duration) *rolloutCache {
	if ttl <= 0 {
		return nil
	}
	return &rolloutCache{
		ttl:     ttl,
		entries: make(map[string]rolloutCacheEntry),
	}
}

func (c *rolloutCache) get(key string, now time.Time) (*PartitionRolloutsResponse, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if now.After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return nil, false
	}
	return entry.payload, true
}

func (c *rolloutCache) set(key string, now time.Time, payload *PartitionRolloutsResponse) {
	if c == nil || payload == nil {
		return
	}
	c.mu.Lock()
	c.entries[key] = rolloutCacheEntry{
		expiresAt: now.Add(c.ttl),
		payload:   payload,
	}
	c.mu.Unlock()
}

func New(opts Options) (*Server, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("ui store is required")
	}
	index, err := template.ParseFS(templateFS, "templates/index.html")
	if err != nil {
		return nil, fmt.Errorf("parse ui template: %w", err)
	}
	staticRoot, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("locate ui static assets: %w", err)
	}
	pushers := append([]string(nil), opts.Pushers...)
	sort.Strings(pushers)
	staleTaskAfter := opts.StaleTaskAfter
	if staleTaskAfter <= 0 {
		staleTaskAfter = defaultStaleTaskAfter
	}
	dataCacheTTL := opts.DataCacheTTL
	if dataCacheTTL <= 0 {
		dataCacheTTL = defaultUIDataCacheTTL
	}
	s := &Server{
		store:              opts.Store,
		dispatcher:         opts.Dispatcher,
		principalID:        strings.TrimSpace(opts.PrincipalID),
		pushers:            pushers,
		staleTaskAfter:     staleTaskAfter,
		enableFlowTopology: opts.EnableFlowTopology,
		flowEvaluator:      opts.FlowEvaluator,
		partitionData:      newPartitionDataCache(dataCacheTTL),
		rolloutCache:       newRolloutCache(defaultRolloutTTL),
		projectionCache:    newUIProjectionCache(opts.Store, dataCacheTTL),
		clientConfig:       opts.ClientConfig,
		clientConfigToken:  strings.TrimSpace(opts.ClientConfigToken),
		autoDeployOnIngest: !opts.DisableAutoDeployOnIngest,
		unmanaged:          opts.Unmanaged,
		access:             opts.Access,
		accessPolicyToken:  strings.TrimSpace(opts.AccessPolicyToken),
		index:              index,
		static:             http.StripPrefix("/static/", http.FileServer(http.FS(staticRoot))),
	}
	if s.flowEvaluator == nil {
		s.flowEvaluator = defaultFlowStateEvaluator()
	}
	if s.principalID == "" {
		s.principalID = "guardian-ui"
	}
	s.mux = s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", s.static)
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/api/v1/status/buildinfo", s.handleBuildInfo)
	mux.HandleFunc("/api/client-config", s.handleClientConfig)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/access/policy", s.handleAccessPolicy)
	mux.HandleFunc("/api/access/explain", s.handleAccessExplain)
	mux.HandleFunc("/api/m2m/overview", s.handleM2MOverview)
	mux.HandleFunc("/api/m2m/federation/partitions", s.handleM2MFederationPartitions)
	mux.HandleFunc("/api/m2m/partitions", s.handleM2MPartitions)
	mux.HandleFunc("/api/m2m/partitions/", s.handleM2MPartitionRoute)
	mux.HandleFunc("/api/m2m/pushers", s.handleM2MPushers)
	mux.HandleFunc("/api/m2m/pushers/", s.handleM2MPusherRoute)
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/catalog", s.handleCatalog)
	mux.HandleFunc("/api/partitions", s.handlePartitions)
	mux.HandleFunc("/api/partitions/", s.handlePartitionRoute)
	mux.HandleFunc("/api/scans", s.handleScans)
	mux.HandleFunc("/api/scans/", s.handleScanRoute)
	mux.HandleFunc("/api/pushers", s.handlePushers)
	mux.HandleFunc("/api/pushers/", s.handlePusherRoute)
	mux.HandleFunc("/api/probes", s.handleProbes)
	mux.HandleFunc("/api/probes/", s.handleProbeRoute)
	mux.HandleFunc("/api/observability/identities", s.handleObservabilityIdentities)
	mux.HandleFunc("/api/repos", s.handleRepos)
	mux.HandleFunc("/api/repos/", s.handleRepoRoute)
	mux.HandleFunc("/api/aws/unmanaged", s.handleUnmanaged)
	mux.HandleFunc("/api/aws/unmanaged/", s.handleUnmanagedRoute)
	mux.HandleFunc("/api/sets", s.handleSets)
	mux.HandleFunc("/api/sets/", s.handleSetRoute)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	build := buildinfo.Current()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := s.index.Execute(&buf, map[string]any{
		"Title":              "Guardian Control Center",
		"BuildVersionLabel":  indexBuildVersionLabel(build),
		"BuildVersionDetail": indexBuildVersionDetail(build),
	}); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	buf.WriteTo(w)
}

func indexBuildVersionLabel(info buildinfo.Info) string {
	version := strings.TrimSpace(info.Version)
	if version == "" {
		version = "dev"
	}
	revision := strings.TrimSpace(info.Revision)
	if revision != "" && revision != "unknown" {
		if len(revision) > 8 {
			revision = revision[:8]
		}
		version = fmt.Sprintf("%s+%s", version, revision)
	}
	if info.Modified {
		version += "*"
	}
	return version
}

func indexBuildVersionDetail(info buildinfo.Info) string {
	parts := []string{fmt.Sprintf("version: %s", strings.TrimSpace(info.Version))}
	if revision := strings.TrimSpace(info.Revision); revision != "" {
		parts = append(parts, fmt.Sprintf("revision: %s", revision))
	}
	if branch := strings.TrimSpace(info.Branch); branch != "" {
		parts = append(parts, fmt.Sprintf("branch: %s", branch))
	}
	if buildDate := strings.TrimSpace(info.BuildDate); buildDate != "" {
		parts = append(parts, fmt.Sprintf("built: %s", buildDate))
	}
	if goVersion := strings.TrimSpace(info.GoVersion); goVersion != "" {
		parts = append(parts, fmt.Sprintf("go: %s", goVersion))
	}
	if info.Modified {
		parts = append(parts, "working tree modified")
	}
	return strings.Join(parts, " | ")
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleMe reports the authenticated caller so the UI can show who is signed
// in. The access-policy middleware has already resolved the identity.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	identity, _ := coreauthz.IdentityFromContext(r.Context())
	display := identity.Email
	if display == "" {
		display = identity.Subject
	}
	if display == "" {
		display = identity.ClientID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": !identity.IsAnonymous(),
		"subject":       identity.Subject,
		"email":         identity.Email,
		"groups":        identity.Groups,
		"client_id":     identity.ClientID,
		"display":       display,
	})
}

func (s *Server) handleBuildInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   buildinfo.Current().StatusFields(),
	})
}

func (s *Server) handleClientConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !requestCanReadClientConfig(r, s.clientConfigToken) {
		writeError(w, http.StatusForbidden, fmt.Errorf("client config requires loopback access or a valid discovery token"))
		return
	}
	if s.clientConfig.MonoFS == nil || strings.TrimSpace(s.clientConfig.MonoFS.RouterAddr) == "" {
		writeError(w, http.StatusNotFound, fmt.Errorf("client config unavailable"))
		return
	}
	writeJSON(w, http.StatusOK, s.clientConfig)
}

// handleAccessPolicy serves the compiled access policy bundle to MonoFS and
// Doctor. Access requires the internal token (when configured), the admin
// role, or — with no token configured — a loopback caller.
func (s *Server) handleAccessPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.access == nil || !s.access.Enabled() {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("access policy is not enabled"))
		return
	}
	if !s.requestCanReadAccessPolicy(r) {
		status := http.StatusForbidden
		if _, ok := coreauthz.IdentityFromContext(r.Context()); !ok {
			status = http.StatusUnauthorized
		}
		writeError(w, status, fmt.Errorf("access policy requires the internal token, the admin role, or loopback access"))
		return
	}
	content, err := s.access.PolicyJSON()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", `"`+s.access.Version()+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// handleAccessExplain reports why a subject has the access it has. It is
// admin-only.
func (s *Server) handleAccessExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.access == nil || !s.access.Enabled() {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("access policy is not enabled"))
		return
	}
	if !s.access.IsAdmin(r.Context()) {
		writeError(w, http.StatusForbidden, fmt.Errorf("access explain requires the admin role"))
		return
	}
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	if user == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("user query parameter is required"))
		return
	}
	var groups []string
	if raw := strings.TrimSpace(r.URL.Query().Get("groups")); raw != "" {
		for _, group := range strings.Split(raw, ",") {
			if group = strings.TrimSpace(group); group != "" {
				groups = append(groups, group)
			}
		}
	}
	writeJSON(w, http.StatusOK, s.access.Explain(r.Context(), user, groups))
}

// requestCanReadAccessPolicy mirrors the client-config discovery auth: a
// matching internal token, an admin identity, or loopback when no token is
// configured.
func (s *Server) requestCanReadAccessPolicy(r *http.Request) bool {
	token := strings.TrimSpace(s.accessPolicyToken)
	if token != "" {
		provided := strings.TrimSpace(r.Header.Get("X-Guardian-Internal-Token"))
		if provided == "" {
			authorization := strings.TrimSpace(r.Header.Get("Authorization"))
			if strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
				provided = strings.TrimSpace(authorization[len("Bearer "):])
			}
		}
		if provided != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1 {
			return true
		}
	} else if requestIsLoopback(r) {
		return true
	}
	return s.access != nil && s.access.IsAdmin(r.Context())
}

// canSeePartition applies the repo-managed access policy to one partition.
func (s *Server) canSeePartition(ctx context.Context, partition string) bool {
	if s.access == nil || !s.access.Enabled() {
		return true
	}
	return s.access.CanSee(ctx, partition)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	payload, err := s.buildOverview(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleM2MOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	payload, err := s.buildOverview(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, s.buildCatalog())
}

func (s *Server) handlePartitions(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/partitions" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		payload, err := s.buildOverview(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, payload.Partitions)
	default:
		writeMethodNotAllowed(w, http.MethodGet)
	}
}

func (s *Server) handleM2MPartitions(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/m2m/partitions" {
		http.NotFound(w, r)
		return
	}
	cloned := r.Clone(r.Context())
	cloned.URL.Path = "/api/partitions"
	s.handlePartitions(w, cloned)
}

func (s *Server) handleM2MPartitionRoute(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/m2m/partitions/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	cloned := r.Clone(r.Context())
	cloned.URL.Path = "/api/partitions/" + trimmed
	s.handlePartitionRoute(w, cloned)
}

func (s *Server) handlePartitionRoute(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/partitions/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(trimmed, "/")
	name := parts[0]
	// Repo-managed access policy: hide GETs on partitions the caller cannot
	// see (DELETE/PUT/POST keep their existing authz behavior).
	if r.Method == http.MethodGet && !s.canSeePartition(r.Context(), name) {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			payload, err := s.loadPartitionDetailEnriched(r.Context(), name)
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, os.ErrNotExist) {
					status = http.StatusNotFound
				}
				writeError(w, status, err)
				return
			}
			writeJSON(w, http.StatusOK, payload)
		case http.MethodDelete:
			removed, purged, err := s.deletePartition(r.Context(), name)
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, os.ErrNotExist) {
					status = http.StatusNotFound
				}
				writeError(w, status, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"success":            true,
				"partition":          name,
				"deletedPaths":       removed,
				"deletionInProgress": !purged,
			})
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
		}
		return
	}

	switch parts[1] {
	case "topology":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		payload, err := s.loadPartitionDetail(r.Context(), name)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, payload.Topology)
	case "rollouts":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		opts, err := parsePartitionHistoryOptions(r.URL.Query())
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if opts.Since == nil {
			since := time.Now().UTC().Add(-defaultRolloutWindow)
			opts.Since = &since
		}
		if r.URL.Query().Get("limit") == "" {
			opts.LimitPerIntent = historyquery.DefaultRolloutLimit
		}
		cacheKey := fmt.Sprintf("rollouts:%s:%s:%s:%d", name,
			timePtrStr(opts.Since), timePtrStr(opts.Until), opts.LimitPerIntent)
		now := time.Now()
		if cached, ok := s.rolloutCache.get(cacheKey, now); ok {
			writeJSON(w, http.StatusOK, cached)
			return
		}
		rollouts, err := s.loadPartitionRollouts(r.Context(), name, opts)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		s.rolloutCache.set(cacheKey, now, rollouts)
		writeJSON(w, http.StatusOK, rollouts)
	case "history":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		opts, err := parsePartitionHistoryOptions(r.URL.Query())
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		history, err := s.loadPartitionHistory(r.Context(), name, opts)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, history)
	case "bundle":
		if r.Method != http.MethodPut {
			writeMethodNotAllowed(w, http.MethodPut)
			return
		}
		var req SaveBundleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
			return
		}
		resp, err := s.saveBundle(r.Context(), name, &req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case "validate":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var req SaveBundleRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
			return
		}
		writeJSON(w, http.StatusOK, s.validateBundle(r.Context(), name, &req))
	case "reconcile":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		forceApply := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("forceApply"))) == "true"
		var err error
		if forceApply {
			err = s.reconcilePartitionForceApply(r.Context(), name)
		} else {
			err = s.reconcilePartition(r.Context(), name)
		}
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":    true,
			"partition":  name,
			"forceApply": forceApply,
		})
	case "intents":
		if len(parts) != 4 || parts[3] != "activity" {
			http.NotFound(w, r)
			return
		}
		intentName := parts[2]
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		activity, err := s.loadIntentActivity(r.Context(), name, intentName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, activity)
	default:
		http.NotFound(w, r)
	}
}

func parsePartitionHistoryOptions(values url.Values) (PartitionHistoryOptions, error) {
	opts := PartitionHistoryOptions{
		LimitPerIntent: historyquery.DefaultDeploymentLimit,
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return PartitionHistoryOptions{}, fmt.Errorf("limit must be a positive integer")
		}
		opts.LimitPerIntent = limit
	}
	if raw := strings.TrimSpace(values.Get("since")); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return PartitionHistoryOptions{}, fmt.Errorf("since must be RFC3339")
		}
		opts.Since = &parsed
	}
	if raw := strings.TrimSpace(values.Get("until")); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return PartitionHistoryOptions{}, fmt.Errorf("until must be RFC3339")
		}
		opts.Until = &parsed
	}
	filter := historyquery.DeploymentFilter{
		Limit: opts.LimitPerIntent,
		Since: opts.Since,
		Until: opts.Until,
	}
	if err := filter.Validate(); err != nil {
		return PartitionHistoryOptions{}, err
	}
	return opts, nil
}

var assetErrorPattern = regexp.MustCompile(`^asset "([^"]+)"`)

func (s *Server) validateBundle(ctx context.Context, partitionName string, req *SaveBundleRequest) *ValidateBundleResponse {
	response := &ValidateBundleResponse{Valid: true}
	addIssue := func(issue ValidateBundleIssue) {
		if issue.Severity == "" {
			issue.Severity = "error"
		}
		response.Valid = false
		response.Errors = append(response.Errors, issue)
	}
	if req == nil {
		addIssue(ValidateBundleIssue{Message: "request body is required"})
		return response
	}

	partition := req.Partition
	partition.APIVersion = defaultString(partition.APIVersion, "guardian/v1alpha1")
	partition.Kind = defaultString(partition.Kind, "Partition")
	if strings.TrimSpace(partition.Metadata.Name) == "" {
		partition.Metadata.Name = partitionName
	}
	for i := range req.Intents {
		req.Intents[i].Manifest.APIVersion = defaultString(req.Intents[i].Manifest.APIVersion, "guardian/v1alpha1")
		req.Intents[i].Manifest.Kind = defaultString(req.Intents[i].Manifest.Kind, "Intent")
	}
	if partition.Metadata.Name != partitionName {
		addIssue(ValidateBundleIssue{Message: fmt.Sprintf("partition metadata.name %q does not match path %q", partition.Metadata.Name, partitionName)})
	}
	if err := validatePartitionForSave(partition); err != nil {
		addIssue(ValidateBundleIssue{Message: err.Error()})
	}

	intentNames := make([]string, 0, len(req.Intents))
	intentSeen := map[string]struct{}{}
	for i := range req.Intents {
		name := strings.TrimSpace(req.Intents[i].Manifest.Metadata.Name)
		if name == "" {
			addIssue(ValidateBundleIssue{Message: fmt.Sprintf("intent at index %d is missing metadata.name", i)})
			continue
		}
		if _, exists := intentSeen[name]; exists {
			addIssue(ValidateBundleIssue{Intent: name, Message: fmt.Sprintf("duplicate intent %q", name)})
			continue
		}
		intentSeen[name] = struct{}{}
		intentNames = append(intentNames, name)
	}
	sort.Strings(intentNames)

	knownPushers := s.knownPushers(ctx)
	for i := range req.Intents {
		name := strings.TrimSpace(req.Intents[i].Manifest.Metadata.Name)
		if name == "" {
			continue
		}
		if err := validateIntentForSave(req.Intents[i].Manifest, intentNames, knownPushers); err != nil {
			issue := ValidateBundleIssue{Intent: name, Message: err.Error()}
			if match := assetErrorPattern.FindStringSubmatch(err.Error()); match != nil {
				issue.Asset = match[1]
			}
			addIssue(issue)
		}
	}
	return response
}

func (s *Server) saveBundle(ctx context.Context, partitionName string, req *SaveBundleRequest) (*SaveBundleResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request body is required")
	}
	req.Partition.APIVersion = defaultString(req.Partition.APIVersion, "guardian/v1alpha1")
	req.Partition.Kind = defaultString(req.Partition.Kind, "Partition")
	req.Partition.Metadata.Name = defaultString(req.Partition.Metadata.Name, partitionName)
	if req.Partition.Metadata.Name != partitionName {
		return nil, fmt.Errorf("partition metadata.name %q does not match path %q", req.Partition.Metadata.Name, partitionName)
	}
	if err := validatePartitionForSave(req.Partition); err != nil {
		return nil, err
	}

	intentNames := make([]string, 0, len(req.Intents))
	intentSeen := map[string]struct{}{}
	for i := range req.Intents {
		req.Intents[i].Manifest.APIVersion = defaultString(req.Intents[i].Manifest.APIVersion, "guardian/v1alpha1")
		req.Intents[i].Manifest.Kind = defaultString(req.Intents[i].Manifest.Kind, "Intent")
		if strings.TrimSpace(req.Intents[i].Manifest.Metadata.Name) == "" {
			return nil, fmt.Errorf("intent at index %d is missing metadata.name", i)
		}
		if _, exists := intentSeen[req.Intents[i].Manifest.Metadata.Name]; exists {
			return nil, fmt.Errorf("duplicate intent %q", req.Intents[i].Manifest.Metadata.Name)
		}
		intentSeen[req.Intents[i].Manifest.Metadata.Name] = struct{}{}
		intentNames = append(intentNames, req.Intents[i].Manifest.Metadata.Name)
	}
	sort.Strings(intentNames)
	for i := range req.Intents {
		if err := validateIntentForSave(req.Intents[i].Manifest, intentNames, s.knownPushers(ctx)); err != nil {
			return nil, err
		}
	}

	partitionContent, err := yaml.Marshal(req.Partition)
	if err != nil {
		return nil, fmt.Errorf("marshal partition: %w", err)
	}
	writes := []guardianapi.PathWrite{{
		LogicalPath:       paths.PartitionConfig(partitionName),
		Content:           partitionContent,
		ExpectedVersionID: req.PartitionExpectedVersionID,
	}}
	existingSources, err := s.intentManifestSources(ctx, partitionName)
	if err != nil {
		return nil, err
	}
	existingPaths := make(map[string]string, len(existingSources))
	for _, source := range existingSources {
		existingPaths[source.Name] = source.LogicalPath
	}
	intentPaths := make(map[string]string, len(req.Intents))
	sort.Slice(req.Intents, func(i, j int) bool {
		return req.Intents[i].Manifest.Metadata.Name < req.Intents[j].Manifest.Metadata.Name
	})
	for _, intent := range req.Intents {
		content, err := yaml.Marshal(intent.Manifest)
		if err != nil {
			return nil, fmt.Errorf("marshal intent %s: %w", intent.Manifest.Metadata.Name, err)
		}
		logicalPath := paths.IntentManifest(partitionName, intent.Manifest.Metadata.Name)
		if existingPath, ok := existingPaths[intent.Manifest.Metadata.Name]; ok {
			logicalPath = existingPath
		}
		intentPaths[logicalPath] = intent.Manifest.Metadata.Name
		writes = append(writes, guardianapi.PathWrite{
			LogicalPath:       logicalPath,
			Content:           content,
			ExpectedVersionID: intent.ExpectedVersionID,
		})
	}

	result, err := s.store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: writes,
		Context: guardianapi.MutationContext{
			PrincipalID:   s.principalID,
			Reason:        "save bundle from guardian ui",
			CorrelationID: newCorrelationID(),
		},
	})
	if err != nil {
		return nil, err
	}
	s.partitionData.invalidatePartition(partitionName)

	removed := make([]string, 0)
	if req.RemoveMissingIntents {
		missing := make([]string, 0)
		for existing := range existingPaths {
			if _, ok := intentSeen[existing]; ok {
				continue
			}
			missing = append(missing, existing)
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			deletes := make([]guardianapi.PathDelete, 0, len(missing))
			for _, name := range missing {
				deletes = append(deletes, guardianapi.PathDelete{LogicalPath: existingPaths[name]})
			}
			if _, err := s.store.DeletePaths(ctx, guardianapi.DeleteBatch{
				Deletes: deletes,
				Context: guardianapi.MutationContext{
					PrincipalID:   s.principalID,
					Reason:        "remove intents from guardian ui bundle",
					CorrelationID: newCorrelationID(),
				},
			}); err != nil {
				return nil, err
			}
			removed = missing
		}
	}

	s.writeEvent(ctx, &historydomain.EventRecord{
		Partition: partitionName,
		Type:      "ui.bundle.saved",
		Message:   "saved partition bundle from ui",
		Details: map[string]string{
			"intentCount":          fmt.Sprintf("%d", len(req.Intents)),
			"removeMissingIntents": fmt.Sprintf("%t", req.RemoveMissingIntents),
		},
	})
	for _, name := range removed {
		s.writeEvent(ctx, &historydomain.EventRecord{
			Partition: partitionName,
			Intent:    name,
			Type:      "ui.intent.removed",
			Message:   "intent removed from ui bundle",
		})
	}

	resp := &SaveBundleResponse{
		Success:          true,
		BatchRevisionID:  result.BatchRevisionID,
		IntentVersionIDs: map[string]string{},
		RemovedIntents:   removed,
	}
	for _, file := range result.Files {
		switch file.LogicalPath {
		case paths.PartitionConfig(partitionName):
			resp.PartitionVersionID = file.VersionID
		default:
			intent, ok := intentPaths[file.LogicalPath]
			if ok {
				resp.IntentVersionIDs[intent] = file.VersionID
			}
		}
	}
	return resp, nil
}

// autoDeployPartitions reconciles every partition fed by a just-ingested repo.
// It is best-effort: ingest succeeded, so a reconcile failure is logged and
// retried by the partition watcher / schedule.
func (s *Server) autoDeployPartitions(ctx context.Context, names []string) {
	if !s.autoDeployOnIngest || len(names) == 0 {
		return
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if err := s.reconcilePartition(ctx, name); err != nil {
			log.Printf("guardian ui: auto-deploy partition %s after repo ingest: %v", name, err)
		}
	}
}

func (s *Server) reconcilePartition(ctx context.Context, partitionName string) error {
	disp := s.dispatcher
	if disp == nil {
		disp = dispatcher.NewDispatcher(s.store, s.principalID)
	}
	recon := reconciler.NewReconciler(s.store, disp, time.Minute)
	if err := recon.ReconcilePartition(ctx, partitionName, true); err != nil {
		return err
	}
	s.partitionData.invalidatePartition(partitionName)
	s.writeEvent(ctx, &historydomain.EventRecord{
		Partition: partitionName,
		Type:      "ui.reconcile.requested",
		Message:   "reconciliation triggered from ui",
	})
	return nil
}

func (s *Server) reconcilePartitionForceApply(ctx context.Context, partitionName string) error {
	disp := s.dispatcher
	if disp == nil {
		disp = dispatcher.NewDispatcher(s.store, s.principalID)
	}
	recon := reconciler.NewReconciler(s.store, disp, time.Minute)
	if err := recon.ForceApplyPartition(ctx, partitionName, true); err != nil {
		return err
	}
	s.partitionData.invalidatePartition(partitionName)
	s.writeEvent(ctx, &historydomain.EventRecord{
		Partition: partitionName,
		Type:      "ui.reconcile.force-apply",
		Message:   "force-apply reconciliation triggered from ui",
	})
	return nil
}

// deletePartition requests a deferred partition deletion and runs one
// reconcile pass. Under the destroy policy this queues DESTROY tasks for every
// intent; the partition files are purged by the reconciler once teardown
// completes. Under orphan policy the partition is purged in this call.
// It returns whether the partition is already purged.
func (s *Server) deletePartition(ctx context.Context, partitionName string) (int, bool, error) {
	if _, err := reconciler.RequestPartitionDeletion(ctx, s.store, partitionName, s.principalID); err != nil {
		return 0, false, err
	}
	disp := s.dispatcher
	if disp == nil {
		disp = dispatcher.NewDispatcher(s.store, s.principalID)
	}
	recon := reconciler.NewReconciler(s.store, disp, time.Minute)
	if err := recon.ReconcilePartition(ctx, partitionName, true); err != nil {
		return 0, false, err
	}
	if _, err := s.store.Stat(ctx, paths.PartitionConfig(partitionName)); errors.Is(err, os.ErrNotExist) {
		s.partitionData.invalidatePartition(partitionName)
		return 1, true, nil
	}
	return 0, false, nil
}

func (s *Server) writeEvent(ctx context.Context, event *historydomain.EventRecord) {
	if s.dispatcher == nil || event == nil {
		return
	}
	_ = s.dispatcher.WriteEvent(ctx, event)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed ...string) {
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
	}
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
}

func requestIsLoopback(r *http.Request) bool {
	host := strings.TrimSpace(r.RemoteAddr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func requestCanReadClientConfig(r *http.Request, expectedToken string) bool {
	if requestIsLoopback(r) {
		return true
	}
	expectedToken = strings.TrimSpace(expectedToken)
	if expectedToken == "" {
		return false
	}
	provided := strings.TrimSpace(r.Header.Get("X-Guardian-Discovery-Token"))
	if provided == "" {
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
			provided = strings.TrimSpace(authorization[len("Bearer "):])
		}
	}
	if provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expectedToken)) == 1
}

func validatePartitionForSave(partition any) error {
	content, err := yaml.Marshal(partition)
	if err != nil {
		return err
	}
	return validatePartitionContent(content)
}

func validateIntentForSave(intent any, knownIntents []string, knownPushers []string) error {
	content, err := yaml.Marshal(intent)
	if err != nil {
		return err
	}
	return validateIntentContent(content, knownIntents, knownPushers)
}

func validatePartitionContent(content []byte) error {
	parsed, err := parsePartition(content)
	if err != nil {
		return err
	}
	return validatePartition(parsed)
}

func validateIntentContent(content []byte, knownIntents []string, knownPushers []string) error {
	parsed, err := parseIntent(content)
	if err != nil {
		return err
	}
	return validateIntent(parsed, knownIntents, knownPushers)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func timePtrStr(t *time.Time) string {
	if t == nil {
		return "nil"
	}
	return t.Format(time.RFC3339)
}

func newCorrelationID() string {
	return revisions.NewCorrelationID()
}

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 15*time.Second)
}
