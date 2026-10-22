// Package dashboardgen renders a per-partition Grafana dashboard for observed
// customer stacks. The dashboard is intentionally generic: probe health, app
// OTLP metrics filtered by guardian.partition, the stack log group, and a trace
// drill-down. Grafana provisioning can mount the generated JSON from the
// Guardian registry.
package dashboardgen

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const (
	prometheusDatasource = "prometheus"
	lokiDatasource       = "doctor-logs"
	tempoDatasource      = "doctor-traces"
)

// Info is the partition metadata used to render a dashboard.
type Info struct {
	Partition string
	Account   string
	Region    string
	Stacks    []string
	LogGroup  string
	// Source is the telemetry source label: "aws" when metrics are queried
	// through the CloudWatch pusher backend, "doctor" when they are in the
	// central Prometheus pipeline (the kind demo).
	Source string
}

// Render returns an indented Grafana dashboard JSON document.
func Render(info Info) ([]byte, error) {
	partition := strings.TrimSpace(info.Partition)
	if partition == "" {
		return nil, fmt.Errorf("dashboardgen: partition is required")
	}
	uid := "partition-" + slug(partition)
	title := "Partition - " + partition
	logGroup := strings.TrimSpace(info.LogGroup)
	if logGroup == "" {
		logGroup = "/aws/ecs/" + partition
	}
	source := strings.TrimSpace(info.Source)
	if source == "" {
		source = "aws"
	}
	// The central Doctor pipeline normalizes OTLP resource attributes to
	// guardian_partition, while the CloudWatch pusher read API exposes them as
	// @resource.guardian.partition. The left side wins when it has data, so the
	// same board works in both topologies.
	probeSelector := fmt.Sprintf(
		`max(guardian_probe_up{source="%s", guardian_partition="%s"}) or max(guardian_probe_up{source="%s", "@resource.guardian.partition"="%s"})`,
		escape(source), escape(partition), escape(source), escape(partition),
	)
	probeLatency := fmt.Sprintf(
		`max by (guardian_probe_name) (guardian_probe_duration_seconds{source="%s", guardian_partition="%s"}) or max by (guardian_probe_name) (guardian_probe_duration_seconds{source="%s", "@resource.guardian.partition"="%s"})`,
		escape(source), escape(partition), escape(source), escape(partition),
	)
	awsSelector := fmt.Sprintf(`"@aws.tag.guardian.io/partition"=~"%s"`, escape(partition))
	appMetric := fmt.Sprintf(`sum(Invocations{source="%s",%s})`, escape(source), awsSelector)
	metricExplorer := fmt.Sprintf(`sum($awsMetric{source="%s",%s})`, escape(source), awsSelector)
	invocationsByFunction := fmt.Sprintf(`sum by (FunctionName) (Invocations{source="%s",%s})`, escape(source), awsSelector)
	// CloudWatch records returned by the pusher carry the account id in the log
	// group label ("780603797542:guardian/.../app"). Match with a prefix regex
	// so the same panel works with and without the account prefix.
	logGroupPattern := ".*" + regexp.QuoteMeta(logGroup)
	awsLogVolume := fmt.Sprintf(`sum(count_over_time({source="%s", aws_log_group=~"%s"}[$__rate_interval]))`, escape(source), escape(logGroupPattern))
	logQuery := fmt.Sprintf(`{source="%s", aws_log_group=~"%s"}`, escape(source), escape(logGroupPattern))
	traceSearch := fmt.Sprintf(`{ source = "%s" && resource.service.name =~ "%s.*" }`, escape(source), escape(partition))

	panels := []any{
		textPanel(1, "Overview", fmt.Sprintf(
			"### %s\n\n**Account:** %s  \n**Region:** %s  \n**Stacks:** %s\n\n**AWS metric example:** `%s`\n\n**AWS trace filter:** `%s`",
			partition, emptyDash(info.Account), emptyDash(info.Region), strings.Join(info.Stacks, ", "), appMetric, traceSearch,
		), 0, 0),
		statPanel(2, "Probes Up", probeSelector, 0, 4),
		timeseriesPanel(3, "Probe Latency", probeLatency, 4, 4),
		statPanel(7, "Lambda Invocations", fmt.Sprintf(`sum(Invocations{source="%s",%s}) or vector(0)`, escape(source), awsSelector), 0, 12),
		statPanel(8, "Lambda Errors", fmt.Sprintf(`sum(Errors{source="%s",%s}) or vector(0)`, escape(source), awsSelector), 6, 12),
		statPanel(9, "Lambda Throttles", fmt.Sprintf(`sum(Throttles{source="%s",%s}) or vector(0)`, escape(source), awsSelector), 12, 12),
		statPanel(10, "ECS CPU %", fmt.Sprintf(`avg(CPUUtilization{source="%s"}) or vector(0)`, escape(source)), 18, 12),
		timeseriesPanel(11, "CloudWatch metric: $awsMetric", metricExplorer, 0, 20),
		timeseriesPanel(12, "Invocations by Function", invocationsByFunction, 0, 28),
		timeseriesPanelWithUnit(13, "CloudWatch Log Volume", awsLogVolume, 12, 28, "short"),
		logsPanel(4, "Logs", logQuery, 0, 36),
		tracesPanel(5, "Trace Drill-down", "$traceId", 48),
		tracesPanel(14, "X-Ray Traces", traceSearch, 64),
		textPanel(6, "Queries", fmt.Sprintf(
			"### Query cheatsheet\n\n**AWS metric**\n```promql\n%s\n```\n\n**Probes**\n```promql\n%s\n```\n\n**Logs**\n```logql\n%s\n```\n\n**Traces**\n```traceql\n%s\n```",
			appMetric, probeSelector, logQuery, traceSearch,
		), 0, 80),
	}
	dashboard := map[string]any{
		"uid":           uid,
		"title":         title,
		"tags":          []string{"partition", "observed", "guardian", "aws"},
		"timezone":      "browser",
		"schemaVersion": 39,
		"version":       1,
		"refresh":       "30s",
		"time":          map[string]any{"from": "now-1h", "to": "now"},
		"editable":      true,
		"templating": map[string]any{"list": []any{
			map[string]any{
				"name": "awsMetric", "label": "CloudWatch metric", "type": "textbox",
				"query": "Invocations", "current": map[string]any{"text": "Invocations", "value": "Invocations"}, "hide": 0,
			},
			map[string]any{
				"name": "traceId", "label": "Trace ID", "type": "textbox",
				"query": "", "current": map[string]any{"text": "", "value": ""}, "hide": 0,
			},
		}},
		"panels": panels,
	}
	return json.MarshalIndent(dashboard, "", "  ")
}

func textPanel(id int, title, content string, x, y int) map[string]any {
	return map[string]any{
		"id": id, "type": "text", "title": title,
		"gridPos": map[string]any{"h": 4, "w": 24, "x": x, "y": y},
		"options": map[string]any{"mode": "markdown", "content": content},
	}
}

func statPanel(id int, title, expr string, x, y int) map[string]any {
	return map[string]any{
		"id": id, "type": "stat", "title": title,
		"datasource":  map[string]any{"type": "prometheus", "uid": prometheusDatasource},
		"gridPos":     map[string]any{"h": 8, "w": 6, "x": x, "y": y},
		"fieldConfig": map[string]any{"defaults": map[string]any{"unit": "short", "color": map[string]any{"mode": "thresholds"}}, "overrides": []any{}},
		"options":     map[string]any{"reduceOptions": map[string]any{"calcs": []string{"lastNotNull"}, "fields": "", "values": false}, "colorMode": "value", "graphMode": "area", "justifyMode": "auto", "orientation": "auto", "textMode": "auto"},
		"targets":     []any{promTarget(expr)},
	}
}

func timeseriesPanel(id int, title, expr string, x, y int) map[string]any {
	return timeseriesPanelAt(id, title, expr, x, y, 18, "s")
}

func timeseriesPanelWithUnit(id int, title, expr string, x, y int, unit string) map[string]any {
	return timeseriesPanelAt(id, title, expr, x, y, 12, unit)
}

func timeseriesPanelAt(id int, title, expr string, x, y, w int, unit string) map[string]any {
	return map[string]any{
		"id": id, "type": "timeseries", "title": title,
		"datasource":  map[string]any{"type": "prometheus", "uid": prometheusDatasource},
		"gridPos":     map[string]any{"h": 8, "w": w, "x": x, "y": y},
		"fieldConfig": map[string]any{"defaults": map[string]any{"unit": unit, "custom": map[string]any{"drawStyle": "line", "fillOpacity": 12, "lineInterpolation": "smooth", "showPoints": "never"}}, "overrides": []any{}},
		"options":     map[string]any{"legend": map[string]any{"displayMode": "table", "placement": "bottom", "calcs": []string{"lastNotNull", "max"}}, "tooltip": map[string]any{"mode": "multi", "sort": "desc"}},
		"targets":     []any{promTarget(expr)},
	}
}

func logsPanel(id int, title, expr string, x, y int) map[string]any {
	return map[string]any{
		"id": id, "type": "logs", "title": title,
		"datasource": map[string]any{"type": "loki", "uid": lokiDatasource},
		"gridPos":    map[string]any{"h": 12, "w": 24, "x": x, "y": y},
		"options":    map[string]any{"showTime": true, "showLabels": true, "wrapLogMessage": true, "enableLogDetails": true, "dedupStrategy": "none", "sortOrder": "Descending"},
		"targets":    []any{map[string]any{"refId": "A", "expr": expr, "queryType": "range", "direction": "backward"}},
	}
}

func tracesPanel(id int, title, query string, y int) map[string]any {
	return map[string]any{
		"id": id, "type": "traces", "title": title,
		"datasource": map[string]any{"type": "tempo", "uid": tempoDatasource},
		"gridPos":    map[string]any{"h": 16, "w": 24, "x": 0, "y": y},
		"options":    map[string]any{},
		"targets":    []any{map[string]any{"refId": "A", "queryType": "traceql", "query": query}},
	}
}

func promTarget(expr string) map[string]any {
	return map[string]any{
		"refId": "A", "expr": expr,
		"datasource": map[string]any{"type": "prometheus", "uid": prometheusDatasource},
	}
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func escape(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	prevDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
