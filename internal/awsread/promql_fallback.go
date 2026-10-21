package awsread

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The CloudWatch PromQL endpoint returns an empty result set in some accounts
// even for metrics that are present in ListMetrics. Fall back to GetMetricData
// for the simple selectors used by the AWS dashboards (bare metric name,
// count(...), sum/avg/max/min and topk(...)).
type metricSeriesQuerier interface {
	QueryMetric(context.Context, MetricQuery) ([]MetricSeries, error)
}

type metricInsightsQuerier interface {
	QueryMetricInsights(context.Context, MetricQuery) ([]MetricSeries, error)
}

var promSelectorMetric = regexp.MustCompile(`(?:^|[\s(,])([A-Za-z_:][A-Za-z0-9_:]*)\s*(?:\{|\)|$)`)

func fallbackMetricName(query string) string {
	for _, raw := range promSelectorMetric.FindAllStringSubmatch(query, -1) {
		match := raw[1]
		switch strings.ToLower(match) {
		case "count", "sum", "avg", "max", "min", "topk", "bottomk", "rate", "irate",
			"increase", "count_over_time", "sum_over_time", "avg_over_time", "last_over_time",
			"by", "without", "on", "ignoring", "group_left", "group_right", "le":
			continue
		}
		return match
	}
	return ""
}

func parseFallbackTime(value string, fallback time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(n, 0).UTC()
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC()
	}
	return fallback
}

func parseFallbackDuration(value string, fallback time.Duration) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if d, err := time.ParseDuration(value); err == nil && d > 0 {
		return d
	}
	if n, err := strconv.Atoi(value); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return fallback
}

func promValue(ts time.Time, value float64) []any {
	return []any{float64(ts.Unix()), strconv.FormatFloat(value, 'g', -1, 64)}
}

func seriesLabels(series MetricSeries) map[string]string {
	labels := map[string]string{
		"__name__": series.MetricName,
		"source":   "aws",
	}
	if series.Namespace != "" {
		labels["namespace"] = series.Namespace
		labels["@aws.namespace"] = series.Namespace
	}
	if series.Region != "" {
		labels["@aws.region"] = series.Region
		labels["region"] = series.Region
	}
	if series.Account != "" {
		labels["@aws.account"] = series.Account
		labels["account"] = series.Account
	}
	for name, value := range series.Dimensions {
		if name != "" && value != "" {
			labels[name] = value
		}
	}
	// EMF metrics published by the demo carry the partition in their
	// namespace (guardian/<partition>/<intent>). Mirror it as the CloudWatch
	// tag label the AWS dashboards filter on.
	parts := strings.Split(strings.Trim(series.Namespace, "/"), "/")
	if len(parts) >= 2 && parts[0] == "guardian" {
		labels["@aws.tag.guardian-partition"] = parts[1]
		labels["guardian_partition"] = parts[1]
	}
	return labels
}

// promqlFallback executes a simple metric selector through GetMetricData and
// returns a Prometheus-compatible JSON response. It returns nil when the query
// is not reducible to a single CloudWatch metric.
func (h *Handler) promqlFallback(ctx context.Context, cwPath string, params url.Values) []byte {
	querier, ok := h.Reader.(metricSeriesQuerier)
	if !ok {
		return nil
	}
	insights, hasInsights := h.Reader.(metricInsightsQuerier)
	query := strings.TrimSpace(params.Get("query"))
	metric := fallbackMetricName(query)
	if metric == "" {
		return nil
	}

	now := time.Now().UTC()
	to := parseFallbackTime(params.Get("end"), now)
	start := parseFallbackTime(params.Get("start"), to.Add(-time.Hour))
	if cwPath == "/api/v1/query" {
		to = parseFallbackTime(params.Get("time"), now)
		start = to.Add(-5 * time.Minute)
	}
	if !start.Before(to) {
		start = to.Add(-5 * time.Minute)
	}
	period := parseFallbackDuration(params.Get("step"), time.Minute)
	if period < time.Minute {
		period = time.Minute
	}

	descriptors, err := h.Reader.ListMetrics(ctx, "", metric)
	if err != nil || len(descriptors) == 0 {
		return nil
	}
	var series []MetricSeries
	for _, descriptor := range descriptors {
		dimensions := make(map[string]string, len(descriptor.Dimensions))
		for _, name := range descriptor.Dimensions {
			if strings.TrimSpace(name) != "" {
				dimensions[name] = ""
			}
		}
		query := MetricQuery{
			Namespace:  descriptor.Namespace,
			MetricName: descriptor.MetricName,
			Dimensions: dimensions,
			Stat:       "Sum",
			Period:     period,
			From:       start,
			To:         to,
		}
		if hasInsights {
			if got, queryErr := insights.QueryMetricInsights(ctx, query); queryErr == nil {
				for _, item := range got {
					if len(item.Points) > 0 {
						series = append(series, item)
					}
				}
			}
		}
		if len(series) > 0 {
			continue
		}
		if got, queryErr := querier.QueryMetric(ctx, query); queryErr == nil {
			for _, item := range got {
				if len(item.Points) > 0 {
					series = append(series, item)
				}
			}
		}
	}
	if len(series) == 0 {
		return nil
	}

	lower := strings.ToLower(query)
	countOnly := strings.HasPrefix(lower, "count(") || strings.HasPrefix(lower, "count ")
	singleLast := cwPath == "/api/v1/query"

	response := struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string           `json:"resultType"`
			Result     []map[string]any `json:"result"`
		} `json:"data"`
	}{Status: "success"}
	if countOnly {
		response.Data.ResultType = "vector"
		labels := seriesLabels(series[0])
		response.Data.Result = append(response.Data.Result, map[string]any{
			"metric": labels,
			"value":  promValue(series[0].Points[len(series[0].Points)-1].Timestamp, float64(len(series))),
		})
	} else if singleLast {
		response.Data.ResultType = "vector"
		for _, item := range series {
			last := item.Points[len(item.Points)-1]
			response.Data.Result = append(response.Data.Result, map[string]any{
				"metric": seriesLabels(item),
				"value":  promValue(last.Timestamp, last.Value),
			})
		}
	} else {
		response.Data.ResultType = "matrix"
		for _, item := range series {
			values := make([][]any, 0, len(item.Points))
			for _, point := range item.Points {
				values = append(values, promValue(point.Timestamp, point.Value))
			}
			response.Data.Result = append(response.Data.Result, map[string]any{
				"metric": seriesLabels(item),
				"values": values,
			})
		}
	}
	payload, err := json.Marshal(response)
	if err != nil {
		return nil
	}
	return payload
}

// promqlEmpty reports whether a Prometheus response contains no series.
func promqlEmpty(payload []byte) bool {
	var parsed struct {
		Status string `json:"status"`
		Data   struct {
			Result []json.RawMessage `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return false
	}
	return parsed.Status == "success" && len(parsed.Data.Result) == 0
}

// promqlLabelValuesEmpty reports whether a Prometheus label-values response
// has no values. CloudWatch returns {"status":"success","data":[...]} for this
// endpoint rather than the vector shape handled by promqlEmpty.
func promqlLabelValuesEmpty(payload []byte) bool {
	var parsed struct {
		Status string            `json:"status"`
		Data   []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return false
	}
	return parsed.Status == "success" && len(parsed.Data) == 0
}
