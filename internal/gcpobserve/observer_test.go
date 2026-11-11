package gcpobserve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/storage/v1/b/state-bucket/o", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("prefix") != "guardian/" {
			t.Errorf("prefix = %q", r.URL.Query().Get("prefix"))
		}
		_, _ = w.Write([]byte(`{"items":[
			{"name":"guardian/demo/api/default.tfstate","updated":"2026-11-06T10:00:00Z","size":"1200"},
			{"name":"guardian/demo/worker/default.tfstate","updated":"2026-11-06T10:05:00Z","size":"900"},
			{"name":"guardian/other.txt","updated":"2026-11-06T10:05:00Z"}
		]}`))
	})
	mux.HandleFunc("/v1/projects/demo:searchAllResources", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[
			{"name":"//run.googleapis.com/projects/demo/locations/europe-west1/services/guardian-demo-api-app","assetType":"run.googleapis.com/Service","location":"europe-west1","displayName":"guardian-demo-api-app"},
			{"name":"//storage.googleapis.com/guardian-demo-api-media","assetType":"storage.googleapis.com/Bucket","location":"EU","displayName":"guardian-demo-api-media"},
			{"name":"//storage.googleapis.com/guardian-demo-worker-data","assetType":"storage.googleapis.com/Bucket","location":"EU","displayName":"guardian-demo-worker-data"}
		]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestRefreshMatchesResources(t *testing.T) {
	server := testServer(t)
	refresher := &Refresher{
		StateBucket: "state-bucket",
		Region:      "europe-west1",
		HTTPClient:  server.Client(),
		StorageURL:  server.URL,
		AssetURL:    server.URL,
	}
	stacks, err := refresher.Refresh(context.Background(), "demo", "europe-west1", nil)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(stacks) != 2 {
		t.Fatalf("stacks = %+v", stacks)
	}
	if stacks[0].StackName != "demo/api" || stacks[0].Project != "demo" {
		t.Errorf("stack = %+v", stacks[0])
	}
	if len(stacks[0].Resources) != 2 {
		t.Errorf("api resources = %+v", stacks[0].Resources)
	}
	if len(stacks[1].Resources) != 1 {
		t.Errorf("worker resources = %+v", stacks[1].Resources)
	}
	if stacks[0].Status != "StatePresent" || stacks[0].Source != "gcs-state" {
		t.Errorf("status/source = %q/%q", stacks[0].Status, stacks[0].Source)
	}
}

func TestRefreshFiltersStackNames(t *testing.T) {
	server := testServer(t)
	refresher := &Refresher{
		StateBucket: "state-bucket",
		Region:      "europe-west1",
		HTTPClient:  server.Client(),
		StorageURL:  server.URL,
		AssetURL:    server.URL,
	}
	stacks, err := refresher.Refresh(context.Background(), "demo", "europe-west1", []string{"demo/worker"})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(stacks) != 1 || stacks[0].StackName != "demo/worker" {
		t.Fatalf("stacks = %+v", stacks)
	}
}

func TestServiceCacheAndResources(t *testing.T) {
	server := testServer(t)
	service := NewService(NewCache(), &Refresher{
		StateBucket: "state-bucket",
		Region:      "europe-west1",
		HTTPClient:  server.Client(),
		StorageURL:  server.URL,
		AssetURL:    server.URL,
	})
	if _, err := service.Refresh(context.Background(), "demo", "europe-west1", nil); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := service.List("demo", "europe-west1"); len(got) != 2 {
		t.Fatalf("List = %+v", got)
	}
	if got := service.List("demo", "us-central1"); len(got) != 0 {
		t.Fatalf("region filter = %+v", got)
	}
	stack, ok := service.Get("demo", "", "demo/api")
	if !ok || stack.StackName != "demo/api" {
		t.Fatalf("Get = %+v %v", stack, ok)
	}
	resources, err := service.StackResources(context.Background(), "demo", "europe-west1", "demo/api")
	if err != nil {
		t.Fatalf("StackResources: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("resources = %+v", resources)
	}
}

func TestParseStateName(t *testing.T) {
	partition, intent, ok := parseStateName("guardian/demo/api/default.tfstate")
	if !ok || partition != "demo" || intent != "api" {
		t.Fatalf("parse = %q %q %v", partition, intent, ok)
	}
	// Per-asset state objects share the partition/intent prefix.
	partition, intent, ok = parseStateName("guardian/demo/api/service/default.tfstate")
	if !ok || partition != "demo" || intent != "api" {
		t.Fatalf("asset parse = %q %q %v", partition, intent, ok)
	}
	for _, bad := range []string{"guardian/demo/default.tfstate", "other/demo/api/default.tfstate", "guardian/a/b/c/d/default.tfstate"} {
		if _, _, ok := parseStateName(bad); ok {
			t.Errorf("expected %q to be rejected", bad)
		}
	}
}

func TestRefreshTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	refresher := &Refresher{
		StateBucket: "state-bucket",
		HTTPClient:  server.Client(),
		StorageURL:  server.URL,
		AssetURL:    server.URL,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := refresher.Refresh(ctx, "demo", "", nil); err == nil {
		t.Fatal("expected a context deadline error")
	}
}

func TestSanitizeNameMatchesDriverNaming(t *testing.T) {
	t.Parallel()
	if got, want := sanitizeName("Diva_Shared.App"), "diva-shared-app"; got != want {
		t.Fatalf("sanitizeName = %q, want %q", got, want)
	}
}

func TestMatchesAnyResourcePrefixIgnoresNumericHashSuffix(t *testing.T) {
	t.Parallel()
	resource := StackResource{
		Name:        "//run.googleapis.com/projects/p/locations/europe-west1/revisions/guardian-diva-shared-gcp-app-00002-876",
		DisplayName: "guardian-diva-shared-gcp-app-00002-876",
	}
	if !matchesAnyResourcePrefix(resource, []string{"guardian-diva-shared-gcp-"}) {
		t.Fatal("expected revision to match the intent prefix")
	}
	if matchesAnyResourcePrefix(resource, []string{"guardian-other-gcp-"}) {
		t.Fatal("unexpected match for a different intent prefix")
	}
}

func TestRefreshKeepsResourcesWhenCloudAssetInventoryFails(t *testing.T) {
	var failAssets atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/storage/v1/b/state-bucket/o", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"name":"guardian/demo/api/default.tfstate","updated":"2026-11-06T10:00:00Z"}]}`))
	})
	mux.HandleFunc("/v1/projects/demo:searchAllResources", func(w http.ResponseWriter, r *http.Request) {
		if failAssets.Load() {
			http.Error(w, "quota exceeded", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"name":"//run.googleapis.com/projects/demo/locations/europe-west1/services/guardian-demo-api-app","assetType":"run.googleapis.com/Service","location":"europe-west1","displayName":"guardian-demo-api-app"}]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	service := NewService(NewCache(), &Refresher{
		StateBucket: "state-bucket",
		Region:      "europe-west1",
		HTTPClient:  server.Client(),
		StorageURL:  server.URL,
		AssetURL:    server.URL,
	})
	if _, err := service.Refresh(context.Background(), "demo", "europe-west1", nil); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}
	failAssets.Store(true)
	if _, err := service.Refresh(context.Background(), "demo", "europe-west1", nil); err != nil {
		t.Fatalf("refresh with CAL failure: %v", err)
	}
	stacks := service.List("demo", "europe-west1")
	if len(stacks) != 1 || len(stacks[0].Resources) != 1 {
		t.Fatalf("resources were cleared after a CAL failure: %+v", stacks)
	}
}

func TestPutSnapshotEvictsDeletedStacks(t *testing.T) {
	cache := NewCache()
	cache.PutSnapshot("demo", []StackState{{Project: "demo", StackName: "demo/api", Partition: "demo", Intent: "api"}})
	cache.PutSnapshot("demo", []StackState{{Project: "demo", StackName: "demo/worker", Partition: "demo", Intent: "worker"}})
	if stacks := cache.List("demo", ""); len(stacks) != 1 || stacks[0].StackName != "demo/worker" {
		t.Fatalf("stale stack was not evicted: %+v", stacks)
	}
}
