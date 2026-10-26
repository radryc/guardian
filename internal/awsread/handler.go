package awsread

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/awsobserve"
	"github.com/rydzu/ainfra/guardian/internal/onboarding"
	"github.com/rydzu/ainfra/guardian/internal/pusher/probes"
)

// Handler serves the AWS read API over HTTP.
type Handler struct {
	Reader Reader
	Token  string
	// PromQL proxies PromQL to CloudWatch's Prometheus-compatible API.
	PromQL PromQLProxier
	// Observed serves observed CloudFormation stack state for Guardian.
	Observed awsobserve.Provider
	// Onboarding serves customer stack descriptors read from SSM.
	Onboarding onboarding.Reader
	// Probes serves the latest storeless probe results.
	Probes     ProbeProvider
	PusherName string
	Account    string
	Region     string
}

// ProbeProvider exposes the latest probe results without requiring a store.
type ProbeProvider interface {
	Latest() []probes.Result
}

// NewHandler builds an HTTP handler for the reader.
func NewHandler(reader Reader, token string) *Handler {
	return &Handler{Reader: reader, Token: strings.TrimSpace(token)}
}

// WithPromQL attaches a CloudWatch PromQL proxy.
func (h *Handler) WithPromQL(promql PromQLProxier) *Handler {
	h.PromQL = promql
	return h
}

// WithObserved attaches the observed CloudFormation state provider.
func (h *Handler) WithObserved(provider awsobserve.Provider) *Handler {
	h.Observed = provider
	return h
}

// WithOnboarding attaches the SSM-backed onboarding descriptor reader.
func (h *Handler) WithOnboarding(reader onboarding.Reader) *Handler {
	h.Onboarding = reader
	return h
}

// WithProbes attaches the latest probe result provider.
func (h *Handler) WithProbes(provider ProbeProvider) *Handler {
	h.Probes = provider
	return h
}

// WithIdentity attaches the pusher/account/region metadata surfaced by /v1/aws/meta.
func (h *Handler) WithIdentity(pusher, account, region string) *Handler {
	h.PusherName = strings.TrimSpace(pusher)
	h.Account = strings.TrimSpace(account)
	h.Region = strings.TrimSpace(region)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/healthz":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case path == "/v1/aws/meta":
		h.handleMeta(w, r)
	case path == "/v1/aws/metrics":
		h.handleListMetrics(w, r)
	case path == "/v1/aws/metrics/query":
		h.handleQueryMetric(w, r)
	case path == "/v1/aws/log-groups":
		h.handleListLogGroups(w, r)
	case path == "/v1/aws/logs/query":
		h.handleQueryLogs(w, r)
	case path == "/v1/aws/xray/services":
		h.handleListServices(w, r)
	case path == "/v1/aws/xray/traces":
		h.handleQueryTraces(w, r)
	case strings.HasPrefix(path, "/v1/aws/xray/trace/"):
		h.handleGetTrace(w, r, strings.TrimPrefix(path, "/v1/aws/xray/trace/"))
	case strings.HasPrefix(path, "/v1/aws/promql/"):
		h.handlePromQL(w, r, strings.TrimPrefix(path, "/v1/aws/promql"))
	case path == "/v1/aws/onboarding":
		h.handleOnboarding(w, r)
	case path == "/v1/aws/probes":
		h.handleProbes(w, r)
	case path == "/v1/aws/observed/stacks":
		h.handleObservedList(w, r)
	case strings.HasPrefix(path, "/v1/aws/observed/stacks/"):
		h.handleObservedStack(w, r, strings.TrimPrefix(path, "/v1/aws/observed/stacks/"))
	case path == "/v1/aws/observed/events":
		h.handleObservedEvents(w, r)
	case path == "/v1/aws/observed/refresh":
		h.handleObservedRefresh(w, r)
	case path == "/v1/aws/observed/diff":
		h.handleObservedDiff(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// handlePromQL proxies PromQL operations to CloudWatch's Prometheus-compatible
// API. The optional "region" parameter selects the CloudWatch region.
func (h *Handler) handlePromQL(w http.ResponseWriter, r *http.Request, suffix string) {
	if h.PromQL == nil {
		writeError(w, http.StatusServiceUnavailable, "cloudwatch promql proxy not configured")
		return
	}
	cwPath, err := promqlOperationPath(suffix)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request form")
		return
	}
	params := url.Values{}
	region := ""
	for key, values := range r.Form {
		if key == "region" {
			if len(values) > 0 {
				region = strings.TrimSpace(values[0])
			}
			continue
		}
		for _, value := range values {
			params.Add(key, value)
		}
	}
	payload, status, err := h.PromQL.Do(r.Context(), region, cwPath, params)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// Some CloudWatch regions do not implement the Prometheus label-values
	// operation, so fall back to ListMetrics only when the native response is
	// empty. OTLP-ingested metrics are visible through native PromQL label
	// discovery but may not appear in ListMetrics.
	if strings.HasPrefix(cwPath, "/api/v1/label/") && strings.HasSuffix(cwPath, "/values") {
		name := strings.TrimSuffix(strings.TrimPrefix(cwPath, "/api/v1/label/"), "/values")
		if (status != 0 && status != http.StatusOK) || promqlLabelValuesEmpty(payload) {
			if fallback := h.promqlLabelValuesFallback(r.Context(), name); fallback != nil {
				payload = fallback
				status = http.StatusOK
			}
		}
	}
	// Some accounts return a successful but empty CloudWatch PromQL result for
	// metrics that ListMetrics can see. Use GetMetricData for the simple
	// selectors used by the AWS dashboards.
	if promqlEmpty(payload) {
		if fallback := h.promqlFallback(r.Context(), cwPath, params); fallback != nil {
			payload = fallback
			status = http.StatusOK
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func promqlOperationPath(suffix string) (string, error) {
	switch {
	case suffix == "/query":
		return "/api/v1/query", nil
	case suffix == "/query_range":
		return "/api/v1/query_range", nil
	case suffix == "/series":
		return "/api/v1/series", nil
	case suffix == "/labels":
		return "/api/v1/labels", nil
	case strings.HasPrefix(suffix, "/label/") && strings.HasSuffix(suffix, "/values"):
		name := strings.TrimSuffix(strings.TrimPrefix(suffix, "/label/"), "/values")
		if name == "" {
			return "", fmt.Errorf("label name is required")
		}
		return "/api/v1/label/" + name + "/values", nil
	default:
		return "", fmt.Errorf("unknown promql operation %q", suffix)
	}
}

func (h *Handler) authorized(r *http.Request) bool {
	if h.Token == "" {
		return true
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")) == h.Token
	}
	return false
}

func (h *Handler) handleMeta(w http.ResponseWriter, r *http.Request) {
	metrics, _ := h.Reader.ListMetrics(r.Context(), "", "")
	namespaceSet := map[string]struct{}{}
	metricSet := map[string]struct{}{}
	for _, metric := range metrics {
		if metric.Namespace != "" {
			namespaceSet[metric.Namespace] = struct{}{}
		}
		if metric.MetricName != "" {
			metricSet[metric.MetricName] = struct{}{}
		}
	}
	logGroups, _ := h.Reader.ListLogGroups(r.Context())
	services, _ := h.Reader.ListServices(r.Context())
	serviceNames := make([]string, 0, len(services))
	for _, service := range services {
		if service.Name != "" {
			serviceNames = append(serviceNames, service.Name)
		}
	}
	observedStacks := 0
	if h.Observed != nil {
		observedStacks = len(h.Observed.List(h.Account, h.Region))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"pusher":           h.PusherName,
		"account":          h.Account,
		"region":           h.Region,
		"metricNamespaces": mapKeys(namespaceSet),
		"metricNames":      mapKeys(metricSet),
		"logGroups":        logGroups,
		"xrayServices":     serviceNames,
		"observedStacks":   observedStacks,
	})
}

func mapKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (h *Handler) handleListMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := h.Reader.ListMetrics(r.Context(), queryParam(r, "namespace"), queryParam(r, "metric"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (h *Handler) handleQueryMetric(w http.ResponseWriter, r *http.Request) {
	period, err := parseDuration(queryParam(r, "period"), 5*time.Minute)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, err := parseTime(queryParam(r, "from"), time.Now().UTC().Add(-time.Hour))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	to, err := parseTime(queryParam(r, "to"), time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dimensions := map[string]string{}
	for key, values := range r.URL.Query() {
		if !strings.HasPrefix(key, "dim.") || len(values) == 0 {
			continue
		}
		dimensions[strings.TrimPrefix(key, "dim.")] = values[0]
	}
	series, err := h.Reader.QueryMetric(r.Context(), MetricQuery{
		Namespace:  queryParam(r, "namespace"),
		MetricName: queryParam(r, "metric"),
		Dimensions: dimensions,
		Stat:       queryParam(r, "stat"),
		Period:     period,
		From:       from,
		To:         to,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, series)
}

func (h *Handler) handleListLogGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.Reader.ListLogGroups(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func (h *Handler) handleQueryLogs(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LogGroups []string `json:"log_groups"`
		Query     string   `json:"query"`
		From      string   `json:"from"`
		To        string   `json:"to"`
		Limit     int      `json:"limit"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	from, err := parseTime(body.From, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	to, err := parseTime(body.To, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	records, err := h.Reader.QueryLogs(r.Context(), LogQuery{
		LogGroups: body.LogGroups,
		Query:     body.Query,
		From:      from,
		To:        to,
		Limit:     body.Limit,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (h *Handler) handleListServices(w http.ResponseWriter, r *http.Request) {
	services, err := h.Reader.ListServices(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, services)
}

func (h *Handler) handleQueryTraces(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(queryParam(r, "limit"))
	from, err := parseTime(queryParam(r, "from"), time.Now().UTC().Add(-time.Hour))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	to, err := parseTime(queryParam(r, "to"), time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	traces, err := h.Reader.QueryTraces(r.Context(), TraceQuery{
		Service: queryParam(r, "service"),
		From:    from,
		To:      to,
		Limit:   limit,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, traces)
}

func (h *Handler) handleGetTrace(w http.ResponseWriter, r *http.Request, traceID string) {
	spans, err := h.Reader.GetTrace(r.Context(), traceID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, spans)
}

func (h *Handler) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.Onboarding == nil {
		writeJSON(w, http.StatusOK, map[string]any{"descriptors": []onboarding.Descriptor{}})
		return
	}
	descriptors, err := h.Onboarding.List(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if descriptors == nil {
		descriptors = []onboarding.Descriptor{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"descriptors": descriptors})
}

func (h *Handler) handleProbes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.Probes == nil {
		writeJSON(w, http.StatusOK, map[string]any{"probes": []probes.Result{}})
		return
	}
	partition := queryParam(r, "partition")
	intent := queryParam(r, "intent")
	results := h.Probes.Latest()
	filtered := make([]probes.Result, 0, len(results))
	for _, result := range results {
		if partition != "" && result.Partition != partition {
			continue
		}
		if intent != "" && result.Intent != intent {
			continue
		}
		filtered = append(filtered, result)
	}
	writeJSON(w, http.StatusOK, map[string]any{"probes": filtered})
}

func (h *Handler) observedReady(w http.ResponseWriter) bool {
	if h.Observed == nil {
		writeError(w, http.StatusServiceUnavailable, "observed state API not configured")
		return false
	}
	return true
}

func (h *Handler) handleObservedList(w http.ResponseWriter, r *http.Request) {
	if !h.observedReady(w) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	stacks := h.Observed.List(queryParam(r, "account"), queryParam(r, "region"))
	writeJSON(w, http.StatusOK, map[string]any{"stacks": stacks})
}

func (h *Handler) handleObservedStack(w http.ResponseWriter, r *http.Request, suffix string) {
	if !h.observedReady(w) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	account := queryParam(r, "account")
	region := queryParam(r, "region")
	suffix = strings.Trim(suffix, "/")
	if suffix == "" {
		writeError(w, http.StatusBadRequest, "stack name is required")
		return
	}
	if strings.HasSuffix(suffix, "/template") {
		stackName := strings.TrimSuffix(suffix, "/template")
		if stackName == "" {
			writeError(w, http.StatusBadRequest, "stack name is required")
			return
		}
		template, err := h.Observed.Template(r.Context(), account, region, stackName)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"stackName": stackName, "template": template})
		return
	}
	if strings.HasSuffix(suffix, "/resources") {
		stackName := strings.TrimSuffix(suffix, "/resources")
		if stackName == "" {
			writeError(w, http.StatusBadRequest, "stack name is required")
			return
		}
		resources, err := h.Observed.StackResources(r.Context(), account, region, stackName)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"stackName": stackName, "resources": resources})
		return
	}
	state, ok := h.Observed.Get(account, region, suffix)
	if !ok {
		writeError(w, http.StatusNotFound, "stack not observed")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *Handler) handleObservedEvents(w http.ResponseWriter, r *http.Request) {
	if !h.observedReady(w) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	since, err := parseTime(queryParam(r, "since"), time.Time{})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, _ := strconv.Atoi(queryParam(r, "limit"))
	events := h.Observed.Events(queryParam(r, "account"), queryParam(r, "region"), queryParam(r, "stack"), since, limit)
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *Handler) handleObservedRefresh(w http.ResponseWriter, r *http.Request) {
	if !h.observedReady(w) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Account     string   `json:"account"`
		Region      string   `json:"region"`
		Stacks      []string `json:"stacks"`
		DetectDrift bool     `json:"detectDrift"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(body.Region) == "" {
		body.Region = queryParam(r, "region")
	}
	if strings.TrimSpace(body.Account) == "" {
		body.Account = queryParam(r, "account")
	}
	states, err := h.Observed.Refresh(r.Context(), body.Account, body.Region, body.Stacks, body.DetectDrift)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stacks": states})
}

func (h *Handler) handleObservedDiff(w http.ResponseWriter, r *http.Request) {
	if !h.observedReady(w) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req awsobserve.SourceDiffRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Region) == "" {
		req.Region = queryParam(r, "region")
	}
	if strings.TrimSpace(req.Account) == "" {
		req.Account = queryParam(r, "account")
	}
	result, err := h.Observed.DiffSource(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func queryParam(r *http.Request, name string) string {
	return strings.TrimSpace(r.URL.Query().Get(name))
}

func parseTime(value string, fallback time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		return time.UnixMilli(int64(seconds * 1000)).UTC(), nil
	}
	return time.Time{}, &parseError{value}
}

func parseDuration(value string, fallback time.Duration) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	if duration, err := time.ParseDuration(value); err == nil && duration > 0 {
		return duration, nil
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second, nil
	}
	return 0, &parseError{value}
}

type parseError struct{ value string }

func (e *parseError) Error() string { return "invalid value " + strconv.Quote(e.value) }

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
