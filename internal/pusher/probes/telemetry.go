package probes

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type otelExporter struct {
	endpoint string
	client   *http.Client
}

func newOTELExporter(endpoint string) *otelExporter {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = deriveHTTPEndpoint(firstEnv("GUARDIAN_OTEL_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT"))
	}
	return &otelExporter{
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (r *Runner) emitTelemetry(ctx context.Context, results []Result, previous map[string]string) {
	if r.otel == nil || len(results) == 0 {
		return
	}
	now := time.Now().UTC()
	metrics := make([]any, 0)
	logs := make([]any, 0)
	spans := make([]any, 0)
	for _, result := range results {
		resource := otelResource(result)
		attrs := probeAttributes(result)
		metrics = append(metrics, map[string]any{
			"resource": resource,
			"scopeMetrics": []any{map[string]any{
				"scope": map[string]any{"name": "guardian.probes"},
				"metrics": []any{
					metricGauge("guardian_probe_up", "1", boolFloat(result.Success), now, attrs),
					metricGauge("guardian_probe_duration_seconds", "s", float64(result.DurationMs)/1000, now, attrs),
					metricGauge("guardian_probe_last_success_timestamp_seconds", "s", timestampSeconds(result.LastSuccessAt), now, attrs),
					metricGauge("guardian_probe_consecutive_failures", "1", float64(result.ConsecutiveFailures), now, attrs),
					metricGauge("guardian_probe_state_age_seconds", "s", 0, now, attrs),
					metricGauge("guardian_probe_info", "1", 1, now, append(attrs,
						attrString("guardian.probe.target", result.Target),
						attrString("guardian.probe.path", result.Path),
					)),
				},
			}},
		})
		changed := previous[result.ProbeID] != result.Status
		if changed || !result.Success {
			severity := 9
			severityText := "INFO"
			body := fmt.Sprintf("probe %s: %s/%s/%s %s %s after %dms", result.Status, result.Partition, result.Intent, result.Asset, result.Type, result.Target, result.DurationMs)
			if !result.Success {
				severity = 17
				severityText = "ERROR"
				body += ": " + result.Error
			}
			logs = append(logs, map[string]any{
				"resource": resource,
				"scopeLogs": []any{map[string]any{
					"scope": map[string]any{"name": "guardian.probes"},
					"logRecords": []any{map[string]any{
						"timeUnixNano":   strconv.FormatInt(now.UnixNano(), 10),
						"severityNumber": severity,
						"severityText":   severityText,
						"body":           map[string]any{"stringValue": body},
						"attributes":     attrs,
					}},
				}},
			})
			spans = append(spans, map[string]any{
				"resource": resource,
				"scopeSpans": []any{map[string]any{
					"scope": map[string]any{"name": "guardian.probes"},
					"spans": []any{map[string]any{
						"traceId":           randomHex(16),
						"spanId":            randomHex(8),
						"name":              "guardian.probe",
						"kind":              1,
						"startTimeUnixNano": strconv.FormatInt(now.Add(-time.Duration(result.DurationMs)*time.Millisecond).UnixNano(), 10),
						"endTimeUnixNano":   strconv.FormatInt(now.UnixNano(), 10),
						"attributes":        attrs,
						"status":            map[string]any{"code": spanStatus(result.Success), "message": result.Error},
					}},
				}},
			})
		}
	}
	if len(metrics) > 0 {
		_ = r.otel.send(ctx, "/v1/metrics", map[string]any{"resourceMetrics": metrics})
	}
	if len(logs) > 0 {
		_ = r.otel.send(ctx, "/v1/logs", map[string]any{"resourceLogs": logs})
	}
	if len(spans) > 0 {
		_ = r.otel.send(ctx, "/v1/traces", map[string]any{"resourceSpans": spans})
	}
}

func (e *otelExporter) send(ctx context.Context, path string, payload any) error {
	if e == nil || e.endpoint == "" {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("otlp export %s: HTTP %d", path, resp.StatusCode)
	}
	return nil
}

func otelResource(result Result) map[string]any {
	attrs := []any{
		attrString("service.name", result.Service),
		attrString("service.namespace", result.Partition),
		attrString("guardian.partition", result.Partition),
		attrString("guardian.intent", result.Intent),
		attrString("guardian.asset", result.Asset),
		attrString("guardian.pusher", result.Pusher),
		attrString("guardian.provider", "probe"),
	}
	if result.Cluster != "" {
		attrs = append(attrs, attrString("guardian.cluster", result.Cluster), attrString("k8s.cluster.name", result.Cluster))
	}
	if result.FullName != "" {
		attrs = append(attrs, attrString("guardian.service.full_name", result.FullName))
	}
	return map[string]any{"attributes": attrs}
}

func probeAttributes(result Result) []any {
	return []any{
		attrString("pusher", result.Pusher),
		attrString("cluster", result.Cluster),
		attrString("partition", result.Partition),
		attrString("intent", result.Intent),
		attrString("asset", result.Asset),
		attrString("probe", result.Name),
		attrString("type", result.Type),
		attrString("mode", result.Mode),
		attrString("provider", "probe"),
		attrString("guardian.probe.id", result.ProbeID),
		attrString("guardian.probe.status", result.Status),
	}
}

func metricGauge(name, unit string, value float64, at time.Time, attrs []any) map[string]any {
	return map[string]any{
		"name": name,
		"unit": unit,
		"gauge": map[string]any{
			"dataPoints": []any{map[string]any{
				"attributes":   attrs,
				"timeUnixNano": strconv.FormatInt(at.UnixNano(), 10),
				"asDouble":     value,
			}},
		},
	}
}

func attrString(key, value string) map[string]any {
	return map[string]any{"key": key, "value": map[string]any{"stringValue": value}}
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func timestampSeconds(value time.Time) float64 {
	if value.IsZero() {
		return 0
	}
	return float64(value.Unix())
}

func spanStatus(success bool) int {
	if success {
		return 1
	}
	return 2
}

func randomHex(bytesLen int) string {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", bytesLen*2)
	}
	return hex.EncodeToString(buf)
}

// HTTPEndpoint derives the collector OTLP/HTTP endpoint from a gRPC endpoint.
func HTTPEndpoint(grpcEndpoint string) string { return deriveHTTPEndpoint(grpcEndpoint) }

func deriveHTTPEndpoint(grpcEndpoint string) string {
	grpcEndpoint = strings.TrimSpace(grpcEndpoint)
	if grpcEndpoint == "" {
		return ""
	}
	if strings.HasPrefix(grpcEndpoint, "http://") || strings.HasPrefix(grpcEndpoint, "https://") {
		parsed, err := url.Parse(grpcEndpoint)
		if err != nil {
			return ""
		}
		if parsed.Port() == "4317" {
			parsed.Host = net.JoinHostPort(parsed.Hostname(), "4318")
			return parsed.String()
		}
		return strings.TrimRight(grpcEndpoint, "/")
	}
	host, port, err := net.SplitHostPort(grpcEndpoint)
	if err != nil {
		return "http://" + grpcEndpoint + ":4318"
	}
	if port == "4317" {
		port = "4318"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
