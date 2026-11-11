package gcpread

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/gcpobserve"
)

// Handler serves the GCP read API over HTTP.
type Handler struct {
	Reader     Reader
	Token      string
	TokenFile  string
	PromQL     PromQLProxier
	PusherName string
	Project    string
	Region     string
	Scopes     map[string]struct{}
	Observed   ObservedProvider
}

// ObservedProvider is the observed-state surface served by the read API.
type ObservedProvider interface {
	List(project, region string) []gcpobserve.StackState
	Get(project, region, stackName string) (gcpobserve.StackState, bool)
	Refresh(ctx context.Context, project, region string, stackNames []string) ([]gcpobserve.StackState, error)
	StackResources(ctx context.Context, project, region, stackName string) ([]gcpobserve.StackResource, error)
}

// NewHandler builds an HTTP handler for the reader.
func NewHandler(reader Reader, token string) *Handler {
	return &Handler{Reader: reader, Token: strings.TrimSpace(token)}
}

// WithPromQL installs the Managed Prometheus proxy.
func (h *Handler) WithPromQL(promql PromQLProxier) *Handler {
	h.PromQL = promql
	return h
}

// WithIdentity records the pusher/project/region served by this API.
func (h *Handler) WithIdentity(pusher, project, region string) *Handler {
	h.PusherName = strings.TrimSpace(pusher)
	h.Project = strings.TrimSpace(project)
	h.Region = strings.TrimSpace(region)
	return h
}

// WithObserved installs the observed-state provider.
func (h *Handler) WithObserved(provider ObservedProvider) *Handler {
	h.Observed = provider
	return h
}

// WithTokenFile re-reads the bearer token from disk on each request.
func (h *Handler) WithTokenFile(path string) *Handler {
	h.TokenFile = strings.TrimSpace(path)
	return h
}

// WithScopes restricts the token to read-only operations. The GCP read API
// currently exposes only read, so any non-empty scope set must contain "read".
func (h *Handler) WithScopes(scopes ...string) *Handler {
	if len(scopes) == 0 {
		scopes = []string{"read"}
	}
	h.Scopes = map[string]struct{}{}
	for _, scope := range scopes {
		if scope = strings.TrimSpace(strings.ToLower(scope)); scope != "" {
			h.Scopes[scope] = struct{}{}
		}
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if !h.tokenValid(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !h.scopeAllowed() {
		writeError(w, http.StatusForbidden, "insufficient token scope")
		return
	}

	switch {
	case path == "/healthz":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case path == "/v1/gcp/meta":
		writeJSON(w, http.StatusOK, map[string]string{
			"provider": "gcp",
			"pusher":   h.PusherName,
			"project":  h.Project,
			"region":   h.Region,
		})
	case path == "/v1/gcp/metrics":
		h.handleMetrics(w, r)
	case path == "/v1/gcp/metrics/query":
		h.handleQueryMetric(w, r)
	case path == "/v1/gcp/log-names":
		h.handleLogNames(w, r)
	case path == "/v1/gcp/logs/query":
		h.handleQueryLogs(w, r)
	case path == "/v1/gcp/services":
		h.handleServices(w, r)
	case path == "/v1/gcp/alerts":
		h.handleAlerts(w, r)
	case path == "/v1/gcp/traces":
		h.handleTraces(w, r)
	case strings.HasPrefix(path, "/v1/gcp/traces/"):
		h.handleGetTrace(w, r, strings.TrimPrefix(path, "/v1/gcp/traces/"))
	case strings.HasPrefix(path, "/v1/gcp/promql/"):
		h.handlePromQL(w, r, strings.TrimPrefix(path, "/v1/gcp/promql"))
	case path == "/v1/gcp/observed/stacks":
		h.handleObservedList(w, r)
	case strings.HasPrefix(path, "/v1/gcp/observed/stacks/"):
		h.handleObservedStack(w, r, strings.TrimPrefix(path, "/v1/gcp/observed/stacks/"))
	case path == "/v1/gcp/observed/refresh":
		h.handleObservedRefresh(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) tokenValid(r *http.Request) bool {
	token := h.Token
	if h.TokenFile != "" {
		content, err := os.ReadFile(h.TokenFile)
		if err != nil {
			// A configured token file that cannot be read must never fall back
			// to an empty token (which would disable authentication).
			return false
		}
		token = strings.TrimSpace(string(content))
		if token == "" {
			return false
		}
	}
	if token == "" {
		return true
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return false
	}
	provided := strings.TrimSpace(header[len("bearer "):])
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

func (h *Handler) scopeAllowed() bool {
	if len(h.Scopes) == 0 {
		return true
	}
	_, ok := h.Scopes["read"]
	return ok
}

func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	descriptors, err := h.Reader.ListMetrics(r.Context(), r.URL.Query().Get("namespace"), r.URL.Query().Get("metric"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": descriptors})
}

func (h *Handler) handleQueryMetric(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	from, err := parseTime(values.Get("from"), time.Now().Add(-1*time.Hour))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid from: "+err.Error())
		return
	}
	to, err := parseTime(values.Get("to"), time.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to: "+err.Error())
		return
	}
	dimensions := map[string]string{}
	for key, list := range values {
		if !strings.HasPrefix(key, "dim.") || len(list) == 0 {
			continue
		}
		dimensions[strings.TrimPrefix(key, "dim.")] = list[0]
	}
	series, err := h.Reader.QueryMetric(r.Context(), MetricQuery{
		Namespace:    values.Get("namespace"),
		MetricName:   values.Get("metric"),
		Dimensions:   dimensions,
		ResourceType: values.Get("resource_type"),
		Aligner:      values.Get("aligner"),
		From:         from,
		To:           to,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": series})
}

func (h *Handler) handleLogNames(w http.ResponseWriter, r *http.Request) {
	names, err := h.Reader.ListLogNames(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logNames": names})
}

func (h *Handler) handleQueryLogs(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	from, err := parseTime(values.Get("from"), time.Now().Add(-1*time.Hour))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid from: "+err.Error())
		return
	}
	to, err := parseTime(values.Get("to"), time.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to: "+err.Error())
		return
	}
	limit := 200
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	records, err := h.Reader.QueryLogs(r.Context(), LogQuery{
		LogName: values.Get("log"),
		Query:   values.Get("query"),
		From:    from,
		To:      to,
		Limit:   limit,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": records})
}

func (h *Handler) handleServices(w http.ResponseWriter, r *http.Request) {
	services, err := h.Reader.ListServices(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": services})
}

func (h *Handler) handleTraces(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	from, err := parseTime(values.Get("from"), time.Now().Add(-1*time.Hour))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid from: "+err.Error())
		return
	}
	to, err := parseTime(values.Get("to"), time.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to: "+err.Error())
		return
	}
	limit := 200
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	traces, err := h.Reader.QueryTraces(r.Context(), TraceQuery{
		Service: values.Get("service"),
		From:    from,
		To:      to,
		Limit:   limit,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"traces": traces})
}

func (h *Handler) handleGetTrace(w http.ResponseWriter, r *http.Request, traceID string) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		writeError(w, http.StatusBadRequest, "trace id is required")
		return
	}
	spans, err := h.Reader.GetTrace(r.Context(), traceID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"spans": spans})
}

func (h *Handler) handlePromQL(w http.ResponseWriter, r *http.Request, suffix string) {
	if h.PromQL == nil {
		writeError(w, http.StatusServiceUnavailable, "promql proxy is not configured")
		return
	}
	params := map[string][]string{}
	for key, values := range r.URL.Query() {
		params[key] = append([]string(nil), values...)
	}
	payload, status, err := h.PromQL.Do(r.Context(), suffix, params)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func parseTime(raw string, fallback time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if len(raw) >= 13 {
			return time.UnixMilli(seconds), nil
		}
		return time.Unix(seconds, 0), nil
	}
	return time.Time{}, errors.New("expected RFC3339 or Unix time")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (h *Handler) observedProject(r *http.Request) string {
	// One pusher is bound to one project. Do not let a bearer token query a
	// different project that the underlying service account happens to reach.
	if project := strings.TrimSpace(h.Project); project != "" {
		return project
	}
	return strings.TrimSpace(r.URL.Query().Get("project"))
}

func (h *Handler) handleObservedList(w http.ResponseWriter, r *http.Request) {
	if h.Observed == nil {
		writeError(w, http.StatusServiceUnavailable, "observed provider is not configured")
		return
	}
	stacks := h.Observed.List(h.observedProject(r), r.URL.Query().Get("region"))
	writeJSON(w, http.StatusOK, map[string]any{"stacks": stacks})
}

func (h *Handler) handleObservedStack(w http.ResponseWriter, r *http.Request, stackName string) {
	if h.Observed == nil {
		writeError(w, http.StatusServiceUnavailable, "observed provider is not configured")
		return
	}
	stackName = strings.Trim(stackName, "/")
	if strings.HasSuffix(stackName, "/resources") {
		name := strings.TrimSuffix(stackName, "/resources")
		if name == "" {
			writeError(w, http.StatusBadRequest, "stack name is required")
			return
		}
		resources, err := h.Observed.StackResources(r.Context(), h.observedProject(r), r.URL.Query().Get("region"), name)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"stackName": name, "resources": resources})
		return
	}
	stack, ok := h.Observed.Get(h.observedProject(r), r.URL.Query().Get("region"), stackName)
	if !ok {
		writeError(w, http.StatusNotFound, "stack not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stack": stack})
}

func (h *Handler) handleObservedRefresh(w http.ResponseWriter, r *http.Request) {
	if h.Observed == nil {
		writeError(w, http.StatusServiceUnavailable, "observed provider is not configured")
		return
	}
	stacks, err := h.Observed.Refresh(r.Context(), h.observedProject(r), r.URL.Query().Get("region"), r.URL.Query()["stack"])
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stacks": stacks})
}

func (h *Handler) handleAlerts(w http.ResponseWriter, r *http.Request) {
	reader, ok := h.Reader.(AlertReader)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "alert reader is not configured")
		return
	}
	alerts, err := reader.ListAlerts(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": alerts})
}
