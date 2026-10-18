package probes

import (
	"context"
	"net/http"
	"net/http/httptest"
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
