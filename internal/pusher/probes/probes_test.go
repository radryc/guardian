package probes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	statedomain "github.com/radryc/guardian/internal/domain/state"
)

func TestProbeHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	runner := New(Config{
		Enabled:     true,
		Pusher:      "k8s-main",
		Cluster:     "main",
		Provider:    "kubernetes",
		Timeout:     2 * time.Second,
		HTTPPaths:   []string{"/healthz"},
		Concurrency: 1,
		Store:       nil,
	})
	target := Target{
		ID:        "probe-1",
		Name:      "auto-http",
		Service:   "demo.api.app",
		Partition: "demo",
		Intent:    "api",
		Asset:     "app",
		Mode:      "auto",
		Type:      "http",
		URL:       server.URL,
		Interval:  time.Second,
		Timeout:   2 * time.Second,
	}
	result := runner.execute(context.Background(), target)
	if !result.Success || result.Status != "up" {
		t.Fatalf("result = %+v, want up", result)
	}
	if result.Path != "/healthz" {
		t.Fatalf("path = %q", result.Path)
	}
}

func TestProbeHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	runner := New(Config{Timeout: time.Second, HTTPPaths: []string{"/healthz"}, Concurrency: 1})
	target := Target{
		ID:        "probe-2",
		Name:      "auto-http",
		Partition: "demo",
		Intent:    "api",
		Asset:     "app",
		Type:      "http",
		URL:       server.URL,
		Timeout:   time.Second,
	}
	result := runner.execute(context.Background(), target)
	if result.Success || result.Status != "down" {
		t.Fatalf("result = %+v, want down", result)
	}
}

func TestProbeTelemetryUsesTargetService(t *testing.T) {
	paths := make([]string, 0)
	bodies := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		bodies[r.URL.Path] = string(body)
		if !strings.Contains(string(body), `"stringValue":"doctor.query.query"`) {
			t.Errorf("%s does not attribute target service: %s", r.URL.Path, body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	runner := New(Config{Pusher: "k8s-main", OTLPEndpoint: server.URL})
	result := Result{
		ProbeID:   "abc123",
		Service:   "doctor.query.query",
		Pusher:    "k8s-main",
		Partition: "doctor",
		Intent:    "query",
		Asset:     "query",
		Name:      "auto-http",
		Type:      "http",
		Mode:      "auto",
		Target:    "query:18080",
		Status:    "up",
		Success:   true,
		CheckedAt: time.Now().UTC(),
	}
	runner.emitTelemetry(context.Background(), []Result{result}, map[string]string{})
	if len(paths) < 2 {
		t.Fatalf("telemetry paths = %#v", paths)
	}
	for _, path := range []string{"/v1/metrics", "/v1/logs", "/v1/traces"} {
		if !contains(paths, path) {
			t.Fatalf("missing %s in %#v", path, paths)
		}
	}
	if !strings.Contains(bodies["/v1/metrics"], `"name":"guardian.probe.up"`) {
		t.Fatalf("probe metric name is not canonical: %s", bodies["/v1/metrics"])
	}
	if strings.Contains(bodies["/v1/metrics"], `"key":"partition"`) {
		t.Fatalf("probe metric repeats identity as a point label: %s", bodies["/v1/metrics"])
	}
	if !strings.Contains(bodies["/v1/traces"], `"name":"guardian.probe.run"`) {
		t.Fatalf("probe span name is not canonical: %s", bodies["/v1/traces"])
	}
}

func TestProbeTelemetryJSONShape(t *testing.T) {
	resource := otelResource(Result{Service: "demo.api.app", Partition: "demo", Intent: "api", Asset: "app", Pusher: "k8s-main"}, Config{Provider: "kubernetes", Cluster: "main"})
	payload, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "service.name") {
		t.Fatalf("resource = %s", payload)
	}
}

func TestProbeTelemetryJourneyAttributes(t *testing.T) {
	bodies := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies[r.URL.Path] = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	runner := New(Config{Pusher: "k8s-main", OTLPEndpoint: server.URL})
	result := Result{
		ProbeID:    "journey123",
		Service:    "demo.api.app",
		Pusher:     "k8s-main",
		Partition:  "demo",
		Intent:     "api",
		Asset:      "app",
		Name:       "checkout",
		Type:       "journey",
		Mode:       "explicit",
		Target:     "app:8080",
		Status:     "down",
		Success:    false,
		Error:      `step "update": unexpected HTTP status 500`,
		FailedStep: "update",
		Steps: []StepResult{
			{Name: "read", Type: "http", Status: "up", Success: true},
			{Name: "update", Type: "http", Status: "down", Success: false},
		},
		CheckedAt: time.Now().UTC(),
	}
	runner.emitTelemetry(context.Background(), []Result{result}, map[string]string{})
	for _, path := range []string{"/v1/logs", "/v1/traces"} {
		body := bodies[path]
		if !strings.Contains(body, `"guardian.probe.failed_step"`) {
			t.Fatalf("%s missing failed step: %s", path, body)
		}
		if !strings.Contains(body, `"guardian.probe.step_count"`) {
			t.Fatalf("%s missing step count: %s", path, body)
		}
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestSortResultsUsesTargetService(t *testing.T) {
	results := []Result{
		{Service: "z.intent.asset", Pusher: "a", ProbeID: "1"},
		{Service: "a.intent.asset", Pusher: "z", ProbeID: "2"},
		{Service: "a.intent.asset", Pusher: "b", ProbeID: "3"},
	}
	SortResults(results)
	if results[0].Service != "a.intent.asset" || results[0].Pusher != "b" {
		t.Fatalf("first result = %+v", results[0])
	}
	if results[1].Pusher != "z" || results[2].Service != "z.intent.asset" {
		t.Fatalf("unexpected order: %+v", results)
	}
}

func TestDisabledExplicitProbeSuppressesAutoprobe(t *testing.T) {
	runner := New(Config{Pusher: "k8s-main"})
	intent := &intentView{
		Partition: "demo",
		Name:      "api",
		Spec: intentdomain.IntentSpec{Assets: []intentdomain.AssetSpec{
			{Type: assetdomain.TypeProbe, Name: "health", Properties: map[string]any{"target": "app", "enabled": false}},
			{Type: assetdomain.TypeCompute, Name: "app"},
		}},
	}
	state := &statedomain.IntentState{Outputs: map[string]string{"app.address": "app:8080"}}
	policy := probePolicy{Enabled: true, Interval: time.Second, Timeout: time.Second, HTTPPaths: []string{"/healthz"}}
	if targets := runner.targetsForIntent(context.Background(), intent, state, policy); len(targets) != 0 {
		t.Fatalf("disabled explicit probe generated targets: %+v", targets)
	}
}

func TestSkippedProbeIsIgnored(t *testing.T) {
	runner := New(Config{Pusher: "k8s-main"})
	intent := &intentView{
		Partition: "demo",
		Name:      "api",
		Spec: intentdomain.IntentSpec{Assets: []intentdomain.AssetSpec{
			{Type: assetdomain.TypeProbe, Name: "health", Skip: true, Properties: map[string]any{"target": "app"}},
			{Type: assetdomain.TypeCompute, Name: "app"},
		}},
	}
	state := &statedomain.IntentState{Outputs: map[string]string{"app.address": "app:8080"}}
	policy := probePolicy{Enabled: true, Interval: time.Second, Timeout: time.Second, HTTPPaths: []string{"/healthz"}}
	if targets := runner.targetsForIntent(context.Background(), intent, state, policy); len(targets) != 1 {
		t.Fatalf("targets = %+v", targets)
	}
}

func TestSortTargetsPrioritizesExplicitAndService(t *testing.T) {
	targets := []Target{
		{ID: "3", Mode: "auto", Type: "tcp", Service: "a.intent.asset"},
		{ID: "2", Mode: "auto", Type: "http", Service: "z.intent.asset"},
		{ID: "1", Mode: "explicit", Type: "tcp", Service: "m.intent.asset"},
	}
	sortTargets(targets)
	if targets[0].ID != "1" || targets[1].ID != "2" || targets[2].ID != "3" {
		t.Fatalf("unexpected target order: %+v", targets)
	}
}

func TestShardOwnershipIsStable(t *testing.T) {
	first := New(Config{Pusher: "k8s-main", ShardIndex: 0, ShardCount: 2})
	second := New(Config{Pusher: "k8s-main", ShardIndex: 1, ShardCount: 2})
	for _, id := range []string{"a", "b", "c", "d"} {
		if first.ownsShard(id) == second.ownsShard(id) {
			t.Fatalf("shard ownership overlapped for %q", id)
		}
	}
}

func TestProbeResourceUsesProviderIdentity(t *testing.T) {
	resource := otelResource(Result{
		Service:   "aws-demo.api.app",
		FullName:  "aws-demo.api.app",
		Partition: "aws-demo",
		Intent:    "api",
		Asset:     "app",
		Pusher:    "aws-123",
	}, Config{Provider: "aws", Account: "123", Region: "us-east-1"})
	payload, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, want := range []string{`"guardian.provider"`, `"stringValue":"aws"`, `"guardian.account"`, `"guardian.region"`, `"guardian.service.truncated"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("resource missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, `"k8s.cluster.name"`) {
		t.Fatalf("AWS resource must not set k8s.cluster.name: %s", body)
	}
}

func TestProbeIDDeterministic(t *testing.T) {
	first := ProbeID("k8s-main", "demo", "api", "app", "auto-http", "http", "app:8080")
	second := ProbeID("k8s-main", "demo", "api", "app", "auto-http", "http", "app:8080")
	if first != second {
		t.Fatalf("probe IDs differ: %q vs %q", first, second)
	}
	if len(first) != 16 {
		t.Fatalf("probe ID length = %d", len(first))
	}
}

func TestProbeHTTPUsesExplicitURLPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/foo/bar" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	runner := New(Config{
		Timeout:     time.Second,
		HTTPPaths:   []string{"/healthz"},
		Concurrency: 1,
	})
	target := Target{
		ID:        "probe-url-path",
		Partition: "demo",
		Intent:    "api",
		Asset:     "app",
		Name:      "explicit",
		Type:      "http",
		URL:       server.URL + "/foo/bar",
		Timeout:   time.Second,
	}
	result := runner.execute(context.Background(), target)
	if !result.Success || result.Path != "/foo/bar" {
		t.Fatalf("result = %+v", result)
	}
}

func TestProbeHTTPPutBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/foo/bar" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"status":"ready"}` {
			t.Errorf("body = %s", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	runner := New(Config{Timeout: time.Second, Concurrency: 1})
	target := Target{
		ID:           "probe-put",
		Partition:    "demo",
		Intent:       "api",
		Asset:        "app",
		Name:         "update",
		Type:         "http",
		URL:          server.URL + "/foo/bar",
		Method:       "PUT",
		Body:         map[string]any{"status": "ready"},
		ExpectStatus: []int{http.StatusNoContent},
		Timeout:      time.Second,
	}
	result := runner.execute(context.Background(), target)
	if !result.Success || result.Status != "up" {
		t.Fatalf("result = %+v", result)
	}
}
