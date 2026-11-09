package gcpread

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	defaultMonitoringURL = "https://monitoring.googleapis.com"
	defaultLoggingURL    = "https://logging.googleapis.com"
	defaultTraceURL      = "https://cloudtrace.googleapis.com"

	defaultPageSize = 1000
)

// Config configures the GCP reader.
type Config struct {
	Project           string
	DefaultRegion     string
	TokenSource       oauth2.TokenSource
	HTTPClient        *http.Client
	MonitoringBaseURL string
	LoggingBaseURL    string
	TraceBaseURL      string
}

// Client reads Cloud Monitoring, Logging, and Trace over REST.
type Client struct {
	project    string
	region     string
	tokens     oauth2.TokenSource
	http       *http.Client
	monitoring string
	logging    string
	trace      string
}

// New builds a reader. Without an explicit TokenSource it uses Application
// Default Credentials (WIF or GKE Workload Identity in production).
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Project) == "" {
		return nil, fmt.Errorf("gcpread: project is required")
	}
	httpClient := cfg.HTTPClient
	tokens := cfg.TokenSource
	if httpClient == nil {
		if tokens == nil {
			var err error
			tokens, err = google.DefaultTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
			if err != nil {
				return nil, fmt.Errorf("gcpread: application default credentials: %w", err)
			}
		}
		httpClient = oauth2.NewClient(context.Background(), tokens)
	}
	return &Client{
		project:    strings.TrimSpace(cfg.Project),
		region:     strings.TrimSpace(cfg.DefaultRegion),
		tokens:     tokens,
		http:       httpClient,
		monitoring: firstNonEmpty(cfg.MonitoringBaseURL, defaultMonitoringURL),
		logging:    firstNonEmpty(cfg.LoggingBaseURL, defaultLoggingURL),
		trace:      firstNonEmpty(cfg.TraceBaseURL, defaultTraceURL),
	}, nil
}

// Project returns the project served by this reader.
func (c *Client) Project() string { return c.project }

// DefaultRegion returns the configured default region.
func (c *Client) DefaultRegion() string { return c.region }

func (c *Client) do(ctx context.Context, method, endpoint string, params url.Values, body []byte, out any) error {
	requestURL := endpoint
	if len(params) > 0 {
		requestURL += "?" + params.Encode()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.tokens != nil {
		token, err := c.tokens.Token()
		if err != nil {
			return fmt.Errorf("gcpread: acquire token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gcpread: %s %s: HTTP %d: %s", method, requestURL, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("gcpread: decode %s: %w", requestURL, err)
	}
	return nil
}

// --------------------------------------------------------------------------
// Metrics
// --------------------------------------------------------------------------

func (c *Client) ListMetrics(ctx context.Context, namespace, metric string) ([]MetricDescriptor, error) {
	filter := metricFilter(namespace, metric)
	endpoint := fmt.Sprintf("%s/v3/projects/%s/metricDescriptors", c.monitoring, url.PathEscape(c.project))
	type descriptor struct {
		Type   string `json:"type"`
		Unit   string `json:"unit"`
		Labels []struct {
			Key string `json:"key"`
		} `json:"labels"`
	}
	out := make([]MetricDescriptor, 0)
	pageToken := ""
	for page := 0; page < 20; page++ {
		params := url.Values{}
		params.Set("pageSize", strconv.Itoa(defaultPageSize))
		if filter != "" {
			params.Set("filter", filter)
		}
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		var response struct {
			Descriptors   []descriptor `json:"metricDescriptors"`
			NextPageToken string       `json:"nextPageToken"`
		}
		if err := c.do(ctx, http.MethodGet, endpoint, params, nil, &response); err != nil {
			return nil, err
		}
		for _, item := range response.Descriptors {
			ns, name := splitMetricType(item.Type)
			dimensions := make([]string, 0, len(item.Labels))
			for _, label := range item.Labels {
				dimensions = append(dimensions, label.Key)
			}
			out = append(out, MetricDescriptor{
				Namespace:  ns,
				MetricName: name,
				Dimensions: dimensions,
				Region:     c.region,
				Project:    c.project,
			})
		}
		if strings.TrimSpace(response.NextPageToken) == "" {
			break
		}
		pageToken = response.NextPageToken
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].MetricName < out[j].MetricName
	})
	return out, nil
}

func metricFilter(namespace, metric string) string {
	namespace = strings.TrimSpace(namespace)
	metric = strings.TrimSpace(metric)
	switch {
	case namespace != "" && metric != "":
		return fmt.Sprintf(`metric.type = "%s/%s"`, namespace, metric)
	case namespace != "":
		return fmt.Sprintf(`metric.type = starts_with("%s/")`, namespace)
	case metric != "":
		return fmt.Sprintf(`metric.type = ends_with("/%s")`, metric)
	default:
		return ""
	}
}

func splitMetricType(metricType string) (string, string) {
	if idx := strings.Index(metricType, "/"); idx > 0 {
		return metricType[:idx], metricType[idx+1:]
	}
	return "", metricType
}

func (c *Client) QueryMetric(ctx context.Context, q MetricQuery) ([]MetricSeries, error) {
	filters := []string{fmt.Sprintf(`metric.type = "%s/%s"`, strings.TrimSpace(q.Namespace), strings.TrimSpace(q.MetricName))}
	if resourceType := strings.TrimSpace(q.ResourceType); resourceType != "" {
		filters = append(filters, fmt.Sprintf(`resource.type = "%s"`, resourceType))
	}
	keys := make([]string, 0, len(q.Dimensions))
	for key := range q.Dimensions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		filters = append(filters, fmt.Sprintf(`metric.labels.%s = "%s"`, key, q.Dimensions[key]))
	}

	params := url.Values{}
	params.Set("filter", strings.Join(filters, " AND "))
	params.Set("interval.startTime", q.From.UTC().Format(time.RFC3339))
	params.Set("interval.endTime", q.To.UTC().Format(time.RFC3339))
	aligner := strings.TrimSpace(q.Aligner)
	if aligner == "" {
		aligner = "ALIGN_MEAN"
	}
	params.Set("aggregation.perSeriesAligner", aligner)
	params.Set("aggregation.crossSeriesReducer", "REDUCE_SUM")
	params.Add("aggregation.groupByFields", "resource.type")
	params.Add("aggregation.groupByFields", "resource.labels.location")

	var response struct {
		TimeSeries []struct {
			Metric struct {
				Type   string            `json:"type"`
				Labels map[string]string `json:"labels"`
			} `json:"metric"`
			Resource struct {
				Type   string            `json:"type"`
				Labels map[string]string `json:"labels"`
			} `json:"resource"`
			Points []struct {
				Interval struct {
					EndTime time.Time `json:"endTime"`
				} `json:"interval"`
				Value struct {
					DoubleValue *float64 `json:"doubleValue"`
					Int64Value  *string  `json:"int64Value"`
				} `json:"value"`
			} `json:"points"`
		} `json:"timeSeries"`
	}
	endpoint := fmt.Sprintf("%s/v3/projects/%s/timeSeries", c.monitoring, url.PathEscape(c.project))
	if err := c.do(ctx, http.MethodGet, endpoint, params, nil, &response); err != nil {
		return nil, err
	}

	out := make([]MetricSeries, 0, len(response.TimeSeries))
	for _, series := range response.TimeSeries {
		namespace, name := splitMetricType(series.Metric.Type)
		dimensions := map[string]string{}
		for key, value := range series.Metric.Labels {
			dimensions[key] = value
		}
		for key, value := range series.Resource.Labels {
			dimensions["resource."+key] = value
		}
		points := make([]MetricPoint, 0, len(series.Points))
		for _, point := range series.Points {
			value := 0.0
			switch {
			case point.Value.DoubleValue != nil:
				value = *point.Value.DoubleValue
			case point.Value.Int64Value != nil:
				if parsed, err := strconv.ParseFloat(*point.Value.Int64Value, 64); err == nil {
					value = parsed
				}
			}
			points = append(points, MetricPoint{Timestamp: point.Interval.EndTime.UTC(), Value: value})
		}
		sort.Slice(points, func(i, j int) bool { return points[i].Timestamp.Before(points[j].Timestamp) })
		out = append(out, MetricSeries{
			Namespace:  namespace,
			MetricName: name,
			Dimensions: dimensions,
			Region:     firstNonEmpty(series.Resource.Labels["location"], series.Resource.Labels["region"], c.region),
			Project:    c.project,
			Points:     points,
		})
	}
	return out, nil
}

// --------------------------------------------------------------------------
// Logging
// --------------------------------------------------------------------------

func (c *Client) ListLogNames(ctx context.Context) ([]string, error) {
	endpoint := fmt.Sprintf("%s/v2/projects/%s/logs", c.logging, url.PathEscape(c.project))
	out := make([]string, 0)
	pageToken := ""
	for page := 0; page < 20; page++ {
		params := url.Values{}
		params.Set("pageSize", strconv.Itoa(defaultPageSize))
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		var response struct {
			LogNames      []string `json:"logNames"`
			NextPageToken string   `json:"nextPageToken"`
		}
		if err := c.do(ctx, http.MethodGet, endpoint, params, nil, &response); err != nil {
			return nil, err
		}
		for _, name := range response.LogNames {
			out = append(out, shortLogName(name))
		}
		if strings.TrimSpace(response.NextPageToken) == "" {
			break
		}
		pageToken = response.NextPageToken
	}
	sort.Strings(out)
	return out, nil
}

func (c *Client) QueryLogs(ctx context.Context, q LogQuery) ([]LogRecord, error) {
	limit := q.Limit
	if limit <= 0 || limit > defaultPageSize {
		limit = defaultPageSize
	}
	filterParts := make([]string, 0, 4)
	if !q.From.IsZero() {
		filterParts = append(filterParts, fmt.Sprintf(`timestamp >= "%s"`, q.From.UTC().Format(time.RFC3339)))
	}
	if !q.To.IsZero() {
		filterParts = append(filterParts, fmt.Sprintf(`timestamp <= "%s"`, q.To.UTC().Format(time.RFC3339)))
	}
	if name := strings.TrimSpace(q.LogName); name != "" {
		logName := name
		if !strings.HasPrefix(logName, "projects/") {
			logName = fmt.Sprintf("projects/%s/logs/%s", c.project, strings.TrimPrefix(logName, "logs/"))
		}
		filterParts = append(filterParts, fmt.Sprintf(`logName = "%s"`, logName))
	}
	if raw := strings.TrimSpace(q.Query); raw != "" {
		filterParts = append(filterParts, raw)
	}

	requestBody, err := json.Marshal(map[string]any{
		"resourceNames": []string{fmt.Sprintf("projects/%s", c.project)},
		"filter":        strings.Join(filterParts, " AND "),
		"orderBy":       "timestamp desc",
		"pageSize":      limit,
	})
	if err != nil {
		return nil, err
	}

	var response struct {
		Entries []struct {
			LogName   string            `json:"logName"`
			Timestamp time.Time         `json:"timestamp"`
			Severity  string            `json:"severity"`
			Text      string            `json:"textPayload"`
			JSON      map[string]any    `json:"jsonPayload"`
			Labels    map[string]string `json:"labels"`
			Resource  struct {
				Type   string            `json:"type"`
				Labels map[string]string `json:"labels"`
			} `json:"resource"`
		} `json:"entries"`
	}
	endpoint := fmt.Sprintf("%s/v2/entries:list", c.logging)
	if err := c.do(ctx, http.MethodPost, endpoint, nil, requestBody, &response); err != nil {
		return nil, err
	}

	out := make([]LogRecord, 0, len(response.Entries))
	for _, entry := range response.Entries {
		fields := map[string]string{}
		for key, value := range entry.JSON {
			fields[key] = stringify(value)
		}
		for key, value := range entry.Labels {
			fields[key] = value
		}
		message := entry.Text
		if message == "" {
			if raw, ok := entry.JSON["message"]; ok {
				message = stringify(raw)
			} else if len(entry.JSON) > 0 {
				if encoded, err := json.Marshal(entry.JSON); err == nil {
					message = string(encoded)
				}
			}
		}
		out = append(out, LogRecord{
			Timestamp: entry.Timestamp.UTC(),
			LogGroup:  shortLogName(entry.LogName),
			LogStream: firstNonEmpty(entry.Resource.Labels["revision_name"], entry.Resource.Labels["pod_name"], entry.Resource.Labels["instance_id"]),
			Message:   message,
			Fields:    fields,
			Region:    firstNonEmpty(entry.Resource.Labels["location"], entry.Resource.Labels["region"], c.region),
			Project:   c.project,
			Severity:  entry.Severity,
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func shortLogName(logName string) string {
	if idx := strings.LastIndex(logName, "/logs/"); idx >= 0 {
		return logName[idx+len("/logs/"):]
	}
	return logName
}

// --------------------------------------------------------------------------
// Trace
// --------------------------------------------------------------------------

type cloudTrace struct {
	TraceID string      `json:"traceId"`
	Spans   []cloudSpan `json:"spans"`
}

type cloudSpan struct {
	SpanID       string         `json:"spanId"`
	ParentSpanID string         `json:"parentSpanId"`
	Name         string         `json:"name"`
	StartTime    time.Time      `json:"startTime"`
	EndTime      time.Time      `json:"endTime"`
	Labels       map[string]any `json:"labels"`
	Attributes   map[string]any `json:"attributes"`
}

func (c *Client) QueryTraces(ctx context.Context, q TraceQuery) ([]TraceSummary, error) {
	limit := q.Limit
	if limit <= 0 || limit > defaultPageSize {
		limit = defaultPageSize
	}
	from := q.From
	if from.IsZero() {
		from = time.Now().Add(-1 * time.Hour)
	}
	to := q.To
	if to.IsZero() {
		to = time.Now()
	}
	params := url.Values{}
	params.Set("startTime", from.UTC().Format(time.RFC3339))
	params.Set("endTime", to.UTC().Format(time.RFC3339))
	params.Set("pageSize", strconv.Itoa(limit))
	params.Set("view", "COMPLETE")

	var response struct {
		Traces []cloudTrace `json:"traces"`
	}
	endpoint := fmt.Sprintf("%s/v1/projects/%s/traces", c.trace, url.PathEscape(c.project))
	if err := c.do(ctx, http.MethodGet, endpoint, params, nil, &response); err != nil {
		return nil, err
	}

	out := make([]TraceSummary, 0, len(response.Traces))
	for _, trace := range response.Traces {
		root := rootSpan(trace.Spans)
		if root == nil {
			continue
		}
		service := firstNonEmpty(root.Name, spanAttribute(*root, "service.name"), c.project)
		if wanted := strings.TrimSpace(q.Service); wanted != "" && !strings.EqualFold(service, wanted) {
			continue
		}
		out = append(out, TraceSummary{
			TraceID:   trace.TraceID,
			Service:   service,
			StartTime: root.StartTime.UTC(),
			EndTime:   root.EndTime.UTC(),
			Duration:  root.EndTime.Sub(root.StartTime),
			Status:    spanAttribute(*root, "status.code"),
			SpanCount: len(trace.Spans),
			Region:    c.region,
			Project:   c.project,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartTime.After(out[j].StartTime) })
	return out, nil
}

func (c *Client) GetTrace(ctx context.Context, traceID string) ([]Span, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil, fmt.Errorf("gcpread: trace id is required")
	}
	var response struct {
		Spans []cloudSpan `json:"spans"`
	}
	endpoint := fmt.Sprintf("%s/v1/projects/%s/traces/%s", c.trace, url.PathEscape(c.project), url.PathEscape(traceID))
	if err := c.do(ctx, http.MethodGet, endpoint, nil, nil, &response); err != nil {
		return nil, err
	}
	out := make([]Span, 0, len(response.Spans))
	for _, span := range response.Spans {
		service := firstNonEmpty(spanAttribute(span, "service.name"), span.Name)
		out = append(out, Span{
			TraceID:      traceID,
			SpanID:       span.SpanID,
			ParentSpanID: span.ParentSpanID,
			Name:         span.Name,
			Service:      service,
			StartTime:    span.StartTime.UTC(),
			EndTime:      span.EndTime.UTC(),
			StatusCode:   spanAttribute(span, "status.code"),
			Attributes:   spanAttributes(span),
			Region:       c.region,
			Project:      c.project,
		})
	}
	return out, nil
}

func (c *Client) ListServices(ctx context.Context) ([]ServiceInfo, error) {
	// Cloud Trace has no service-discovery API. Derive service names from the
	// root spans of the last hour.
	summaries, err := c.QueryTraces(ctx, TraceQuery{From: time.Now().Add(-1 * time.Hour), To: time.Now(), Limit: defaultPageSize})
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]ServiceInfo, 0)
	for _, summary := range summaries {
		if summary.Service == "" {
			continue
		}
		if _, ok := seen[summary.Service]; ok {
			continue
		}
		seen[summary.Service] = struct{}{}
		out = append(out, ServiceInfo{Name: summary.Service, Region: c.region, Project: c.project})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func rootSpan(spans []cloudSpan) *cloudSpan {
	var earliest *cloudSpan
	for i := range spans {
		if spans[i].ParentSpanID != "" {
			continue
		}
		if earliest == nil || spans[i].StartTime.Before(earliest.StartTime) {
			earliest = &spans[i]
		}
	}
	if earliest != nil {
		return earliest
	}
	for i := range spans {
		if earliest == nil || spans[i].StartTime.Before(earliest.StartTime) {
			earliest = &spans[i]
		}
	}
	return earliest
}

func spanAttributes(span cloudSpan) map[string]string {
	out := map[string]string{}
	for key, value := range span.Attributes {
		out[key] = stringify(value)
	}
	for key, value := range span.Labels {
		out[key] = stringify(value)
	}
	return out
}

func spanAttribute(span cloudSpan, key string) string {
	if value, ok := span.Attributes[key]; ok {
		return stringify(value)
	}
	if value, ok := span.Labels[key]; ok {
		return stringify(value)
	}
	return ""
}

// --------------------------------------------------------------------------
// PromQL
// --------------------------------------------------------------------------

// PromQLClient proxies PromQL operations to Cloud Monitoring.
type PromQLClient struct {
	client *Client
}

// PromQL returns a Managed Prometheus client for this project.
func (c *Client) PromQL() *PromQLClient { return &PromQLClient{client: c} }

// Do forwards a PromQL API suffix (for example /api/v1/query) with params.
func (p *PromQLClient) Do(ctx context.Context, suffix string, params map[string][]string) ([]byte, int, error) {
	c := p.client
	endpoint := fmt.Sprintf("%s/v1/projects/%s/location/global/prometheus%s", c.monitoring, url.PathEscape(c.project), suffix)
	values := url.Values{}
	for key, list := range params {
		for _, value := range list {
			values.Add(key, value)
		}
	}
	if len(values) > 0 {
		endpoint += "?" + values.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	if c.tokens != nil {
		token, err := c.tokens.Token()
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, 0, err
	}
	return payload, resp.StatusCode, nil
}

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringify(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(encoded)
	}
}

// --------------------------------------------------------------------------
// Alerts
// --------------------------------------------------------------------------

type cloudAlertPolicy struct {
	Name          string `json:"name"`
	DisplayName   string `json:"displayName"`
	Enabled       bool   `json:"enabled"`
	Severity      string `json:"severity"`
	Documentation struct {
		Content string `json:"content"`
	} `json:"documentation"`
	UserLabels map[string]string `json:"userLabels"`
	Conditions []struct {
		DisplayName string `json:"displayName"`
	} `json:"conditions"`
}

type cloudIncident struct {
	Name        string    `json:"name"`
	State       string    `json:"state"`
	StartedAt   time.Time `json:"startedAt"`
	EndedAt     time.Time `json:"endedAt"`
	SummaryText string    `json:"summaryText"`
	Severity    string    `json:"severity"`
	Policy      struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"policy"`
	Resource struct {
		Type   string            `json:"type"`
		Labels map[string]string `json:"labels"`
	} `json:"resource"`
}

// ListAlerts returns alert policies with their open incident state.
func (c *Client) ListAlerts(ctx context.Context) ([]Alert, error) {
	policyEndpoint := fmt.Sprintf("%s/v3/projects/%s/alertPolicies", c.monitoring, url.PathEscape(c.project))
	alertPolicies := make([]cloudAlertPolicy, 0)
	pageToken := ""
	for page := 0; page < 20; page++ {
		policyParams := url.Values{}
		policyParams.Set("pageSize", strconv.Itoa(defaultPageSize))
		if pageToken != "" {
			policyParams.Set("pageToken", pageToken)
		}
		var policies struct {
			AlertPolicies []cloudAlertPolicy `json:"alertPolicies"`
			NextPageToken string             `json:"nextPageToken"`
		}
		if err := c.do(ctx, http.MethodGet, policyEndpoint, policyParams, nil, &policies); err != nil {
			return nil, err
		}
		alertPolicies = append(alertPolicies, policies.AlertPolicies...)
		if strings.TrimSpace(policies.NextPageToken) == "" {
			break
		}
		pageToken = policies.NextPageToken
	}

	incidentEndpoint := fmt.Sprintf("%s/v3/projects/%s/alerts", c.monitoring, url.PathEscape(c.project))
	incidents := make([]cloudIncident, 0)
	pageToken = ""
	for page := 0; page < 20; page++ {
		incidentParams := url.Values{}
		incidentParams.Set("pageSize", strconv.Itoa(defaultPageSize))
		if pageToken != "" {
			incidentParams.Set("pageToken", pageToken)
		}
		var response struct {
			Alerts        []cloudIncident `json:"alerts"`
			NextPageToken string          `json:"nextPageToken"`
		}
		if err := c.do(ctx, http.MethodGet, incidentEndpoint, incidentParams, nil, &response); err != nil {
			return nil, err
		}
		incidents = append(incidents, response.Alerts...)
		if strings.TrimSpace(response.NextPageToken) == "" {
			break
		}
		pageToken = response.NextPageToken
	}

	openByPolicy := map[string][]cloudIncident{}
	for _, incident := range incidents {
		if !strings.EqualFold(incident.State, "OPEN") {
			continue
		}
		openByPolicy[incident.Policy.Name] = append(openByPolicy[incident.Policy.Name], incident)
	}

	out := make([]Alert, 0, len(alertPolicies))
	for _, policy := range alertPolicies {
		alert := Alert{
			Name:        policy.Name,
			DisplayName: policy.DisplayName,
			Policy:      policy.Name,
			State:       "ok",
			Severity:    strings.ToLower(policy.Severity),
			Project:     c.project,
			Region:      c.region,
			Labels:      map[string]string{},
			UpdatedAt:   time.Now().UTC(),
		}
		if !policy.Enabled {
			alert.State = "disabled"
		}
		if content := strings.TrimSpace(policy.Documentation.Content); content != "" {
			alert.Summary = content
		} else if len(policy.Conditions) > 0 {
			alert.Summary = policy.Conditions[0].DisplayName
		}
		for key, value := range policy.UserLabels {
			alert.Labels[key] = value
		}
		if partition := firstNonEmpty(policy.UserLabels["guardian_partition"], policy.UserLabels["guardian.io/partition"]); partition != "" {
			alert.Partition = partition
		}
		if open := openByPolicy[policy.Name]; len(open) > 0 {
			alert.State = "firing"
			if alert.Severity == "" {
				alert.Severity = strings.ToLower(open[0].Severity)
			}
			if summary := strings.TrimSpace(open[0].SummaryText); summary != "" {
				alert.Summary = summary
			}
			alert.StartedAt = open[0].StartedAt.UTC()
			for _, incident := range open {
				for key, value := range incident.Resource.Labels {
					alert.Labels[key] = value
				}
				if alert.Partition == "" {
					alert.Partition = firstNonEmpty(
						incident.Resource.Labels["guardian_partition"],
						incident.Resource.Labels["guardian.io/partition"],
					)
				}
				if alert.Region == "" || alert.Region == c.region {
					alert.Region = firstNonEmpty(incident.Resource.Labels["location"], incident.Resource.Labels["region"], alert.Region)
				}
			}
		}
		alert.Labels["alertname"] = firstNonEmpty(policy.DisplayName, policy.Name)
		alert.Labels["source"] = "gcp"
		if alert.Severity == "" {
			alert.Severity = "warning"
		}
		out = append(out, alert)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
