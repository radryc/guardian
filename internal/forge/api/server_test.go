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

func TestCreateIntentAPI(t *testing.T) {
	server, root := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/intent?token=secret",
		strings.NewReader(`{"partition":"demo","name":"worker","pusher":"k8s-main","cluster":"k8s-main","namespace":"demo"}`))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create intent status = %d: %s", recorder.Code, recorder.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(root, "partitions", "demo", "intents", "worker.yaml"))
	if err != nil {
		t.Fatalf("intent manifest missing: %v", err)
	}
	for _, want := range []string{"kind: Intent", "name: worker", "targetPusher: k8s-main"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("intent manifest missing %q:\n%s", want, data)
		}
	}
}

func TestCreateIntentRequiresPusher(t *testing.T) {
	server, root := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/intent?token=secret",
		strings.NewReader(`{"partition":"demo","name":"worker"}`))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "partitions", "demo", "intents", "worker.yaml")); !os.IsNotExist(err) {
		t.Fatalf("intent should not be written without a pusher: %v", err)
	}
}

func TestCreateAssetPropertiesAPI(t *testing.T) {
	server, root := testServer(t)
	body := `{"partition":"demo","intent":"api","type":"Compute","name":"web","properties":{
		"image":"ghcr.io/acme/web:1","replicas":2,"env":{"LOG_LEVEL":"info"},
		"resources":{"requests":{"cpu":"100m","memory":"128Mi"}},
		"ports":[{"name":"http","containerPort":8080,"protocol":"TCP"}]}}`
	request := httptest.NewRequest(http.MethodPost, "/api/asset?token=secret", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("create asset status = %d: %s", recorder.Code, recorder.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(root, "partitions", "demo", "intents", "api.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"image: ghcr.io/acme/web:1", "LOG_LEVEL: info", "cpu: 100m", "containerPort: 8080"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("asset manifest missing %q:\n%s", want, data)
		}
	}
}

func TestCreateJourneyProbeAPI(t *testing.T) {
	server, root := testServer(t)
	body := `{"partition":"demo","intent":"api","type":"Probe","name":"journey","properties":{
		"type":"http","url":"https://example.com","interval":"30s",
		"steps":[{"name":"read","method":"GET","path":"/healthz"},{"name":"update","method":"PUT","path":"/items/${id}","expectStatus":[200]}]}}`
	request := httptest.NewRequest(http.MethodPost, "/api/asset?token=secret", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("create probe status = %d: %s", recorder.Code, recorder.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(root, "partitions", "demo", "intents", "api.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type: Probe", "path: /items/${id}", "expectStatus:"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("journey manifest missing %q:\n%s", want, data)
		}
	}
}

func TestQuotaAPI(t *testing.T) {
	server, root := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/quota?token=secret",
		strings.NewReader(`{"partition":"demo","quota":{"profile":"medium","enforcement":"enforce"}}`))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("quota status = %d: %s", recorder.Code, recorder.Body.String())
	}
	config, err := os.ReadFile(filepath.Join(root, "partitions", "demo", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "quota:") || !strings.Contains(string(config), "profile: medium") {
		t.Fatalf("partition config missing quota:\n%s", config)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/quota?token=secret",
		strings.NewReader(`{"partition":"demo","clear":true}`))
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("clear quota status = %d: %s", recorder.Code, recorder.Body.String())
	}
	config, _ = os.ReadFile(filepath.Join(root, "partitions", "demo", "config.yaml"))
	if strings.Contains(string(config), "quota:") {
		t.Fatalf("quota was not cleared:\n%s", config)
	}
}

func TestQuotaRejectsUnknownProfileAndInlineLimits(t *testing.T) {
	server, _ := testServer(t)
	for _, body := range []string{
		`{"partition":"demo","quota":{"profile":"does-not-exist"}}`,
		`{"partition":"demo","quota":{"profile":"medium","query":{"requestsPerSecond":10}}}`,
		`{"partition":"demo","quota":{"enforcement":"observe"}}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/quota?token=secret", strings.NewReader(body))
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d: %s", body, recorder.Code, recorder.Body.String())
		}
	}
}

func TestCatalogIncludesQuotaProfiles(t *testing.T) {
	server, _ := testServer(t)
	recorder := get(t, server, "/api/catalog?token=secret")
	if recorder.Code != http.StatusOK {
		t.Fatalf("catalog status = %d", recorder.Code)
	}
	var body struct {
		AssetTypes    []map[string]any `json:"assetTypes"`
		QuotaProfiles []struct {
			Name    string `json:"name"`
			Builtin bool   `json:"builtin"`
		} `json:"quotaProfiles"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.AssetTypes) == 0 {
		t.Fatal("catalog has no asset types")
	}
	names := map[string]bool{}
	for _, profile := range body.QuotaProfiles {
		names[profile.Name] = true
	}
	for _, want := range []string{"small", "medium", "xlarge"} {
		if !names[want] {
			t.Fatalf("catalog missing quota profile %q", want)
		}
	}
}

func TestAccessPolicyAPI(t *testing.T) {
	server, root := testServer(t)
	policy := `version: 1
users:
  alice@example.com:
    partitions: [demo]
    logs: [demo]
    traces: [demo]
    role: viewer
`
	request := httptest.NewRequest(http.MethodPost, "/api/access?token=secret",
		strings.NewReader(mustJSON(t, map[string]any{"partition": "demo", "yaml": policy})))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("access status = %d: %s", recorder.Code, recorder.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(root, "partitions", "demo", "access.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != policy {
		t.Fatalf("access policy = %q", data)
	}

	bad := `version: 1
users:
  bob@example.com:
    partitions: [other]
    role: viewer
`
	request = httptest.NewRequest(http.MethodPost, "/api/access?token=secret",
		strings.NewReader(mustJSON(t, map[string]any{"partition": "demo", "yaml": bad})))
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("cross-partition access status = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestValuesAPI(t *testing.T) {
	server, root := testServer(t)
	post := func(path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path+"?token=secret", strings.NewReader(body))
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		return recorder
	}
	if recorder := post("/api/values", `{"partition":"demo","values":{"imageTag":"v1"}}`); recorder.Code != http.StatusOK {
		t.Fatalf("partition values status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := post("/api/values", `{"partition":"demo","intent":"api","values":{"replicas":3}}`); recorder.Code != http.StatusOK {
		t.Fatalf("intent values status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := post("/api/asset", `{"partition":"demo","intent":"api","type":"Compute","name":"app","properties":{"image":"repo:${value.imageTag}"}}`); recorder.Code != http.StatusOK {
		t.Fatalf("asset status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := post("/api/values", `{"partition":"demo","intent":"api","asset":"app","values":{"replicas":5}}`); recorder.Code != http.StatusOK {
		t.Fatalf("asset values status = %d: %s", recorder.Code, recorder.Body.String())
	}

	config, err := os.ReadFile(filepath.Join(root, "partitions", "demo", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "values:") || !strings.Contains(string(config), "imageTag: v1") {
		t.Fatalf("partition values missing:\n%s", config)
	}
	intentData, err := os.ReadFile(filepath.Join(root, "partitions", "demo", "intents", "api.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"replicas: 3", "replicas: 5", "repo:${value.imageTag}"} {
		if !strings.Contains(string(intentData), want) {
			t.Fatalf("intent values missing %q:\n%s", want, intentData)
		}
	}

	recorder := get(t, server, "/api/partition?name=demo&token=secret")
	if recorder.Code != http.StatusOK {
		t.Fatalf("detail status = %d", recorder.Code)
	}
	var detail struct {
		Values  map[string]any `json:"values"`
		Intents []struct {
			Name   string         `json:"name"`
			Values map[string]any `json:"values"`
			Assets []struct {
				Name   string         `json:"name"`
				Values map[string]any `json:"values"`
			} `json:"assets"`
		} `json:"intents"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Values["imageTag"] != "v1" {
		t.Fatalf("detail partition values = %#v", detail.Values)
	}
	if len(detail.Intents) != 1 || detail.Intents[0].Values["replicas"] != float64(3) {
		t.Fatalf("detail intent values = %#v", detail.Intents)
	}
	if len(detail.Intents[0].Assets) != 1 || detail.Intents[0].Assets[0].Values["replicas"] != float64(5) {
		t.Fatalf("detail asset values = %#v", detail.Intents[0].Assets)
	}

	if recorder := post("/api/values", `{"partition":"demo","intent":"api","asset":"missing","values":{"x":"y"}}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing asset status = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestProxyAuthAndAllowedOrigin(t *testing.T) {
	server, root := testServer(t)
	server.proxyAuthHeader = "X-Forwarded-User"
	server.allowedOrigins = []string{"https://forge.example.com"}

	post := func(remote, user, origin string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/partition", strings.NewReader(`{"name":"proxy"}`))
		request.RemoteAddr = remote
		if user != "" {
			request.Header.Set("X-Forwarded-User", user)
		}
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		return recorder
	}

	if recorder := post("127.0.0.1:1234", "alice@example.com", "https://forge.example.com"); recorder.Code != http.StatusCreated {
		t.Fatalf("proxy auth status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "partitions", "proxy", "config.yaml")); err != nil {
		t.Fatalf("proxy-created partition missing: %v", err)
	}
	if recorder := post("127.0.0.1:1234", "", "https://forge.example.com"); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing proxy user status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := post("192.0.2.10:1234", "alice@example.com", "https://forge.example.com"); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("non-loopback proxy status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := post("127.0.0.1:1234", "alice@example.com", "https://evil.example.com"); recorder.Code != http.StatusForbidden {
		t.Fatalf("foreign origin status = %d: %s", recorder.Code, recorder.Body.String())
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
