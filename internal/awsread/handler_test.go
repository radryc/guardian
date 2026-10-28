package awsread

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/radryc/guardian/internal/awsobserve"
	"github.com/radryc/guardian/internal/onboarding"
	"github.com/radryc/guardian/internal/pusher/probes"
)

type fakePromQL struct {
	lastPath   string
	lastRegion string
	lastParams url.Values
	payload    []byte
	status     int
}

func (f *fakePromQL) Do(_ context.Context, region, path string, params url.Values) ([]byte, int, error) {
	f.lastPath = path
	f.lastRegion = region
	f.lastParams = params
	if f.payload == nil {
		f.payload = []byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`)
	}
	return f.payload, f.status, nil
}

func TestHandlerProxiesCloudWatchPromQL(t *testing.T) {
	promql := &fakePromQL{}
	h := NewHandler(&fakeReader{}, "").WithPromQL(promql)

	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/aws/promql/query_range?region=eu-west-1&query=sum(%7BCPUUtilization%7D)&start=1&end=2&step=60s",
		nil,
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if promql.lastPath != "/api/v1/query_range" {
		t.Fatalf("path = %q, want /api/v1/query_range", promql.lastPath)
	}
	if promql.lastRegion != "eu-west-1" {
		t.Fatalf("region = %q, want eu-west-1", promql.lastRegion)
	}
	if promql.lastParams.Get("query") != "sum({CPUUtilization})" {
		t.Fatalf("query param = %q", promql.lastParams.Get("query"))
	}
	if _, ok := promql.lastParams["region"]; ok {
		t.Fatalf("region leaked into forwarded params: %#v", promql.lastParams)
	}
}

func TestPromQLOperationPath(t *testing.T) {
	cases := map[string]string{
		"/query":                 "/api/v1/query",
		"/query_range":           "/api/v1/query_range",
		"/series":                "/api/v1/series",
		"/labels":                "/api/v1/labels",
		"/label/__name__/values": "/api/v1/label/__name__/values",
	}
	for suffix, want := range cases {
		got, err := promqlOperationPath(suffix)
		if err != nil || got != want {
			t.Fatalf("promqlOperationPath(%q) = %q, %v; want %q", suffix, got, err, want)
		}
	}
	if _, err := promqlOperationPath("/bogus"); err == nil {
		t.Fatalf("expected error for unknown operation")
	}
}

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

func (f *fakeReader) ListServices(context.Context) ([]ServiceInfo, error) {
	services := make([]ServiceInfo, 0, len(f.services))
	for _, name := range f.services {
		services = append(services, ServiceInfo{Name: name})
	}
	return services, nil
}

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
		"annotations": {"guardian.partition": "checkout", "guardian.intent": "api"},
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
	if got := spans[0].Attributes["guardian_partition"]; got != "checkout" {
		t.Fatalf("guardian partition = %q, want checkout", got)
	}
	if got := spans[0].Attributes["guardian_intent"]; got != "api" {
		t.Fatalf("guardian intent = %q, want api", got)
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

type fakeObserved struct {
	stacks []awsobserve.StackState
	events []awsobserve.Event
}

func (f *fakeObserved) List(account, region string) []awsobserve.StackState { return f.stacks }
func (f *fakeObserved) Get(account, region, stackName string) (awsobserve.StackState, bool) {
	for _, state := range f.stacks {
		if state.StackName == stackName {
			return state, true
		}
	}
	return awsobserve.StackState{}, false
}
func (f *fakeObserved) Events(account, region, stackName string, since time.Time, limit int) []awsobserve.Event {
	return f.events
}
func (f *fakeObserved) Refresh(ctx context.Context, account, region string, stacks []string, detectDrift bool) ([]awsobserve.StackState, error) {
	return f.stacks, nil
}
func (f *fakeObserved) Template(ctx context.Context, account, region, stackName string) (string, error) {
	return `{"Resources":{}}`, nil
}
func (f *fakeObserved) StackResources(ctx context.Context, account, region, stackName string) ([]awsobserve.StackResource, error) {
	return []awsobserve.StackResource{{
		LogicalID:    "Bucket",
		PhysicalID:   "demo-bucket",
		ResourceType: "AWS::S3::Bucket",
		Status:       "CREATE_COMPLETE",
		DriftStatus:  "IN_SYNC",
	}}, nil
}
func (f *fakeObserved) DiffSource(ctx context.Context, req awsobserve.SourceDiffRequest) (*awsobserve.SourceDiffResult, error) {
	return &awsobserve.SourceDiffResult{StackName: req.StackName, Summary: "in sync"}, nil
}

type fakeOnboarding struct {
	descriptors []onboarding.Descriptor
}

func (f *fakeOnboarding) List(context.Context) ([]onboarding.Descriptor, error) {
	return f.descriptors, nil
}

type fakeProbes struct {
	results []probes.Result
}

func (f *fakeProbes) Latest() []probes.Result { return f.results }

func TestHandlerServesOnboardingDescriptors(t *testing.T) {
	reader := &fakeOnboarding{descriptors: []onboarding.Descriptor{{
		Stack: "CustomerAppStack", Partition: "customer-prod-1", Intent: "app",
	}}}
	h := NewHandler(&fakeReader{}, "").WithOnboarding(reader)
	req := httptest.NewRequest(http.MethodGet, "/v1/aws/onboarding", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "customer-prod-1") {
		t.Fatalf("body missing descriptor: %s", rec.Body.String())
	}
}

func TestHandlerServesProbeResults(t *testing.T) {
	provider := &fakeProbes{results: []probes.Result{
		{Partition: "customer-prod-1", Intent: "app", Name: "health", Status: "up"},
		{Partition: "other", Intent: "app", Name: "health", Status: "down"},
	}}
	h := NewHandler(&fakeReader{}, "").WithProbes(provider)
	req := httptest.NewRequest(http.MethodGet, "/v1/aws/probes?partition=customer-prod-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "customer-prod-1") || strings.Contains(body, "other") {
		t.Fatalf("probe filter body = %s", body)
	}
}

func TestHandlerPrefersNativeLabelValues(t *testing.T) {
	native := []byte(`{"status":"success","data":["http.server.request.duration"]}`)
	promql := &fakePromQL{payload: native}
	h := NewHandler(&fakeReader{}, "").WithPromQL(promql)
	req := httptest.NewRequest(http.MethodGet, "/v1/aws/promql/label/__name__/values", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "CPUUtilization") {
		t.Fatalf("native OTLP label values were replaced: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "http.server.request.duration") {
		t.Fatalf("native label values missing: %s", rec.Body.String())
	}
}

func TestHandlerServesObservedStacks(t *testing.T) {
	observed := &fakeObserved{stacks: []awsobserve.StackState{{
		Account: "1", Region: "eu-west-1", StackName: "AwsPusherStack", StackStatus: "UPDATE_COMPLETE",
	}}}
	h := NewHandler(&fakeReader{}, "").WithObserved(observed)

	req := httptest.NewRequest(http.MethodGet, "/v1/aws/observed/stacks?account=1&region=eu-west-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "AwsPusherStack") {
		t.Fatalf("body missing stack: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/aws/observed/stacks/AwsPusherStack?region=eu-west-1", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/aws/observed/stacks/MissingStack?region=eu-west-1", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandlerServesObservedTemplate(t *testing.T) {
	observed := &fakeObserved{}
	h := NewHandler(&fakeReader{}, "").WithObserved(observed)
	req := httptest.NewRequest(http.MethodGet, "/v1/aws/observed/stacks/AwsPusherStack/template?region=eu-west-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"template"`) {
		t.Fatalf("body missing template: %s", rec.Body.String())
	}
}

func TestHandlerObservedDisabled(t *testing.T) {
	h := NewHandler(&fakeReader{}, "")
	req := httptest.NewRequest(http.MethodGet, "/v1/aws/observed/stacks", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestHandlerMetaReturnsServiceNames(t *testing.T) {
	reader := &fakeReader{services: []string{"api", "worker"}}
	h := NewHandler(reader, "")

	req := httptest.NewRequest(http.MethodGet, "/v1/aws/meta", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		XrayServices []string `json:"xrayServices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.XrayServices) != 2 || body.XrayServices[0] != "api" || body.XrayServices[1] != "worker" {
		t.Fatalf("xrayServices = %#v, want [api worker]", body.XrayServices)
	}
}

func TestObservedStackResources(t *testing.T) {
	handler := NewHandler(nil, "").WithObserved(&fakeObserved{})
	req := httptest.NewRequest(http.MethodGet, "/v1/aws/observed/stacks/AwsPusherStack/resources?region=eu-west-1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Resources []awsobserve.StackResource `json:"resources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Resources) != 1 || body.Resources[0].PhysicalID != "demo-bucket" {
		t.Fatalf("resources = %+v", body.Resources)
	}
}
