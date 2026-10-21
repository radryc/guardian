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
	SortResults(results)
	for _, result := range results {
		resource := otelResource(result, r.cfg)
		attrs := metricAttributes(result, r.cfg)
		recordAttrs := recordAttributes(result, r.cfg)
		stateAge := 0.0
		ageFrom := result.CheckedAt
		if result.Success {
			ageFrom = result.CheckedAt
		} else if !result.LastSuccessAt.IsZero() {
			ageFrom = result.LastSuccessAt
		}
		if !ageFrom.IsZero() {
			if age := now.Sub(ageFrom).Seconds(); age > 0 {
				stateAge = age
			}
		}
		metrics = append(metrics, map[string]any{
			"resource": resource,
			"scopeMetrics": []any{map[string]any{
				"scope": map[string]any{"name": "guardian.probe"},
				"metrics": []any{
					metricGauge("guardian.probe.up", "1", boolFloat(result.Success), now, attrs),
					metricGauge("guardian.probe.duration.seconds", "s", float64(result.DurationMs)/1000, now, attrs),
					metricGauge("guardian.probe.last_success.timestamp.seconds", "s", timestampSeconds(result.LastSuccessAt), now, attrs),
					metricGauge("guardian.probe.consecutive_failures", "1", float64(result.ConsecutiveFailures), now, attrs),
					metricGauge("guardian.probe.state_age.seconds", "s", stateAge, now, attrs),
					metricGauge("guardian.probe.info", "1", 1, now, append(append([]any(nil), attrs...),
						attrString("guardian.probe.target", metricTarget(result.Target)),
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
					"scope": map[string]any{"name": "guardian.probe"},
					"logRecords": []any{map[string]any{
						"timeUnixNano":   strconv.FormatInt(now.UnixNano(), 10),
						"severityNumber": severity,
						"severityText":   severityText,
						"body":           map[string]any{"stringValue": body},
						"attributes":     recordAttrs,
					}},
				}},
			})
			spans = append(spans, map[string]any{
				"resource": resource,
				"scopeSpans": []any{map[string]any{
					"scope": map[string]any{"name": "guardian.probe"},
					"spans": []any{map[string]any{
						"traceId":           randomHex(16),
						"spanId":            randomHex(8),
						"name":              "guardian.probe.run",
						"kind":              1,
						"startTimeUnixNano": strconv.FormatInt(now.Add(-time.Duration(result.DurationMs)*time.Millisecond).UnixNano(), 10),
						"endTimeUnixNano":   strconv.FormatInt(now.UnixNano(), 10),
						"attributes":        recordAttrs,
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

func otelResource(result Result, cfg Config) map[string]any {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "local"
	}
	attrs := []any{
		attrString("service.name", result.Service),
		attrString("service.namespace", result.Partition),
		attrString("guardian.partition", result.Partition),
		attrString("guardian.intent", result.Intent),
		attrString("guardian.asset", result.Asset),
		attrString("guardian.pusher", result.Pusher),
		attrString("guardian.provider", provider),
	}
	if result.Cluster != "" {
		attrs = append(attrs, attrString("guardian.cluster", result.Cluster))
		if provider == "kubernetes" {
			attrs = append(attrs, attrString("k8s.cluster.name", result.Cluster))
		}
	}
	if provider == "aws" {
		if cfg.Account != "" {
			attrs = append(attrs, attrString("guardian.account", cfg.Account))
		}
		if cfg.Region != "" {
			attrs = append(attrs, attrString("guardian.region", cfg.Region))
		}
	}
	if result.FullName != "" {
		attrs = append(attrs, attrString("guardian.service.full_name", result.FullName))
		truncated := "false"
		if result.FullName != result.Service {
			truncated = "true"
		}
		attrs = append(attrs, attrString("guardian.service.truncated", truncated))
	}
	return map[string]any{"attributes": attrs}
}

// metricAttributes is the documented low-cardinality probe metric label set.
// Asset identity (service, partition, intent, asset, pusher, cluster, provider)
// comes from the OTLP resource, never from point attributes.
func metricAttributes(result Result, cfg Config) []any {
	return []any{
		attrString("guardian.probe.name", result.Name),
		attrString("guardian.probe.type", result.Type),
		attrString("guardian.probe.mode", result.Mode),
	}
}

// recordAttributes adds the probe correlation fields used by logs and spans.
// They are intentionally not part of the metric label set.
func recordAttributes(result Result, cfg Config) []any {
	attrs := append([]any(nil), metricAttributes(result, cfg)...)
	return append(attrs,
		attrString("guardian.probe.id", result.ProbeID),
		attrString("guardian.probe.status", result.Status),
	)
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

// metricTarget keeps the bounded host:port form in metric labels instead of a
// full URL with query parameters.
func metricTarget(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return value
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
