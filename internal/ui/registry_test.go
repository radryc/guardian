package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func newRegistryTestServer(t *testing.T) (*Server, guardianapi.Store) {
	t.Helper()
	store := memory.New()
	srv, err := New(Options{Store: store, PrincipalID: "test-ui"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return srv, store
}

func registryRequest(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestRegistryAPIStoresPusherAndRepo(t *testing.T) {
	srv, _ := newRegistryTestServer(t)

	rec := registryRequest(t, srv, http.MethodPost, "/api/pushers", map[string]any{
		"name":           "aws-123456789012",
		"account":        "123456789012",
		"region":         "eu-west-1",
		"observedApiUrl": "http://guardian-awsread:19090",
		"tokenEnv":       "GUARDIAN_AWSREAD_TOKEN",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST pusher status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = registryRequest(t, srv, http.MethodGet, "/api/pushers", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "aws-123456789012") {
		t.Fatalf("GET pushers status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = registryRequest(t, srv, http.MethodPost, "/api/repos", map[string]any{
		"name":      "strata-aws",
		"pusher":    "aws-123456789012",
		"partition": "strata-aws",
		"source":    "https://github.com/example/strata.git",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST repo status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = registryRequest(t, srv, http.MethodGet, "/api/repos", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "strata-aws") {
		t.Fatalf("GET repos status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = registryRequest(t, srv, http.MethodPost, "/api/repos", map[string]any{
		"name": "orphan", "pusher": "missing", "partition": "orphan",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST orphan repo status = %d, want 400", rec.Code)
	}
}

func TestRegistryAPIIngestFromStoreAssembly(t *testing.T) {
	ctx := context.Background()
	srv, store := newRegistryTestServer(t)

	assemblyDir := "/partitions/strata-aws/payloads/aws/cdk.out"
	seed := func(path, content string) {
		t.Helper()
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{LogicalPath: path, Content: []byte(content)}},
		}); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}
	seed(assemblyDir+"/manifest.json", `{"artifacts":{"StrataStack":{"type":"aws:cloudformation:stack","properties":{"templateFile":"StrataStack.template.json"}}}}`)
	seed(assemblyDir+"/StrataStack.template.json", `{"Resources":{}}`)

	for _, body := range []map[string]any{
		{"name": "aws-123456789012", "account": "123456789012", "region": "eu-west-1", "observedApiUrl": "http://x"},
		{"name": "strata-aws", "pusher": "aws-123456789012", "partition": "strata-aws", "source": "/partitions/strata-aws/payloads/aws/cdk.out"},
	} {
		path := "/api/pushers"
		if body["pusher"] != nil {
			path = "/api/repos"
		}
		if rec := registryRequest(t, srv, http.MethodPost, path, body); rec.Code != http.StatusOK {
			t.Fatalf("POST %s status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}

	rec := registryRequest(t, srv, http.MethodPost, "/api/repos/strata-aws/ingest", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST ingest status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "strata-stack") {
		t.Fatalf("ingest body missing intent: %s", rec.Body.String())
	}
	if _, err := store.ReadFile(ctx, "/partitions/strata-aws/intents/strata-stack.yaml"); err != nil {
		t.Fatalf("generated intent missing: %v", err)
	}

	rec = registryRequest(t, srv, http.MethodDelete, "/api/repos/strata-aws", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE repo status = %d", rec.Code)
	}
	rec = registryRequest(t, srv, http.MethodDelete, "/api/pushers/aws-123456789012", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE pusher status = %d", rec.Code)
	}
}

func TestRegistryAPIAssignBindingsAndMultiPartitionIngest(t *testing.T) {
	ctx := context.Background()
	srv, store := newRegistryTestServer(t)

	assemblyDir := "/repos/strata/cdk.out"
	seed := func(path, content string) {
		t.Helper()
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{LogicalPath: path, Content: []byte(content)}},
		}); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}
	seed(assemblyDir+"/manifest.json", `{"artifacts":{
		"ProdStack":{"type":"aws:cloudformation:stack","properties":{"templateFile":"ProdStack.template.json"}},
		"StgStack":{"type":"aws:cloudformation:stack","properties":{"templateFile":"StgStack.template.json"}}
	}}`)
	seed(assemblyDir+"/ProdStack.template.json", `{"Resources":{}}`)
	seed(assemblyDir+"/StgStack.template.json", `{"Resources":{}}`)

	for _, body := range []map[string]any{
		{"name": "aws-1", "account": "123456789012", "region": "eu-west-1", "observedApiUrl": "http://x", "token": "secret-token"},
		{"name": "strata", "pusher": "aws-1", "source": assemblyDir, "bindings": []map[string]any{
			{"partition": "prod", "selector": "Prod*"},
			{"partition": "staging", "selector": "Stg*"},
		}},
	} {
		path := "/api/pushers"
		if body["pusher"] != nil {
			path = "/api/repos"
		}
		if rec := registryRequest(t, srv, http.MethodPost, path, body); rec.Code != http.StatusOK {
			t.Fatalf("POST %s status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}

	rec := registryRequest(t, srv, http.MethodPost, "/api/repos/strata/ingest", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST ingest status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if _, err := store.ReadFile(ctx, "/partitions/prod/intents/prod-stack.yaml"); err != nil {
		t.Fatalf("prod intent missing: %v", err)
	}
	if _, err := store.ReadFile(ctx, "/partitions/staging/intents/stg-stack.yaml"); err != nil {
		t.Fatalf("staging intent missing: %v", err)
	}
	if _, err := store.ReadFile(ctx, "/partitions/prod/intents/stg-stack.yaml"); err == nil {
		t.Fatalf("selector leaked stg intent into prod")
	}

	rec = registryRequest(t, srv, http.MethodPut, "/api/repos/strata/bindings", map[string]any{
		"bindings": []map[string]any{{"partition": "prod", "selector": "Prod*"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT bindings status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = registryRequest(t, srv, http.MethodGet, "/api/repos/strata", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET repo status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, assemblyDir) {
		t.Fatalf("assignments wiped repo credentials/source: %s", body)
	}
	if strings.Contains(body, "staging") {
		t.Fatalf("removed binding still present: %s", body)
	}
}

func TestRegistryAPIImportsRepoPartitionWithoutAssembly(t *testing.T) {
	ctx := context.Background()
	srv, store := newRegistryTestServer(t)

	seed := func(path, content string) {
		t.Helper()
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{LogicalPath: path, Content: []byte(content)}},
		}); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}
	seed("/repos/strata/guardian/partitions/prod/partition.yaml", `
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: prod
spec:
  deletionPolicy: orphan
`)
	seed("/repos/strata/guardian/partitions/prod/intents/web.yaml", `
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: web
spec:
  targetPusher: aws-1
  target:
    cluster: local
  assets:
    - type: Config
      name: config
      version: v1
      properties:
        content: "key: value"
`)
	seed("/repos/strata/guardian/partitions/prod/payloads/config.txt", "hello")

	for _, body := range []map[string]any{
		{"name": "aws-1", "observedApiUrl": "http://x"},
		{"name": "strata", "pusher": "aws-1", "source": "/repos/strata", "bindings": []map[string]any{
			{"partition": "prod"},
		}},
	} {
		path := "/api/pushers"
		if body["pusher"] != nil {
			path = "/api/repos"
		}
		if rec := registryRequest(t, srv, http.MethodPost, path, body); rec.Code != http.StatusOK {
			t.Fatalf("POST %s status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}

	rec := registryRequest(t, srv, http.MethodPost, "/api/repos/strata/ingest", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST ingest status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if _, err := store.ReadFile(ctx, "/partitions/prod/intents/web.yaml"); err != nil {
		t.Fatalf("imported intent missing: %v", err)
	}
	if _, err := store.ReadFile(ctx, "/partitions/prod/payloads/config.txt"); err != nil {
		t.Fatalf("imported payload missing: %v", err)
	}
	if _, err := store.ReadFile(ctx, "/partitions/prod/config.yaml"); err != nil {
		t.Fatalf("imported config missing: %v", err)
	}
}

func TestRegistryAPINotifyRecordsAndIngests(t *testing.T) {
	ctx := context.Background()
	srv, store := newRegistryTestServer(t)

	assemblyDir := "/partitions/strata-aws/payloads/aws/cdk.out"
	seed := func(path, content string) {
		t.Helper()
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{LogicalPath: path, Content: []byte(content)}},
		}); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}
	seed(assemblyDir+"/manifest.json", `{"artifacts":{"StrataStack":{"type":"aws:cloudformation:stack","properties":{"templateFile":"StrataStack.template.json"}}}}`)
	seed(assemblyDir+"/StrataStack.template.json", `{"Resources":{}}`)

	for _, body := range []map[string]any{
		{"name": "aws-123456789012", "account": "123456789012", "region": "eu-west-1", "observedApiUrl": "http://x"},
		{"name": "strata-aws", "pusher": "aws-123456789012", "partition": "strata-aws", "source": assemblyDir},
	} {
		path := "/api/pushers"
		if body["pusher"] != nil {
			path = "/api/repos"
		}
		if rec := registryRequest(t, srv, http.MethodPost, path, body); rec.Code != http.StatusOK {
			t.Fatalf("POST %s status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}

	rec := registryRequest(t, srv, http.MethodPost, "/api/repos/strata-aws/notify", map[string]any{"commit": "deadbeef", "branch": "main"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST notify status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ingested") {
		t.Fatalf("notify body = %s", rec.Body.String())
	}

	rec = registryRequest(t, srv, http.MethodGet, "/api/repos/strata-aws", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "deadbeef") {
		t.Fatalf("repo record missing notification: %s", rec.Body.String())
	}
}

func TestRegistryAPIRegistersRepoBeforePusherAndBindings(t *testing.T) {
	srv, _ := newRegistryTestServer(t)

	rec := registryRequest(t, srv, http.MethodPost, "/api/repos", map[string]any{
		"name":       "source-only",
		"source":     "https://github.com/example/source.git",
		"sourceType": "git",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST standalone repo status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = registryRequest(t, srv, http.MethodPost, "/api/repos", map[string]any{
		"name": "monofs-source", "source": "/.repos/github.com/example/source", "sourceType": "monofs",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST monofs repo status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = registryRequest(t, srv, http.MethodPost, "/api/repos", map[string]any{
		"name": "bad-monofs", "source": "github.com/example/source", "sourceType": "monofs",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST invalid monofs repo status = %d, want 400", rec.Code)
	}

	rec = registryRequest(t, srv, http.MethodPost, "/api/repos/source-only/ingest", nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "no pusher") {
		t.Fatalf("ingest before pusher = %d %s", rec.Code, rec.Body.String())
	}

	rec = registryRequest(t, srv, http.MethodPost, "/api/pushers", map[string]any{
		"name": "aws-1", "account": "123456789012", "region": "eu-west-1", "observedApiUrl": "http://x",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST pusher status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = registryRequest(t, srv, http.MethodPut, "/api/repos/source-only/bindings", map[string]any{
		"pusher":   "aws-1",
		"bindings": []map[string]any{{"partition": "prod", "selector": "Prod-*"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT late bindings status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = registryRequest(t, srv, http.MethodGet, "/api/repos/source-only", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET repo status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"source-only", "aws-1", "prod", "Prod-*", "sourceType"} {
		if !strings.Contains(body, want) {
			t.Errorf("repo body missing %q: %s", want, body)
		}
	}
}
