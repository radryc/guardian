package awsread

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeReader struct {
	metrics   []MetricSeries
	logs      []LogRecord
	traces    []TraceSummary
	spans     []Span
	services  []string
	groups    []string
	lastQuery LogQuery
}

func (f *fakeReader) ListMetrics(context.Context, string, string) ([]MetricDescriptor, error) {
	return []MetricDescriptor{{Namespace: "AWS/ECS", MetricName: "CPUUtilization"}}, nil
}

func (f *fakeReader) QueryMetric(context.Context, MetricQuery) ([]MetricSeries, error) {
	return f.metrics, nil
}

func (f *fakeReader) ListLogGroups(context.Context) ([]string, error) { return f.groups, nil }

func (f *fakeReader) QueryLogs(_ context.Context, q LogQuery) ([]LogRecord, error) {
	f.lastQuery = q
	return f.logs, nil
}

func (f *fakeReader) ListServices(context.Context) ([]string, error) { return f.services, nil }

func (f *fakeReader) QueryTraces(context.Context, TraceQuery) ([]TraceSummary, error) {
	return f.traces, nil
}

func (f *fakeReader) GetTrace(context.Context, string) ([]Span, error) { return f.spans, nil }

func TestHandlerRequiresToken(t *testing.T) {
	h := NewHandler(&fakeReader{}, "secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/aws/log-groups", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}

	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerQueryLogsDecodesBody(t *testing.T) {
	reader := &fakeReader{logs: []LogRecord{{LogGroup: "/ecs/strata", Message: "hello"}}}
	h := NewHandler(reader, "")

	body := `{"log_groups":["/ecs/strata"],"query":"fields @timestamp, @message","limit":50}`
	req := httptest.NewRequest(http.MethodPost, "/v1/aws/logs/query", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(reader.lastQuery.LogGroups) != 1 || reader.lastQuery.LogGroups[0] != "/ecs/strata" {
		t.Fatalf("log groups = %#v", reader.lastQuery.LogGroups)
	}
	if reader.lastQuery.Limit != 50 {
		t.Fatalf("limit = %d, want 50", reader.lastQuery.Limit)
	}
}

func TestHandlerQueryMetricDimensions(t *testing.T) {
	reader := &fakeReader{metrics: []MetricSeries{{Namespace: "AWS/ECS", MetricName: "CPUUtilization"}}}
	h := NewHandler(reader, "")
	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/aws/metrics/query?namespace=AWS%2FECS&metric=CPUUtilization&dim.ClusterName=prod&stat=Average&period=60s",
		nil,
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var series []MetricSeries
	if err := json.Unmarshal(rec.Body.Bytes(), &series); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(series) != 1 || series[0].MetricName != "CPUUtilization" {
		t.Fatalf("series = %#v", series)
	}
}

func TestParseXRayDocument(t *testing.T) {
	document := `{
		"trace_id": "1-abc",
		"id": "seg-1",
		"name": "guardian-api",
		"start_time": 1700000000.0,
		"end_time": 1700000000.5,
		"subsegments": [
			{"id": "sub-1", "name": "SELECT", "type": "subsegment", "start_time": 1700000000.1, "end_time": 1700000000.2, "namespace": "aws"},
			{"id": "sub-2", "name": "call", "type": "subsegment", "start_time": 1700000000.2, "end_time": 1700000000.3, "error": true, "parent_id": "seg-1"}
		]
	}`
	spans := parseXRayDocument(document, "", "")
	if len(spans) != 3 {
		t.Fatalf("spans = %d, want 3", len(spans))
	}
	if spans[0].Service != "guardian-api" || spans[0].TraceID != "1-abc" {
		t.Fatalf("root span = %#v", spans[0])
	}
	if spans[1].Service != "aws" || spans[1].ParentSpanID != "seg-1" {
		t.Fatalf("aws subsegment = %#v", spans[1])
	}
	if spans[2].StatusCode != "ERROR" {
		t.Fatalf("error subsegment status = %q, want ERROR", spans[2].StatusCode)
	}
	if !spans[0].StartTime.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Fatalf("start time = %s", spans[0].StartTime)
	}
}
