package awsread

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Handler serves the AWS read API over HTTP.
type Handler struct {
	Reader Reader
	Token  string
}

// NewHandler builds an HTTP handler for the reader.
func NewHandler(reader Reader, token string) *Handler {
	return &Handler{Reader: reader, Token: strings.TrimSpace(token)}
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
	default:
		writeError(w, http.StatusNotFound, "not found")
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
