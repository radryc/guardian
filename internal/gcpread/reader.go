// Package gcpread exposes read-only GCP observability data (Cloud Monitoring,
// Cloud Logging, Cloud Trace) through a normalized API so Doctor can federate
// it into the same Grafana dataspace as OTLP telemetry.
package gcpread

import (
	"context"
	"time"
)

// MetricDescriptor describes a discoverable Cloud Monitoring metric.
type MetricDescriptor struct {
	Namespace  string   `json:"namespace"`
	MetricName string   `json:"metric_name"`
	Dimensions []string `json:"dimensions,omitempty"`
	Region     string   `json:"region,omitempty"`
	Project    string   `json:"project,omitempty"`
}

// MetricPoint is a single Cloud Monitoring datapoint.
type MetricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// MetricSeries is a normalized Cloud Monitoring time series.
type MetricSeries struct {
	Namespace  string            `json:"namespace"`
	MetricName string            `json:"metric_name"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
	Region     string            `json:"region,omitempty"`
	Project    string            `json:"project,omitempty"`
	Unit       string            `json:"unit,omitempty"`
	Points     []MetricPoint     `json:"points"`
}

// LogRecord is a normalized Cloud Logging entry.
type LogRecord struct {
	Timestamp time.Time         `json:"timestamp"`
	LogGroup  string            `json:"log_group"`
	LogStream string            `json:"log_stream,omitempty"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
	Region    string            `json:"region,omitempty"`
	Project   string            `json:"project,omitempty"`
	Severity  string            `json:"severity,omitempty"`
}

// Span is a normalized Cloud Trace span.
type Span struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Name         string            `json:"name"`
	Service      string            `json:"service"`
	StartTime    time.Time         `json:"start_time"`
	EndTime      time.Time         `json:"end_time"`
	StatusCode   string            `json:"status_code,omitempty"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	Region       string            `json:"region,omitempty"`
	Project      string            `json:"project,omitempty"`
}

// TraceSummary is a normalized Cloud Trace trace summary.
type TraceSummary struct {
	TraceID    string            `json:"trace_id"`
	Service    string            `json:"service,omitempty"`
	StartTime  time.Time         `json:"start_time"`
	EndTime    time.Time         `json:"end_time"`
	Duration   time.Duration     `json:"duration"`
	Status     string            `json:"status,omitempty"`
	SpanCount  int               `json:"span_count,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Region     string            `json:"region,omitempty"`
	Project    string            `json:"project,omitempty"`
}

// ServiceInfo identifies a Cloud Trace service with its project metadata.
type ServiceInfo struct {
	Name    string `json:"name"`
	Region  string `json:"region,omitempty"`
	Project string `json:"project,omitempty"`
}

// MetricQuery selects a Cloud Monitoring time series.
type MetricQuery struct {
	Namespace    string
	MetricName   string
	Dimensions   map[string]string
	ResourceType string
	Aligner      string
	From         time.Time
	To           time.Time
}

// LogQuery selects Cloud Logging entries.
type LogQuery struct {
	LogName string
	Query   string
	From    time.Time
	To      time.Time
	Limit   int
}

// TraceQuery selects Cloud Trace traces.
type TraceQuery struct {
	Service string
	From    time.Time
	To      time.Time
	Limit   int
}

// Reader is the read surface served over HTTP and consumed by Doctor.
type Reader interface {
	ListMetrics(ctx context.Context, namespace, metric string) ([]MetricDescriptor, error)
	QueryMetric(ctx context.Context, q MetricQuery) ([]MetricSeries, error)
	ListLogNames(ctx context.Context) ([]string, error)
	QueryLogs(ctx context.Context, q LogQuery) ([]LogRecord, error)
	ListServices(ctx context.Context) ([]ServiceInfo, error)
	QueryTraces(ctx context.Context, q TraceQuery) ([]TraceSummary, error)
	GetTrace(ctx context.Context, traceID string) ([]Span, error)
}

// PromQLProxier forwards PromQL operations to Cloud Monitoring's
// Prometheus-compatible endpoint.
type PromQLProxier interface {
	Do(ctx context.Context, suffix string, params map[string][]string) ([]byte, int, error)
}

// Alert is a normalized Cloud Monitoring alert policy plus its open incidents.
type Alert struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"displayName,omitempty"`
	Policy      string            `json:"policy,omitempty"`
	State       string            `json:"state"`
	Severity    string            `json:"severity,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	Partition   string            `json:"partition,omitempty"`
	Project     string            `json:"project,omitempty"`
	Region      string            `json:"region,omitempty"`
	StartedAt   time.Time         `json:"startedAt,omitempty"`
	UpdatedAt   time.Time         `json:"updatedAt,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// AlertReader is implemented by readers that can list alert policies and open
// incidents. It is optional so the core Reader interface stays small.
type AlertReader interface {
	ListAlerts(ctx context.Context) ([]Alert, error)
}
