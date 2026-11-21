package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/forge/author"
	"github.com/radryc/guardian/internal/forge/publish"
)

func testServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	configPath, err := author.CreatePartition(author.PartitionOptions{Root: root, Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := author.CreateIntent(author.IntentOptions{
		PartitionDir: filepath.Dir(configPath), Name: "api", Pusher: "k8s-main",
		Cluster: "k8s-main", Namespace: "demo",
	}); err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{
		Root: root, Token: "secret",
		DisplayKeys: map[string]string{"asset": "service"},
		Submit: func(_ context.Context, req publish.Request) (publish.Result, error) {
			return publish.Result{Partition: req.Partition, DryRun: req.DryRun, Operations: []publish.OperationSummary{{Kind: "upsert", Path: "partitions/demo/config.yaml"}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return server, root
}

func get(t *testing.T, server *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestCreatePartitionAPI(t *testing.T) {
	server, root := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/partition?token=secret", strings.NewReader(`{"name":"newpart"}`))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "partitions", "newpart", "config.yaml")); err != nil {
		t.Fatalf("partition config missing: %v", err)
	}
	config, err := os.ReadFile(filepath.Join(root, ".guardian", "config.yaml"))
	if err != nil {
		t.Fatalf("repo config missing: %v", err)
	}
	if !strings.Contains(string(config), "partition: newpart") {
		t.Fatalf("repo config missing binding:\n%s", config)
	}
}

func TestOverviewAndAuth(t *testing.T) {
	server, _ := testServer(t)
	if code := get(t, server, "/api/overview").Code; code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", code)
	}
	recorder := get(t, server, "/api/overview?token=secret")
	if recorder.Code != http.StatusOK {
		t.Fatalf("overview status = %d", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	partitions, _ := body["partitions"].([]any)
	if len(partitions) != 1 || partitions[0] != "demo" {
		t.Fatalf("partitions = %v", body["partitions"])
	}
	display, _ := body["display"].(map[string]any)
	if display["asset"] != "service" {
		t.Fatalf("display = %v", body["display"])
	}
}

func TestPartitionYAMLStaysCanonical(t *testing.T) {
	server, _ := testServer(t)
	recorder := get(t, server, "/api/partition?name=demo&token=secret")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "service:") {
		t.Fatal("display aliases must not appear in manifest YAML")
	}
	if !strings.Contains(recorder.Body.String(), "name: api") {
		t.Fatal("intent YAML missing from response")
	}
}

func TestAddAsset(t *testing.T) {
	server, _ := testServer(t)
	body := `{"partition":"demo","intent":"api","type":"Compute","name":"worker","image":"ghcr.io/acme/worker:1"}`
	request := httptest.NewRequest(http.MethodPost, "/api/asset?token=secret", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestSubmitRequiresConfirmation(t *testing.T) {
	server, _ := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/submit?token=secret", strings.NewReader(`{"partition":"demo"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestSubmitDryRun(t *testing.T) {
	server, _ := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/submit?token=secret", strings.NewReader(`{"partition":"demo","dryRun":true}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	data, _ := io.ReadAll(recorder.Body)
	if !strings.Contains(string(data), "dry-run") && !strings.Contains(string(data), "dryRun") {
		t.Fatalf("body = %s", data)
	}
}
