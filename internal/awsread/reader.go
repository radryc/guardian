// Package awsread exposes read-only AWS observability data (CloudWatch metrics,
// CloudWatch Logs Insights and X-Ray traces) through a small, normalized API so
// that Doctor can federate it into the same Grafana dataspace as OTLP telemetry.
package awsread

import (
	"context"
	"time"
)

// MetricDescriptor describes a discoverable CloudWatch metric.
type MetricDescriptor struct {
	Namespace  string   `json:"namespace"`
	MetricName string   `json:"metric_name"`
	Dimensions []string `json:"dimensions,omitempty"`
	Region     string   `json:"region,omitempty"`
}

// MetricPoint is a single CloudWatch datapoint.
type MetricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// MetricSeries is a normalized CloudWatch metric series.
type MetricSeries struct {
	Namespace  string            `json:"namespace"`
	MetricName string            `json:"metric_name"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
	Region     string            `json:"region,omitempty"`
	Account    string            `json:"account,omitempty"`
	Unit       string            `json:"unit,omitempty"`
	Points     []MetricPoint     `json:"points"`
}

// LogRecord is a normalized CloudWatch Logs Insights result row.
type LogRecord struct {
	Timestamp time.Time         `json:"timestamp"`
	LogGroup  string            `json:"log_group"`
	LogStream string            `json:"log_stream,omitempty"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
	Region    string            `json:"region,omitempty"`
	Account   string            `json:"account,omitempty"`
}

// Span is a normalized X-Ray segment/subsegment.
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
	Account      string            `json:"account,omitempty"`
}

// TraceSummary is a normalized X-Ray trace summary.
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
	Account    string            `json:"account,omitempty"`
}

// ServiceInfo identifies an X-Ray service together with its AWS partition
// (region and account) so callers can surface it in partition-aware service
// discovery alongside native telemetry.
type ServiceInfo struct {
	Name    string `json:"name"`
	Region  string `json:"region,omitempty"`
	Account string `json:"account,omitempty"`
}

// Alarm is a normalized CloudWatch alarm.
type Alarm struct {
	Name                    string            `json:"name"`
	ARN                     string            `json:"arn,omitempty"`
	Account                 string            `json:"account,omitempty"`
	Region                  string            `json:"region,omitempty"`
	Type                    string            `json:"type"` // metric or composite
	State                   string            `json:"state"`
	StateReason             string            `json:"stateReason,omitempty"`
	StateUpdatedAt          time.Time         `json:"stateUpdatedAt,omitempty"`
	Namespace               string            `json:"namespace,omitempty"`
	MetricName              string            `json:"metricName,omitempty"`
	Dimensions              map[string]string `json:"dimensions,omitempty"`
	Statistic               string            `json:"statistic,omitempty"`
	Period                  int32             `json:"period,omitempty"`
	EvaluationPeriods       int32             `json:"evaluationPeriods,omitempty"`
	DatapointsToAlarm       int32             `json:"datapointsToAlarm,omitempty"`
	Threshold               float64           `json:"threshold,omitempty"`
	ComparisonOperator      string            `json:"comparisonOperator,omitempty"`
	TreatMissingData        string            `json:"treatMissingData,omitempty"`
	ActionsEnabled          bool              `json:"actionsEnabled"`
	AlarmActions            []string          `json:"alarmActions,omitempty"`
	OKActions               []string          `json:"okActions,omitempty"`
	InsufficientDataActions []string          `json:"insufficientDataActions,omitempty"`
	Tags                    map[string]string `json:"tags,omitempty"`
	CompositeRule           string            `json:"compositeRule,omitempty"`
	Parent                  string            `json:"parent,omitempty"`
	Children                []string          `json:"children,omitempty"`
}

// AlarmQuery selects CloudWatch alarms.
type AlarmQuery struct {
	State      string
	NamePrefix string
	Region     string
	Limit      int
}

// AlarmHistoryQuery selects CloudWatch alarm history.
type AlarmHistoryQuery struct {
	AlarmName string
	Region    string
	From      time.Time
	To        time.Time
	Limit     int
}

// AlarmHistoryItem is one alarm state transition.
type AlarmHistoryItem struct {
	AlarmName       string    `json:"alarmName"`
	AlarmType       string    `json:"alarmType,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
	HistoryItemType string    `json:"historyItemType,omitempty"`
	HistorySummary  string    `json:"historySummary,omitempty"`
	HistoryData     string    `json:"historyData,omitempty"`
	Region          string    `json:"region,omitempty"`
	Account         string    `json:"account,omitempty"`
}

// MetricQuery selects a CloudWatch metric series.
type MetricQuery struct {
	Namespace  string
	MetricName string
	Dimensions map[string]string
	Stat       string
	Period     time.Duration
	From       time.Time
	To         time.Time
}

// LogQuery selects CloudWatch log records.
type LogQuery struct {
	LogGroups []string
	Query     string
	From      time.Time
	To        time.Time
	Limit     int
}

// TraceQuery selects X-Ray traces.
type TraceQuery struct {
	Service string
	From    time.Time
	To      time.Time
	Limit   int
}

// Reader is the read surface exposed to Doctor.
type Reader interface {
	ListMetrics(ctx context.Context, namespace, metricName string) ([]MetricDescriptor, error)
	QueryMetric(ctx context.Context, q MetricQuery) ([]MetricSeries, error)
	ListLogGroups(ctx context.Context) ([]string, error)
	QueryLogs(ctx context.Context, q LogQuery) ([]LogRecord, error)
	ListServices(ctx context.Context) ([]ServiceInfo, error)
	QueryTraces(ctx context.Context, q TraceQuery) ([]TraceSummary, error)
	GetTrace(ctx context.Context, traceID string) ([]Span, error)
	ListAlarms(ctx context.Context, q AlarmQuery) ([]Alarm, error)
	AlarmHistory(ctx context.Context, q AlarmHistoryQuery) ([]AlarmHistoryItem, error)
}
