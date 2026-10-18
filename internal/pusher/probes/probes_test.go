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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		body, _ := io.ReadAll(r.Body)
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
}

func TestProbeTelemetryJSONShape(t *testing.T) {
	resource := otelResource(Result{Service: "demo.api.app", Partition: "demo", Intent: "api", Asset: "app", Pusher: "k8s-main"})
	payload, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "service.name") {
		t.Fatalf("resource = %s", payload)
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
