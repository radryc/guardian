package probes

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/radryc/guardian/internal/paths"
	memorystore "github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func TestProbeJourneyHTTPGetPutPost(t *testing.T) {
	var mu sync.Mutex
	requests := map[string]map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if len(body) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("%s body is not JSON: %v (%s)", r.URL.Path, err, body)
			}
		}
		mu.Lock()
		requests[r.Method+" "+r.URL.Path] = payload
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /foo/bar":
			w.Header().Set("X-Trace", "trace-1")
			_, _ = w.Write([]byte(`{"id":"42","name":"widget","status":"old"}`))
		case "PUT /foo/bar/42":
			if payload["status"] != "modified" || payload["name"] != "widget" {
				t.Errorf("update payload = %#v", payload)
			}
			_, _ = w.Write([]byte(`{"id":"42","status":"modified"}`))
		case "POST /foo/42/events":
			if payload["event"] != "modified" {
				t.Errorf("event payload = %#v", payload)
			}
			if nested, ok := payload["payload"].(map[string]any); !ok || nested["id"] != "42" {
				t.Errorf("nested payload = %#v", payload["payload"])
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"evt-1"}`))
		case "GET /foo/42/events/evt-1":
			_, _ = w.Write([]byte(`{"id":"evt-1","event":"modified"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target := Target{
		ID:        "journey-http",
		Partition: "demo",
		Intent:    "api",
		Asset:     "app",
		Name:      "checkout",
		Type:      "journey",
		Address:   strings.TrimPrefix(server.URL, "http://"),
		Timeout:   time.Second,
		Steps: []Step{
			{
				Name:    "read",
				Type:    "http",
				Method:  "GET",
				Path:    "/foo/bar",
				Capture: map[string]string{"foo": "json:$", "trace": "header:X-Trace"},
			},
			{
				Name:         "update",
				Type:         "http",
				Method:       "PUT",
				Path:         "/foo/bar/${foo.id}",
				Body:         map[string]any{"id": "${foo.id}", "name": "${foo.name}", "status": "modified"},
				ExpectStatus: []int{http.StatusOK},
			},
			{
				Name:         "create",
				Type:         "http",
				Method:       "POST",
				Path:         "/foo/${foo.id}/events",
				Body:         map[string]any{"event": "modified", "payload": "${foo}"},
				ExpectStatus: []int{http.StatusCreated},
				Capture:      map[string]string{"eventId": "json:$.id"},
			},
			{
				Name:       "verify",
				Type:       "http",
				Method:     "GET",
				Path:       "/foo/${foo.id}/events/${eventId}",
				ExpectBody: `"event":"modified"`,
			},
		},
	}
	runner := New(Config{Pusher: "k8s-main", Timeout: time.Second})
	result := runner.execute(context.Background(), target)
	if !result.Success || result.Status != "up" {
		t.Fatalf("result = %+v", result)
	}
	if result.Type != "journey" || result.FailedStep != "" {
		t.Fatalf("type/failed step = %q/%q", result.Type, result.FailedStep)
	}
	if len(result.Steps) != 4 {
		t.Fatalf("steps = %+v", result.Steps)
	}
	for _, step := range result.Steps {
		if !step.Success || step.Status != "up" {
			t.Fatalf("step = %+v", step)
		}
	}
	if got := strings.Join(result.Steps[0].Captured, ","); got != "foo,trace" {
		t.Fatalf("captures = %q", got)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d", result.StatusCode)
	}
	if result.Steps[0].Path != "/foo/bar" {
		t.Fatalf("first step path = %q", result.Steps[0].Path)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("requests = %#v", requests)
	}
	if requests["PUT /foo/bar/42"] == nil {
		t.Fatalf("missing PUT request: %#v", requests)
	}
}

func TestProbeJourneyPatch(t *testing.T) {
	var posted map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/item":
			_, _ = w.Write([]byte(`{"id":"7","status":"old","nested":{"keep":1,"drop":2}}`))
		case "/item/7":
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &posted); err != nil {
				t.Errorf("patch body: %v (%s)", err, body)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target := Target{
		ID:        "journey-patch",
		Partition: "demo",
		Intent:    "api",
		Asset:     "item",
		Name:      "patch",
		Type:      "journey",
		Address:   strings.TrimPrefix(server.URL, "http://"),
		Timeout:   time.Second,
		Steps: []Step{
			{Name: "read", Type: "http", Method: "GET", Path: "/item", Capture: map[string]string{"item": "json:$"}},
			{
				Name:         "update",
				Type:         "http",
				Method:       "POST",
				Path:         "/item/${item.id}",
				Body:         "${item}",
				Patch:        map[string]any{"status": "new", "nested": map[string]any{"drop": nil}},
				ExpectStatus: []int{http.StatusNoContent},
			},
		},
	}
	result := New(Config{Pusher: "k8s-main", Timeout: time.Second}).execute(context.Background(), target)
	if !result.Success {
		t.Fatalf("result = %+v", result)
	}
	if posted["id"] != "7" || posted["status"] != "new" {
		t.Fatalf("posted = %#v", posted)
	}
	nested, ok := posted["nested"].(map[string]any)
	if !ok || nested["keep"] != float64(1) {
		t.Fatalf("nested = %#v", posted["nested"])
	}
	if _, exists := nested["drop"]; exists {
		t.Fatalf("patch did not delete nested.drop: %#v", nested)
	}
}

func TestProbeJourneyExpectJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately format the JSON with spaces: expectJson must not depend
		// on wire formatting.
		_, _ = w.Write([]byte(`{"status": "success", "data": {"count": 1}}`))
	}))
	defer server.Close()

	base := Target{
		ID:        "journey-json",
		Partition: "demo",
		Intent:    "api",
		Asset:     "api",
		Name:      "json",
		Type:      "journey",
		Address:   strings.TrimPrefix(server.URL, "http://"),
		Timeout:   time.Second,
	}
	okTarget := base
	okTarget.Steps = []Step{{
		Name:       "check",
		Type:       "http",
		Method:     "GET",
		Path:       "/json",
		ExpectJSON: map[string]any{"status": "success", "data.count": 1},
	}}
	result := New(Config{Pusher: "k8s-main", Timeout: time.Second}).execute(context.Background(), okTarget)
	if !result.Success {
		t.Fatalf("result = %+v", result)
	}

	badTarget := base
	badTarget.Steps = []Step{{
		Name:       "check",
		Type:       "http",
		Method:     "GET",
		Path:       "/json",
		ExpectJSON: map[string]any{"status": "failure"},
	}}
	result = New(Config{Pusher: "k8s-main", Timeout: time.Second}).execute(context.Background(), badTarget)
	if result.Success || result.FailedStep != "check" {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Error, "expectJson status") {
		t.Fatalf("error = %q", result.Error)
	}
}

func TestProbeJourneyStopsAtFailedStep(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/second" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	target := Target{
		ID:        "journey-failure",
		Partition: "demo",
		Intent:    "api",
		Asset:     "app",
		Name:      "failure",
		Type:      "journey",
		Address:   strings.TrimPrefix(server.URL, "http://"),
		Timeout:   time.Second,
		Steps: []Step{
			{Name: "first", Type: "http", Path: "/first"},
			{Name: "second", Type: "http", Path: "/second"},
			{Name: "third", Type: "http", Path: "/third"},
		},
	}
	result := New(Config{Pusher: "k8s-main", Timeout: time.Second}).execute(context.Background(), target)
	if result.Success || result.Status != "down" {
		t.Fatalf("result = %+v", result)
	}
	if result.FailedStep != "second" {
		t.Fatalf("failed step = %q", result.FailedStep)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("steps = %+v", result.Steps)
	}
	if result.Steps[1].StatusCode != http.StatusInternalServerError {
		t.Fatalf("failed step = %+v", result.Steps[1])
	}
	if !strings.Contains(result.Error, `step "second"`) {
		t.Fatalf("error = %q", result.Error)
	}
	if hits != 2 {
		t.Fatalf("server hits = %d, want 2", hits)
	}
}

func TestDiscoverJourneyFromIntentManifest(t *testing.T) {
	store := memorystore.New()
	ctx := context.Background()
	_, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{Writes: []guardianapi.PathWrite{
		{
			LogicalPath: paths.PartitionConfig("demo"),
			Content: []byte(`apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  defaults:
    targetPusher: k8s-main
    probes:
      enabled: true
      interval: 30s
      timeout: 2s
`),
		},
		{
			LogicalPath: paths.IntentManifest("demo", "api"),
			Content: []byte(`apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: k8s-main
  assets:
    - type: Compute
      name: app
      version: "1"
      properties:
        image: example/app:latest
    - type: Probe
      name: flow
      version: "1"
      properties:
        target: app
        headers:
          X-Api: demo
        steps:
          - name: read
            method: GET
            path: /foo/bar
            capture:
              foo: json:$
          - name: update
            method: PUT
            path: /foo/bar/${foo.id}
            body:
              status: modified
`),
		},
		{
			LogicalPath: paths.IntentState("demo", "api"),
			Content:     []byte(`{"outputs":{"app.address":"127.0.0.1:18080"}}`),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	runner := New(Config{Pusher: "k8s-main", Enabled: true, Interval: 30 * time.Second, Timeout: 2 * time.Second, Store: store})
	targets, err := runner.discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %#v", targets)
	}
	target := targets[0]
	if target.Type != "journey" || target.Asset != "app" || target.Retries != 0 {
		t.Fatalf("target = %+v", target)
	}
	if len(target.Steps) != 2 {
		t.Fatalf("steps = %+v", target.Steps)
	}
	if target.Steps[0].Method != "GET" || target.Steps[1].Method != "PUT" {
		t.Fatalf("methods = %q/%q", target.Steps[0].Method, target.Steps[1].Method)
	}
	if target.Steps[1].Path != "/foo/bar/${foo.id}" {
		t.Fatalf("path = %q", target.Steps[1].Path)
	}
	body, ok := target.Steps[1].Body.(map[string]any)
	if !ok || body["status"] != "modified" {
		t.Fatalf("body = %#v", target.Steps[1].Body)
	}
	if target.Steps[0].Capture["foo"] != "json:$" {
		t.Fatalf("capture = %#v", target.Steps[0].Capture)
	}
	if target.Steps[0].Headers["X-Api"] != "demo" {
		t.Fatalf("inherited headers = %#v", target.Steps[0].Headers)
	}
}

func TestRunnerExecutesJourneyAndWritesSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /foo/bar":
			_, _ = w.Write([]byte(`{"id":"42","name":"widget"}`))
		case "PUT /foo/bar/42":
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil || payload["status"] != "modified" {
				t.Errorf("update body = %s (%v)", body, err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := memorystore.New()
	ctx := context.Background()
	_, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{Writes: []guardianapi.PathWrite{
		{
			LogicalPath: paths.PartitionConfig("demo"),
			Content: []byte(`apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  defaults:
    targetPusher: k8s-main
    probes:
      enabled: true
      interval: 30s
      timeout: 2s
`),
		},
		{
			LogicalPath: paths.IntentManifest("demo", "api"),
			Content: []byte(`apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: k8s-main
  assets:
    - type: Compute
      name: app
      version: "1"
      properties:
        image: example/app:latest
    - type: Probe
      name: flow
      version: "1"
      properties:
        target: app
        steps:
          - name: read
            method: GET
            path: /foo/bar
            capture:
              foo: json:$
          - name: update
            method: PUT
            path: /foo/bar/${foo.id}
            body:
              id: ${foo.id}
              status: modified
            expectStatus: [204]
`),
		},
		{
			LogicalPath: paths.IntentState("demo", "api"),
			Content:     []byte(`{"outputs":{"app.address":"` + strings.TrimPrefix(server.URL, "http://") + `"}}`),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	runner := New(Config{
		Enabled:     true,
		Pusher:      "k8s-main",
		Interval:    time.Second,
		Timeout:     time.Second,
		Concurrency: 1,
		Store:       store,
	})
	runner.refresh(ctx)
	runner.runDue(ctx)

	latest := runner.Latest()
	if len(latest) != 1 {
		t.Fatalf("latest = %+v", latest)
	}
	if !latest[0].Success || len(latest[0].Steps) != 2 {
		t.Fatalf("result = %+v", latest[0])
	}
	snapshot := readSnapshot(t, store)
	if len(snapshot.Probes) != 1 || len(snapshot.Probes[0].Steps) != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestProbeJourneyGRPCHealthAndReflection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	reflection.Register(server)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	target := Target{
		ID:        "journey-grpc",
		Partition: "demo",
		Intent:    "api",
		Asset:     "grpc",
		Name:      "health",
		Type:      "journey",
		Address:   listener.Addr().String(),
		Timeout:   2 * time.Second,
		Steps: []Step{
			{
				Name:    "health-check",
				Type:    "grpc",
				Capture: map[string]string{"health": "json:$.status"},
			},
			{
				Name:       "reflection-health",
				Type:       "grpc",
				Service:    "grpc.health.v1.Health",
				GRPCMethod: "Check",
				Request:    map[string]any{"service": ""},
				Capture:    map[string]string{"reflected": "json:$.status"},
			},
		},
	}
	result := New(Config{Pusher: "k8s-main", Timeout: 2 * time.Second}).execute(context.Background(), target)
	if !result.Success {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("steps = %+v", result.Steps)
	}
	for _, step := range result.Steps {
		if !step.Success {
			t.Fatalf("step = %+v", step)
		}
	}
}

func TestProbeGRPCHealthSingleRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	target := Target{
		ID:        "grpc-health",
		Partition: "demo",
		Intent:    "api",
		Asset:     "grpc",
		Name:      "health",
		Type:      "grpc",
		Address:   listener.Addr().String(),
		Timeout:   2 * time.Second,
	}
	result := New(Config{Pusher: "k8s-main", Timeout: 2 * time.Second}).execute(context.Background(), target)
	if !result.Success || result.Status != "up" {
		t.Fatalf("result = %+v", result)
	}
}

func TestResolveHTTPURLBareHost(t *testing.T) {
	vars := newJourneyVars(Target{})
	got, err := resolveHTTPURL(Target{}, Step{Type: "http", URL: "example.com:8080", Path: "/foo"}, vars)
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://example.com:8080/foo" {
		t.Fatalf("url = %q", got)
	}
}

func TestJourneyCaptureAndInterpolation(t *testing.T) {
	vars := newJourneyVars(Target{Type: "http", Address: "api:8080", Port: 8080, Name: "api"})
	body := []byte(`{"id":"42","nested":{"token":"abc"},"items":[{"name":"widget"}]}`)
	headers := http.Header{"X-Trace": []string{"trace-1"}}
	captured, err := captureJourneyValues(map[string]string{
		"foo":   "json:$",
		"id":    "json:$.id",
		"token": "json:$.nested.token",
		"name":  "json:$.items[0].name",
		"trace": "header:X-Trace",
		"code":  "status",
	}, body, headerGetter(headers), http.StatusOK, vars)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(captured, ",") != "code,foo,id,name,token,trace" {
		t.Fatalf("captured = %#v", captured)
	}
	expanded, err := vars.expandString("id=${foo.id} name=${name} token=${token} trace=${trace} address=${target.address} code=${code}")
	if err != nil {
		t.Fatal(err)
	}
	want := "id=42 name=widget token=abc trace=trace-1 address=api:8080 code=200"
	if expanded != want {
		t.Fatalf("expanded = %q, want %q", expanded, want)
	}
	value, err := vars.expandValue(map[string]any{"payload": "${foo}"})
	if err != nil {
		t.Fatal(err)
	}
	payload := value.(map[string]any)["payload"].(map[string]any)
	if payload["id"] != "42" {
		t.Fatalf("typed interpolation = %#v", payload)
	}
}

func TestJourneyUnknownVariableFails(t *testing.T) {
	vars := newJourneyVars(Target{})
	if _, err := vars.expandString("${missing}"); err == nil {
		t.Fatal("expected unknown variable error")
	}
}
