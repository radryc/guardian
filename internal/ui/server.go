package ui

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rydzu/ainfra/guardian/internal/buildinfo"
	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	"github.com/rydzu/ainfra/guardian/internal/historyquery"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/dispatcher"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/reconciler"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*.css static/*.js static/*.png
var staticFS embed.FS

type Options struct {
	Store             guardianapi.Store
	Dispatcher        *dispatcher.Dispatcher
	PrincipalID       string
	Pushers           []string
	StaleTaskAfter    time.Duration
	DataCacheTTL      time.Duration
	ClientConfig      ClientConfig
	ClientConfigToken string
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
	store             guardianapi.Store
	dispatcher        *dispatcher.Dispatcher
	principalID       string
	pushers           []string
	staleTaskAfter    time.Duration
	partitionData     *partitionDataCache
	clientConfig      ClientConfig
	clientConfigToken string
	index             *template.Template
	static            http.Handler
	mux               http.Handler
}

const defaultUIDataCacheTTL = 3 * time.Second

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
		store:             opts.Store,
		dispatcher:        opts.Dispatcher,
		principalID:       strings.TrimSpace(opts.PrincipalID),
		pushers:           pushers,
		staleTaskAfter:    staleTaskAfter,
		partitionData:     newPartitionDataCache(dataCacheTTL),
		clientConfig:      opts.ClientConfig,
		clientConfigToken: strings.TrimSpace(opts.ClientConfigToken),
		index:             index,
		static:            http.StripPrefix("/static/", http.FileServer(http.FS(staticRoot))),
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
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/catalog", s.handleCatalog)
	mux.HandleFunc("/api/partitions", s.handlePartitions)
	mux.HandleFunc("/api/partitions/", s.handlePartitionRoute)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.index.Execute(w, map[string]any{
		"Title": "Guardian Control Center",
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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

func (s *Server) handlePartitionRoute(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/partitions/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(trimmed, "/")
	name := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			payload, err := s.loadPartitionDetail(r.Context(), name)
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
			removed, err := s.deletePartition(r.Context(), name)
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, os.ErrNotExist) {
					status = http.StatusNotFound
				}
				writeError(w, status, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"success":      true,
				"partition":    name,
				"deletedPaths": removed,
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
		rollouts, err := s.loadPartitionRollouts(r.Context(), name, opts)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
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
	case "reconcile":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		if err := s.reconcilePartition(r.Context(), name); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":   true,
			"partition": name,
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
		if err := validateIntentForSave(req.Intents[i].Manifest, intentNames, s.pushers); err != nil {
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

func (s *Server) deletePartition(ctx context.Context, partitionName string) (int, error) {
	files, err := s.walkFiles(ctx, paths.PartitionRoot(partitionName))
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, os.ErrNotExist
	}
	deletes := make([]guardianapi.PathDelete, 0, len(files))
	for _, file := range files {
		deletes = append(deletes, guardianapi.PathDelete{LogicalPath: file})
	}
	if _, err := s.store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: deletes,
		Context: guardianapi.MutationContext{
			PrincipalID:   s.principalID,
			Reason:        "delete partition from guardian ui",
			CorrelationID: newCorrelationID(),
		},
	}); err != nil {
		return 0, err
	}
	s.partitionData.invalidatePartition(partitionName)
	return len(files), nil
}

func (s *Server) walkFiles(ctx context.Context, logicalDir string) ([]string, error) {
	entries, err := s.store.ListDir(ctx, logicalDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0)
	for _, entry := range entries {
		child := strings.TrimRight(logicalDir, "/") + "/" + entry.Name
		if entry.IsDir {
			nested, err := s.walkFiles(ctx, child)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
			continue
		}
		out = append(out, child)
	}
	sort.Strings(out)
	return out, nil
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

func newCorrelationID() string {
	return revisions.NewCorrelationID()
}

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 15*time.Second)
}
