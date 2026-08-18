package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	flowMetricWritesTotal      = "monofs_router_guardian_flow_writes_total"
	flowMetricWriteBytesTotal  = "monofs_router_guardian_flow_write_bytes_total"
	flowMetricDeletesTotal     = "monofs_router_guardian_flow_deletes_total"
	flowMetricDeleteBytesTotal = "monofs_router_guardian_flow_delete_bytes_total"
)

var (
	promLineRE  = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{[^}]*\})?\s+([-+]?\d+(?:\.\d+)?(?:[eE][-+]?\d+)?)$`)
	promLabelRE = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)="((?:\\.|[^"])*)"`)
)

type monofsMetricsFlowEvaluator struct {
	endpoint        string
	timeout         time.Duration
	refreshInterval time.Duration
	staleAfter      time.Duration
	client          *http.Client
	fallback        FlowStateEvaluator

	mu        sync.Mutex
	lastFetch time.Time
	values    map[flowMetricSeries]float64
	deltas    map[flowMetricSeries]float64
	lastErr   error
}

type flowMetricSeries struct {
	metric    string
	partition string
	intent    string
	pathKind  string
}

type flowMetricSnapshot struct {
	fetchedAt time.Time
	deltas    map[flowMetricSeries]float64
}

func NewMonofsMetricsFlowEvaluator(endpoint string, timeout, refreshInterval time.Duration) FlowStateEvaluator {
	return newMonofsMetricsFlowEvaluator(endpoint, timeout, refreshInterval, defaultFlowStateEvaluator())
}

func newMonofsMetricsFlowEvaluator(endpoint string, timeout, refreshInterval time.Duration, fallback FlowStateEvaluator) FlowStateEvaluator {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		if fallback != nil {
			return fallback
		}
		return defaultFlowStateEvaluator()
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if refreshInterval < 0 {
		refreshInterval = 5 * time.Second
	}
	if fallback == nil {
		fallback = defaultFlowStateEvaluator()
	}
	staleAfter := 30 * time.Second
	if refreshInterval > 0 {
		staleAfter = refreshInterval * 6
	}
	return &monofsMetricsFlowEvaluator{
		endpoint:        endpoint,
		timeout:         timeout,
		refreshInterval: refreshInterval,
		staleAfter:      staleAfter,
		client:          &http.Client{Timeout: timeout},
		fallback:        fallback,
		values:          map[flowMetricSeries]float64{},
		deltas:          map[flowMetricSeries]float64{},
	}
}

func (e *monofsMetricsFlowEvaluator) Evaluate(ctx context.Context, input FlowAssetContext) FlowObservation {
	if len(input.MetricGroups) == 0 {
		return FlowObservation{}
	}
	base := e.fallback.Evaluate(ctx, input)
	snap, err := e.snapshot(ctx)
	if err != nil {
		return degradeFlowObservation(base, "Flow metrics unavailable")
	}
	if !snap.fetchedAt.IsZero() && time.Since(snap.fetchedAt) > e.staleAfter {
		return degradeFlowObservation(base, "Flow metrics are stale")
	}

	partition := strings.TrimSpace(strings.ToLower(input.Partition))
	intent := strings.TrimSpace(strings.ToLower(input.Intent))
	writes := metricDeltaFor(snap.deltas, flowMetricWritesTotal, partition, intent)
	deletes := metricDeltaFor(snap.deltas, flowMetricDeletesTotal, partition, intent)
	writeBytes := metricDeltaFor(snap.deltas, flowMetricWriteBytesTotal, partition, intent)
	deleteBytes := metricDeltaFor(snap.deltas, flowMetricDeleteBytesTotal, partition, intent)

	activity := selectGroupActivity(input.MetricGroups, writes, deletes, writeBytes, deleteBytes)
	if activity <= 0 {
		return base
	}

	return FlowObservation{
		State:     "busy",
		Summary:   fmt.Sprintf("Recent flow activity detected (writes=%.0f deletes=%.0f)", writes, deletes),
		Source:    "monofs-metrics",
		UpdatedAt: snap.fetchedAt,
	}
}

func degradeFlowObservation(base FlowObservation, note string) FlowObservation {
	note = strings.TrimSpace(note)
	if note == "" {
		note = "Flow metrics unavailable"
	}
	if base.State == "" {
		base.State = "unknown"
	}
	if strings.TrimSpace(base.Summary) == "" {
		base.Summary = note
	} else if !strings.Contains(base.Summary, note) {
		base.Summary = base.Summary + "; " + note
	}
	if base.UpdatedAt.IsZero() {
		base.UpdatedAt = time.Now().UTC()
	}
	return base
}

func (e *monofsMetricsFlowEvaluator) snapshot(ctx context.Context) (flowMetricSnapshot, error) {
	now := time.Now().UTC()
	e.mu.Lock()
	lastFetch := e.lastFetch
	cached := cloneMetricMap(e.deltas)
	lastErr := e.lastErr
	if !lastFetch.IsZero() && e.refreshInterval > 0 && now.Sub(lastFetch) < e.refreshInterval {
		e.mu.Unlock()
		if lastErr != nil {
			return flowMetricSnapshot{}, lastErr
		}
		return flowMetricSnapshot{fetchedAt: lastFetch, deltas: cached}, nil
	}
	prevValues := cloneMetricMap(e.values)
	e.mu.Unlock()

	metrics, err := e.fetchMetrics(ctx)
	if err != nil {
		e.mu.Lock()
		e.lastFetch = now
		e.lastErr = err
		e.mu.Unlock()
		return flowMetricSnapshot{}, err
	}
	deltas := diffCounterValues(prevValues, metrics)

	e.mu.Lock()
	e.lastFetch = now
	e.lastErr = nil
	e.values = metrics
	e.deltas = deltas
	e.mu.Unlock()
	return flowMetricSnapshot{fetchedAt: now, deltas: cloneMetricMap(deltas)}, nil
}

func (e *monofsMetricsFlowEvaluator) fetchMetrics(ctx context.Context) (map[flowMetricSeries]float64, error) {
	requestCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, e.endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("metrics endpoint returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return parseMonofsFlowMetrics(resp.Body)
}

func parseMonofsFlowMetrics(in io.Reader) (map[flowMetricSeries]float64, error) {
	out := map[flowMetricSeries]float64{}
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		series, value, ok := parseMonofsFlowMetricLine(line)
		if !ok {
			continue
		}
		out[series] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseMonofsFlowMetricLine(line string) (flowMetricSeries, float64, bool) {
	matches := promLineRE.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 4 {
		return flowMetricSeries{}, 0, false
	}
	metric := matches[1]
	if !isSupportedFlowMetric(metric) {
		return flowMetricSeries{}, 0, false
	}
	labels := parsePrometheusLabels(matches[2])
	value, err := strconv.ParseFloat(matches[3], 64)
	if err != nil {
		return flowMetricSeries{}, 0, false
	}
	series := flowMetricSeries{
		metric:    metric,
		partition: fallbackLabel(labels["partition"], "unknown"),
		intent:    fallbackLabel(labels["intent"], "none"),
		pathKind:  fallbackLabel(labels["path_kind"], "other"),
	}
	return series, value, true
}

func isSupportedFlowMetric(name string) bool {
	switch name {
	case flowMetricWritesTotal, flowMetricWriteBytesTotal, flowMetricDeletesTotal, flowMetricDeleteBytesTotal:
		return true
	default:
		return false
	}
}

func parsePrometheusLabels(raw string) map[string]string {
	out := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	raw = strings.TrimPrefix(raw, "{")
	raw = strings.TrimSuffix(raw, "}")
	for _, match := range promLabelRE.FindAllStringSubmatch(raw, -1) {
		if len(match) != 3 {
			continue
		}
		decoded, err := strconv.Unquote(`"` + match[2] + `"`)
		if err != nil {
			decoded = match[2]
		}
		out[match[1]] = strings.TrimSpace(strings.ToLower(decoded))
	}
	return out
}

func fallbackLabel(value, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	return value
}

func diffCounterValues(previous, current map[flowMetricSeries]float64) map[flowMetricSeries]float64 {
	out := map[flowMetricSeries]float64{}
	for series, cur := range current {
		prev := previous[series]
		delta := cur - prev
		if prev == 0 && cur > 0 {
			delta = 0
		}
		if delta < 0 {
			delta = 0
		}
		out[series] = delta
	}
	return out
}

func cloneMetricMap(in map[flowMetricSeries]float64) map[flowMetricSeries]float64 {
	out := make(map[flowMetricSeries]float64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func metricDeltaFor(deltas map[flowMetricSeries]float64, metric, partition, intent string) float64 {
	var total float64
	for series, delta := range deltas {
		if series.metric != metric {
			continue
		}
		if series.partition != partition {
			continue
		}
		if series.intent != intent && series.intent != "none" {
			continue
		}
		total += delta
	}
	return total
}

func selectGroupActivity(groups []string, writes, deletes, writeBytes, deleteBytes float64) float64 {
	var activity float64
	for _, group := range groups {
		switch strings.TrimSpace(strings.ToLower(group)) {
		case "ingest", "write", "writes":
			activity += writes + writeBytes
		case "delete", "deletes":
			activity += deletes + deleteBytes
		case "churn", "activity", "flow":
			activity += writes + deletes + writeBytes + deleteBytes
		default:
			activity += writes + deletes + writeBytes + deleteBytes
		}
	}
	return activity
}
