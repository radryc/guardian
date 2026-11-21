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
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	neturl "net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/radryc/guardian/internal/access"
	"github.com/radryc/guardian/internal/buildinfo"
	displaypkg "github.com/radryc/guardian/internal/display"
	historydomain "github.com/radryc/guardian/internal/domain/history"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	"github.com/radryc/guardian/internal/historyquery"
	"github.com/radryc/guardian/internal/orchestrator/dispatcher"
	"github.com/radryc/guardian/internal/orchestrator/reconciler"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/versioning/revisions"
	"github.com/radryc/guardian/pkg/guardianapi"
	"github.com/radryc/monofs/pkg/audit"
	coreauthz "github.com/radryc/monofs/pkg/authz"
	"github.com/radryc/monofs/pkg/display"
	"gopkg.in/yaml.v3"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*.css static/*.js static/*.png static/fonts/*.woff2
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
	// ClientConfigIncludeToken includes MonoFS.Token in /api/client-config
	// responses. Disabled by default so credentials are not leaked.
	ClientConfigIncludeToken bool
	// DisableAutoDeployOnIngest turns off the reconcile that normally follows a
	// successful repo ingest. Auto-deploy is on unless this is set.
	DisableAutoDeployOnIngest bool
	// Unmanaged detects CloudFormation stacks without a repository and
	// prepares repo associations. Optional.
	Unmanaged UnmanagedService
	// Access is the repo-managed access policy service. Nil (or disabled)
	// keeps every read visible, but mutations are denied unless
	// AllowUnpolicedMutations is set.
	Access *access.Service
	// AllowUnpolicedMutations permits mutating UI/API requests while no
	// enforcing access policy is loaded. It defaults to false (fail closed) and
	// must be opted into by local/bootstrap installs.
	AllowUnpolicedMutations bool
	// AccessPolicyToken authenticates /api/access/policy pollers. When empty
	// the endpoint is open to loopback callers only.
	AccessPolicyToken string
	// RequireAuthentication rejects anonymous UI/API mutations. It is enabled
	// by default in production deployments and can be disabled for local
	// development with an explicit escape hatch.
	RequireAuthentication bool
	// AllowLoopbackAnonymous keeps local development usable when no identity
	// provider is configured. It only applies to loopback callers and should be
	// disabled in shared deployments.
	AllowLoopbackAnonymous bool
	// TrustedOrigins lists additional scheme://host origins allowed to submit
	// browser mutations (for example an embedding control plane).
	TrustedOrigins []string
	// EnableHSTS emits Strict-Transport-Security even when TLS terminates at a
	// proxy in front of Guardian.
	EnableHSTS bool
	// AuditEmitter records one access event per UI/API request (including
	// denials) in the shared audit trail.
	AuditEmitter audit.Emitter
	// DisplayProfiles stores presentation-only display-key profiles.
	DisplayProfiles *displaypkg.Service
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
	store                    guardianapi.Store
	dispatcher               *dispatcher.Dispatcher
	principalID              string
	pushers                  []string
	staleTaskAfter           time.Duration
	enableFlowTopology       bool
	flowEvaluator            FlowStateEvaluator
	partitionData            *partitionDataCache
	rolloutCache             *rolloutCache
	projectionCache          *uiProjectionCache
	partitionLoadMu          sync.Mutex
	partitionLoads           map[string]*partitionLoad
	clientConfig             ClientConfig
	clientConfigToken        string
	clientConfigIncludeToken bool
	autoDeployOnIngest       bool
	repoImportMu             sync.Mutex
	unmanaged                UnmanagedService
	access                   *access.Service
	allowUnpolicedMutations  bool
	accessPolicyToken        string
	requireAuthentication    bool
	allowLoopbackAnonymous   bool
	trustedOrigins           []string
	enableHSTS               bool
	auditEmitter             audit.Emitter
	displayProfiles          *displaypkg.Service
	index                    *template.Template
	static                   http.Handler
	mux                      http.Handler
}

const defaultUIDataCacheTTL = 3 * time.Second

// defaultUIOverviewCacheTTL is the soft TTL for the partition overview, which
// is polled by several components and is expensive to rebuild.
const defaultUIOverviewCacheTTL = 15 * time.Second

// defaultUIDataStaleTTL is how long an expired overview/partition payload may
// still be served while a background refresh runs. Overview scans take a few
// seconds and are polled by several components; serving stale data avoids
// turning every poll into a full partition-tree scan.
const defaultUIDataStaleTTL = 2 * time.Minute

const defaultRolloutWindow = 12 * time.Hour
const defaultRolloutTTL = 1 * time.Minute

type partitionDataCache struct {
	ttl         time.Duration
	overviewTTL time.Duration
	staleTTL    time.Duration
	mu          sync.RWMutex
	entries     map[string]partitionDataCacheEntry
}

type partitionDataCacheEntry struct {
	expiresAt  time.Time
	staleUntil time.Time
	payload    *PartitionDetailResponse
}

// partitionLoad collapses concurrent cache misses for the same partition into a
// single expensive load instead of a stampede against MonoFS.
type partitionLoad struct {
	done    chan struct{}
	payload *PartitionDetailResponse
	err     error
}

func newPartitionDataCache(ttl time.Duration) *partitionDataCache {
	if ttl <= 0 {
		return nil
	}
	return &partitionDataCache{
		ttl:         ttl,
		overviewTTL: defaultUIOverviewCacheTTL,
		staleTTL:    defaultUIDataStaleTTL,
		entries:     make(map[string]partitionDataCacheEntry),
	}
}

func (c *partitionDataCache) overviewTTLOrDefault() time.Duration {
	if c == nil || c.overviewTTL <= 0 {
		return defaultUIOverviewCacheTTL
	}
	if c.overviewTTL < c.ttl {
		return c.ttl
	}
	return c.overviewTTL
}

// getWithState returns a cached payload and whether it is still fresh. When ok
// is true but fresh is false the payload is stale (within staleTTL) and the
// caller should trigger a background refresh.
func (c *partitionDataCache) getWithState(key string, now time.Time) (payload *PartitionDetailResponse, fresh bool, ok bool) {
	if c == nil {
		return nil, false, false
	}
	c.mu.RLock()
	entry, found := c.entries[key]
	c.mu.RUnlock()
	if !found {
		return nil, false, false
	}
	if now.Before(entry.expiresAt) {
		return entry.payload, true, true
	}
	if now.Before(entry.staleUntil) {
		return entry.payload, false, true
	}
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
	return nil, false, false
}

func (c *partitionDataCache) set(key string, now time.Time, payload *PartitionDetailResponse, ttl time.Duration) {
	if c == nil || payload == nil {
		return
	}
	if ttl <= 0 {
		ttl = c.ttl
	}
	c.mu.Lock()
	c.entries[key] = partitionDataCacheEntry{
		expiresAt:  now.Add(ttl),
		staleUntil: now.Add(ttl + c.staleTTL),
		payload:    payload,
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
		store:                    opts.Store,
		dispatcher:               opts.Dispatcher,
		principalID:              strings.TrimSpace(opts.PrincipalID),
		pushers:                  pushers,
		staleTaskAfter:           staleTaskAfter,
		enableFlowTopology:       opts.EnableFlowTopology,
		flowEvaluator:            opts.FlowEvaluator,
		partitionData:            newPartitionDataCache(dataCacheTTL),
		rolloutCache:             newRolloutCache(defaultRolloutTTL),
		projectionCache:          newUIProjectionCache(opts.Store, dataCacheTTL),
		clientConfig:             opts.ClientConfig,
		clientConfigToken:        strings.TrimSpace(opts.ClientConfigToken),
		clientConfigIncludeToken: opts.ClientConfigIncludeToken,
		autoDeployOnIngest:       !opts.DisableAutoDeployOnIngest,
		unmanaged:                opts.Unmanaged,
		access:                   opts.Access,
		allowUnpolicedMutations:  opts.AllowUnpolicedMutations,
		accessPolicyToken:        strings.TrimSpace(opts.AccessPolicyToken),
		requireAuthentication:    opts.RequireAuthentication,
		allowLoopbackAnonymous:   opts.AllowLoopbackAnonymous,
		trustedOrigins:           append([]string(nil), opts.TrustedOrigins...),
		enableHSTS:               opts.EnableHSTS,
		auditEmitter:             opts.AuditEmitter,
		displayProfiles:          opts.DisplayProfiles,
		index:                    index,
		static:                   http.StripPrefix("/static/", http.FileServer(http.FS(staticRoot))),
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
	if s.displayProfiles != nil {
		if bundle, _, err := s.displayProfiles.Bundle(r.Context()); err == nil {
			r.URL.RawQuery = neturl.Values(bundle.TranslateQuery(r.URL.Query())).Encode()
		}
	}
	if s.displayProfiles != nil &&
		strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/display-profiles") {
		// An explicit header/query wins; otherwise resolve the caller's default.
		// serveWithDisplay falls back to canonical when no profile resolves.
		s.serveWithDisplay(w, r, displayRequestedProfile(r))
		return
	}
	s.serveWithAudit(w, r)
}

func (s *Server) serveWithAudit(w http.ResponseWriter, r *http.Request) {
	if s.auditEmitter == nil {
		s.serveHTTP(w, r)
		return
	}
	recorder := &auditStatusRecorder{ResponseWriter: w}
	s.serveHTTP(recorder, r)
	s.emitRequestAudit(r, recorder.status)
}

func (s *Server) serveWithDisplay(w http.ResponseWriter, r *http.Request, profileID string) {
	profile, version, err := s.displayProfiles.Resolve(r.Context(), subjectsFromRequest(r), profileID)
	if err != nil || profile == nil {
		s.serveWithAudit(w, r)
		return
	}
	buffer := &bufferedResponse{header: make(http.Header)}
	s.serveWithAudit(buffer, r)
	body := buffer.body.Bytes()
	if buffer.status >= 200 && buffer.status < 300 && strings.Contains(buffer.header.Get("Content-Type"), "json") {
		keepCanonical := strings.EqualFold(strings.TrimSpace(r.Header.Get(display.KeepCanonicalHeader)), "1") ||
			strings.EqualFold(strings.TrimSpace(r.Header.Get(display.KeepCanonicalHeader)), "true")
		if mapped, mapErr := display.MapJSONWithOptions(body, profile, display.MapOptions{
			Surface:       display.SurfaceForPath(r.URL.Path),
			KeepCanonical: keepCanonical,
		}); mapErr == nil {
			body = mapped
		}
	}
	for key, values := range buffer.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Del("Content-Length")
	w.Header().Set(display.HeaderName, profile.ID)
	if version != "" {
		w.Header().Set(display.VersionHeader, version)
	}
	w.Header().Add("Vary", display.HeaderName)
	w.Header().Set("Cache-Control", "private")
	status := buffer.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }
func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}
func (b *bufferedResponse) Write(data []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(data)
}

func displayRequestedProfile(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(display.HeaderName)); value != "" {
		return value
	}
	return strings.TrimSpace(r.URL.Query().Get(display.QueryParam))
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.applySecurityHeaders(w, r)
	if r.URL.Path != "/api/display-profiles/bundle" || !s.canReadDisplayBundle(r) {
		if !s.checkUIAuth(w, r) {
			return
		}
	}
	if !s.checkMutationOrigin(w, r) {
		return
	}
	if !s.authorizeMutation(r) {
		writeError(w, http.StatusForbidden, fmt.Errorf("principal is not authorized to modify this resource"))
		return
	}
	s.mux.ServeHTTP(w, r)
}

type auditStatusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *auditStatusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *auditStatusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

func (s *Server) emitRequestAudit(r *http.Request, status int) {
	if s.auditEmitter == nil {
		return
	}
	action := audit.ActionRead
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		action = audit.ActionWrite
	}
	decision := audit.DecisionAllow
	reason := ""
	if status >= http.StatusBadRequest {
		decision = audit.DecisionDeny
		reason = fmt.Sprintf("status_%d", status)
	}
	id, _ := coreauthz.IdentityFromContext(r.Context())
	detail := ""
	if requested := displayRequestedProfile(r); requested != "" {
		detail = "displayProfile=" + requested
	}
	ev := audit.Event{
		Service:       "guardian",
		Scope:         "ui",
		Action:        action,
		Decision:      decision,
		Reason:        reason,
		ActorSubject:  id.Subject,
		ActorClientID: id.ClientID,
		Email:         id.Email,
		Groups:        id.Groups,
		Resource:      r.URL.Path,
		ResourceType:  "http",
		Method:        r.Method,
		Path:          r.URL.Path,
		Status:        status,
		RequestID:     r.Header.Get("X-Request-ID"),
		SourceIP:      r.RemoteAddr,
		UserAgent:     r.UserAgent(),
		Detail:        detail,
	}
	_ = s.auditEmitter.EmitDurable(r.Context(), ev)
}

// authorizeMutation applies the repo-managed access policy to every mutating
// UI/API request. Partition-scoped mutations require the maintainer/admin role
// on that partition; set membership is scoped to the partitions being added or
// removed; creating a set requires the maintainer/admin role on at least one
// partition; other registry/scan/set-metadata mutations require admin. The
// check is fail-closed: while no enforcing access policy is loaded, mutations
// are denied unless the deployment explicitly opted into unpoliced mutations.
// Reads (GET/HEAD/OPTIONS) are never blocked by this guard.
func (s *Server) authorizeMutation(r *http.Request) bool {
	if s == nil {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if !s.access.Enforcing() {
		if !s.allowUnpolicedMutations {
			return false
		}
		// Explicitly opted in: preserve the historical permissive behaviour
		// for disabled/observe-only deployments.
		return true
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/api/m2m/") ||
		strings.HasPrefix(r.URL.Path, "/api/access/") {
		return true
	}
	// CI merge notifications use the internal M2M token rather than a human
	// maintainer session.
	if strings.HasPrefix(r.URL.Path, "/api/repos/") && strings.HasSuffix(r.URL.Path, "/notify") {
		return s.requestCanReadAccessPolicy(r)
	}
	// Creating an empty set is allowed for principals who can modify at least
	// one partition; membership is still checked partition-by-partition below.
	if r.URL.Path == "/api/sets" && r.Method == http.MethodPost {
		return s.access.CanModifyAny(r.Context())
	}
	if strings.HasPrefix(r.URL.Path, "/api/sets/") {
		return s.authorizeSetMutation(r)
	}
	if name, ok := partitionFromAPIPath(r.URL.Path); ok {
		return s.access.CanModify(r.Context(), name)
	}
	return s.access.IsAdmin(r.Context())
}

// maxSetMutationBytes caps the set-membership request body the authorization
// guard will inspect. It mirrors the handler's own limit.
const maxSetMutationBytes = 1 << 20

// authorizeSetMutation scopes virtual-set membership changes to the partitions
// they name. Sets are metadata only, but adding or removing a partition still
// affects the navigation entry for that partition, so the caller must hold the
// maintainer/admin role on every partition the request touches. Set creation is
// handled by authorizeMutation (maintainer/admin on at least one partition);
// rename/move/delete remains a platform-wide admin operation because it changes
// shared navigation for partitions the caller may not manage.
func (s *Server) authorizeSetMutation(r *http.Request) bool {
	if setAPIAction(r.URL.Path) != "partitions" || r.Method != http.MethodPost {
		return s.access.IsAdmin(r.Context())
	}
	if r.Body == nil {
		return s.access.IsAdmin(r.Context())
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxSetMutationBytes+1))
	if err != nil {
		return false
	}
	if len(body) > maxSetMutationBytes {
		return false
	}
	// The handler decodes the same body, so restore it before returning.
	r.Body = io.NopCloser(bytes.NewReader(body))
	var req setPartitionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		// A malformed body cannot mutate anything; let the handler emit 400.
		return true
	}
	for _, partition := range append(append([]string(nil), req.Add...), req.Remove...) {
		partition = strings.TrimSpace(partition)
		if partition == "" {
			continue
		}
		if !s.access.CanModify(r.Context(), partition) {
			return false
		}
	}
	return true
}

// setAPIAction returns the action segment of /api/sets/{id}/{action} (empty
// when the path has no action).
func setAPIAction(path string) string {
	const prefix = "/api/sets/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" {
		return ""
	}
	if idx := strings.Index(rest, "/"); idx >= 0 {
		return rest[idx+1:]
	}
	return ""
}

func partitionFromAPIPath(path string) (string, bool) {
	const prefix = "/api/partitions/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" {
		return "", false
	}
	name := strings.SplitN(rest, "/", 2)[0]
	if name == "" {
		return "", false
	}
	return name, true
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", s.static)
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/health", s.handleHealth)
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
	mux.HandleFunc("/api/display-profiles", s.handleDisplayProfiles)
	mux.HandleFunc("/api/display-profiles/", s.handleDisplayProfileRoute)
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
	monofsCfg := *s.clientConfig.MonoFS
	if !s.clientConfigIncludeToken {
		monofsCfg.Token = ""
	}
	writeJSON(w, http.StatusOK, ClientConfig{MonoFS: &monofsCfg})
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

type m2mTrustedContextKey struct{}

// withM2MTrusted marks a request authorized by the machine-to-machine token.
// Machine callers (Sentinel, pushers) need the complete partition list; applying
// the end-user access policy to them filters every partition out whenever the
// machine token has no user grants, which silently breaks alerting.
func withM2MTrusted(ctx context.Context) context.Context {
	return context.WithValue(ctx, m2mTrustedContextKey{}, true)
}

func isM2MTrusted(ctx context.Context) bool {
	trusted, _ := ctx.Value(m2mTrustedContextKey{}).(bool)
	return trusted
}

// canSeePartition applies the repo-managed access policy to one partition.
func (s *Server) canSeePartition(ctx context.Context, partition string) bool {
	if isM2MTrusted(ctx) {
		return true
	}
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
	if !s.requireM2MAccess(w, r) {
		return
	}
	payload, err := s.buildOverview(withM2MTrusted(r.Context()))
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
	if !s.requireM2MAccess(w, r) {
		return
	}
	cloned := r.Clone(withM2MTrusted(r.Context()))
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
	if !s.requireM2MAccess(w, r) {
		return
	}
	cloned := r.Clone(withM2MTrusted(r.Context()))
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
	if err := paths.ValidatePartitionName(name); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// Repo-managed access policy: hide GETs on partitions the caller cannot
	// see. Mutating requests are authorized centrally in ServeHTTP.
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
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&req); err != nil {
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
		if err := paths.ValidateIntentName(intentName); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
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

// restoreRedactedSecretValues replaces the UI redaction placeholder with the
// stored secret when the browser round-trips a manifest it only saw redacted.
// If no stored value exists, the save fails rather than persisting "******".
func restoreRedactedSecretValues(existing, incoming *intentdomain.Intent) error {
	if existing == nil || incoming == nil {
		return nil
	}
	existingByName := make(map[string]string, len(existing.Spec.Assets))
	for _, asset := range existing.Spec.Assets {
		if !strings.EqualFold(strings.TrimSpace(asset.Type), "Secret") {
			continue
		}
		value, _ := asset.Properties["value"].(string)
		existingByName[asset.Name] = value
	}
	for i := range incoming.Spec.Assets {
		asset := &incoming.Spec.Assets[i]
		if !strings.EqualFold(strings.TrimSpace(asset.Type), "Secret") {
			continue
		}
		value, _ := asset.Properties["value"].(string)
		if value != redactedSecretValue {
			continue
		}
		stored, ok := existingByName[asset.Name]
		if !ok || stored == "" {
			return fmt.Errorf("intent %s asset %s: cannot save the redacted secret placeholder", incoming.Metadata.Name, asset.Name)
		}
		if asset.Properties == nil {
			asset.Properties = map[string]any{}
		}
		asset.Properties["value"] = stored
	}
	return nil
}

func (s *Server) saveBundle(ctx context.Context, partitionName string, req *SaveBundleRequest) (*SaveBundleResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request body is required")
	}
	if err := paths.ValidatePartitionName(partitionName); err != nil {
		return nil, err
	}
	if managed, repo, err := s.partitionRepoManaged(ctx, partitionName); err != nil {
		return nil, err
	} else if managed && !allowRepoManagedWrites() {
		return nil, repoManagedWriteError(partitionName, repo)
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
		if err := paths.ValidateIntentName(req.Intents[i].Manifest.Metadata.Name); err != nil {
			return nil, err
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
	existingByName := make(map[string]*intentdomain.Intent, len(existingSources))
	for _, source := range existingSources {
		existingByName[source.Name] = source.Manifest
	}
	sort.Slice(req.Intents, func(i, j int) bool {
		return req.Intents[i].Manifest.Metadata.Name < req.Intents[j].Manifest.Metadata.Name
	})
	for _, intent := range req.Intents {
		if err := restoreRedactedSecretValues(existingByName[intent.Manifest.Metadata.Name], &intent.Manifest); err != nil {
			return nil, err
		}
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
	if r == nil || hasForwardingHeaders(r) {
		return false
	}
	return isLoopbackRemote(r.RemoteAddr)
}

func hasForwardingHeaders(r *http.Request) bool {
	if r == nil {
		return false
	}
	return len(r.Header.Values("X-Forwarded-For")) > 0 || len(r.Header.Values("Forwarded")) > 0
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
