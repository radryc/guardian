package gcpread

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T) (*Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/projects/demo/metricDescriptors", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"metricDescriptors":[
			{"type":"run.googleapis.com/request_count","unit":"1","labels":[{"key":"response_code"}]},
			{"type":"compute.googleapis.com/instance/cpu/utilization","labels":[]}
		]}`))
	})
	mux.HandleFunc("/v3/projects/demo/timeSeries", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("filter"); !strings.Contains(got, `metric.type = "run.googleapis.com/request_count"`) {
			t.Errorf("filter = %q", got)
		}
		_, _ = w.Write([]byte(`{"timeSeries":[{
			"metric":{"type":"run.googleapis.com/request_count","labels":{"response_code":"200"}},
			"resource":{"type":"cloud_run_revision","labels":{"location":"europe-west1"}},
			"points":[
				{"interval":{"endTime":"2026-11-06T10:00:00Z"},"value":{"int64Value":"3"}},
				{"interval":{"endTime":"2026-11-06T10:01:00Z"},"value":{"doubleValue":4.5}}
			]
		}]}`))
	})
	mux.HandleFunc("/v2/projects/demo/logs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"logNames":["projects/demo/logs/app","projects/demo/logs/worker"]}`))
	})
	mux.HandleFunc("/v2/entries:list", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode entries body: %v", err)
		}
		if filter, _ := body["filter"].(string); !strings.Contains(filter, `logName = "projects/demo/logs/app"`) {
			t.Errorf("filter = %q", filter)
		}
		_, _ = w.Write([]byte(`{"entries":[{
			"logName":"projects/demo/logs/app",
			"timestamp":"2026-11-06T10:00:00Z",
			"severity":"INFO",
			"jsonPayload":{"message":"hello","request_id":"abc"},
			"resource":{"type":"cloud_run_revision","labels":{"revision_name":"app-00001","location":"europe-west1"}}
		}]}`))
	})
	mux.HandleFunc("/v1/projects/demo/traces", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"traces":[{
			"traceId":"abc123",
			"spans":[
				{"spanId":"root","name":"demo-api","startTime":"2026-11-06T10:00:00Z","endTime":"2026-11-06T10:00:01Z","attributes":{"service.name":"demo-api"}},
				{"spanId":"child","parentSpanId":"root","name":"db","startTime":"2026-11-06T10:00:00.1Z","endTime":"2026-11-06T10:00:00.5Z"}
			]
		}]}`))
	})
	mux.HandleFunc("/v1/projects/demo/traces/abc123", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"spans":[
			{"spanId":"root","name":"demo-api","startTime":"2026-11-06T10:00:00Z","endTime":"2026-11-06T10:00:01Z","attributes":{"service.name":"demo-api"}},
			{"spanId":"child","parentSpanId":"root","name":"db","startTime":"2026-11-06T10:00:00.1Z","endTime":"2026-11-06T10:00:00.5Z"}
		]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client, err := New(Config{
		Project:           "demo",
		DefaultRegion:     "europe-west1",
		HTTPClient:        server.Client(),
		MonitoringBaseURL: server.URL,
		LoggingBaseURL:    server.URL,
		TraceBaseURL:      server.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client, server
}

func TestListMetrics(t *testing.T) {
	client, _ := testClient(t)
	descriptors, err := client.ListMetrics(context.Background(), "", "")
	if err != nil {
		t.Fatalf("ListMetrics: %v", err)
	}
	if len(descriptors) != 2 {
		t.Fatalf("descriptors = %v", descriptors)
	}
	if descriptors[0].Namespace != "compute.googleapis.com" || descriptors[0].MetricName != "instance/cpu/utilization" {
		t.Errorf("first descriptor = %+v", descriptors[0])
	}
}

func TestQueryMetric(t *testing.T) {
	client, _ := testClient(t)
	series, err := client.QueryMetric(context.Background(), MetricQuery{
		Namespace:  "run.googleapis.com",
		MetricName: "request_count",
		Dimensions: map[string]string{"response_code": "200"},
		From:       time.Now().Add(-time.Hour),
		To:         time.Now(),
	})
	if err != nil {
		t.Fatalf("QueryMetric: %v", err)
	}
	if len(series) != 1 || len(series[0].Points) != 2 {
		t.Fatalf("series = %+v", series)
	}
	if series[0].Dimensions["response_code"] != "200" || series[0].Region != "europe-west1" {
		t.Errorf("dimensions/region = %v/%s", series[0].Dimensions, series[0].Region)
	}
	if series[0].Points[0].Value != 3 || series[0].Points[1].Value != 4.5 {
		t.Errorf("points = %+v", series[0].Points)
	}
}

func TestLogs(t *testing.T) {
	client, _ := testClient(t)
	names, err := client.ListLogNames(context.Background())
	if err != nil {
		t.Fatalf("ListLogNames: %v", err)
	}
	if len(names) != 2 || names[0] != "app" {
		t.Fatalf("log names = %v", names)
	}

	records, err := client.QueryLogs(context.Background(), LogQuery{LogName: "app", From: time.Now().Add(-time.Hour), To: time.Now(), Limit: 10})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %+v", records)
	}
	if records[0].Message != "hello" || records[0].LogGroup != "app" || records[0].LogStream != "app-00001" {
		t.Errorf("record = %+v", records[0])
	}
	if records[0].Fields["request_id"] != "abc" {
		t.Errorf("fields = %v", records[0].Fields)
	}
}

func TestTraces(t *testing.T) {
	client, _ := testClient(t)
	summaries, err := client.QueryTraces(context.Background(), TraceQuery{From: time.Now().Add(-time.Hour), To: time.Now(), Limit: 10})
	if err != nil {
		t.Fatalf("QueryTraces: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Service != "demo-api" || summaries[0].SpanCount != 2 {
		t.Fatalf("summaries = %+v", summaries)
	}
	if summaries[0].Duration != time.Second {
		t.Errorf("duration = %s", summaries[0].Duration)
	}

	spans, err := client.GetTrace(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if len(spans) != 2 || spans[1].ParentSpanID != "root" || spans[0].Service != "demo-api" {
		t.Fatalf("spans = %+v", spans)
	}
}

type fakeReader struct {
	metrics []MetricDescriptor
	logs    []LogRecord
	traces  []TraceSummary
	spans   []Span
}

func (f *fakeReader) ListMetrics(context.Context, string, string) ([]MetricDescriptor, error) {
	return f.metrics, nil
}
func (f *fakeReader) QueryMetric(context.Context, MetricQuery) ([]MetricSeries, error) {
	return []MetricSeries{{Namespace: "run.googleapis.com", MetricName: "request_count"}}, nil
}
func (f *fakeReader) ListLogNames(context.Context) ([]string, error) { return []string{"app"}, nil }
func (f *fakeReader) QueryLogs(context.Context, LogQuery) ([]LogRecord, error) {
	return f.logs, nil
}
func (f *fakeReader) ListServices(context.Context) ([]ServiceInfo, error) {
	return []ServiceInfo{{Name: "demo-api"}}, nil
}
func (f *fakeReader) QueryTraces(context.Context, TraceQuery) ([]TraceSummary, error) {
	return f.traces, nil
}
func (f *fakeReader) GetTrace(context.Context, string) ([]Span, error) { return f.spans, nil }
func (f *fakeReader) ListAlerts(context.Context) ([]Alert, error) {
	return []Alert{{Name: "projects/demo/alertPolicies/1", State: "firing", Severity: "warning"}}, nil
}

type fakeProm struct{}

func (fakeProm) Do(_ context.Context, suffix string, _ map[string][]string) ([]byte, int, error) {
	return []byte(`{"status":"success","suffix":"` + suffix + `"}`), http.StatusOK, nil
}

func TestHandlerAuthAndRoutes(t *testing.T) {
	handler := NewHandler(&fakeReader{}, "secret").
		WithPromQL(fakeProm{}).
		WithIdentity("gcp-demo", "demo", "europe-west1").
		WithScopes("read")

	cases := []struct {
		path   string
		status int
	}{
		{"/healthz", http.StatusOK},
		{"/v1/gcp/meta", http.StatusOK},
		{"/v1/gcp/metrics", http.StatusOK},
		{"/v1/gcp/metrics/query?namespace=run.googleapis.com&metric=request_count", http.StatusOK},
		{"/v1/gcp/log-names", http.StatusOK},
		{"/v1/gcp/logs/query?log=app", http.StatusOK},
		{"/v1/gcp/services", http.StatusOK},
		{"/v1/gcp/alerts", http.StatusOK},
		{"/v1/gcp/traces", http.StatusOK},
		{"/v1/gcp/traces/abc123", http.StatusOK},
		{"/v1/gcp/promql/api/v1/query?query=up", http.StatusOK},
		{"/v1/gcp/unknown", http.StatusNotFound},
	}
	for _, testCase := range cases {
		request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
		request.Header.Set("Authorization", "Bearer secret")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != testCase.status {
			t.Errorf("%s = %d, want %d (%s)", testCase.path, recorder.Code, testCase.status, recorder.Body.String())
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/gcp/meta", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("unauthorized = %d", recorder.Code)
	}
}

func TestParseTime(t *testing.T) {
	parsed, err := parseTime("2026-11-06T10:00:00Z", time.Time{})
	if err != nil || parsed.Year() != 2026 {
		t.Fatalf("parse RFC3339: %v %v", parsed, err)
	}
	parsed, err = parseTime("1762461600", time.Time{})
	if err != nil || parsed.IsZero() {
		t.Fatalf("parse unix: %v %v", parsed, err)
	}
	if _, err := parseTime("not-a-time", time.Time{}); err == nil {
		t.Fatal("expected an error")
	}
}
