package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/buildinfo"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	partitiondomain "github.com/rydzu/ainfra/guardian/internal/domain/partition"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

func TestServerSaveBundleAndReadBack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	srv, err := New(Options{
		Store:       store,
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	req := SaveBundleRequest{
		Partition: newPartition("demo"),
		Intents: []SaveIntentRequest{{
			Manifest: newConfigIntent("web", "app-config", "hello from guardian"),
		}},
		RemoveMissingIntents: true,
	}

	var saveResp SaveBundleResponse
	requestJSON(t, httpSrv, http.MethodPut, "/api/partitions/demo/bundle", req, &saveResp)
	if !saveResp.Success {
		t.Fatalf("expected save success")
	}
	if saveResp.BatchRevisionID == "" || saveResp.PartitionVersionID == "" {
		t.Fatalf("expected returned version metadata, got %+v", saveResp)
	}
	if saveResp.IntentVersionIDs["web"] == "" {
		t.Fatalf("expected version for saved intent, got %+v", saveResp.IntentVersionIDs)
	}

	partitionContent, err := store.ReadFile(ctx, paths.PartitionConfig("demo"))
	if err != nil {
		t.Fatalf("read partition config: %v", err)
	}
	if !strings.Contains(string(partitionContent), "name: demo") {
		t.Fatalf("expected persisted partition yaml, got %q", string(partitionContent))
	}

	intentContent, err := store.ReadFile(ctx, paths.IntentManifest("demo", "web"))
	if err != nil {
		t.Fatalf("read intent manifest: %v", err)
	}
	if !strings.Contains(string(intentContent), "name: app-config") {
		t.Fatalf("expected persisted asset yaml, got %q", string(intentContent))
	}

	var detail PartitionDetailResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo", nil, &detail)
	if detail.Partition.Manifest.Metadata.Name != "demo" {
		t.Fatalf("unexpected partition name: %q", detail.Partition.Manifest.Metadata.Name)
	}
	if len(detail.Intents) != 1 || detail.Intents[0].Manifest.Metadata.Name != "web" {
		t.Fatalf("unexpected intents: %+v", detail.Intents)
	}
	if len(detail.Intents[0].Assets) != 1 {
		t.Fatalf("expected one asset in intent, got %+v", detail.Intents[0].Assets)
	}
	if asset := detail.Intents[0].Assets[0]; asset.Name != "app-config" || asset.Type != assetdomain.TypeConfig {
		t.Fatalf("unexpected asset details: %+v", asset)
	}
	if len(detail.Topology.Nodes) < 3 {
		t.Fatalf("expected partition, intent, and asset nodes, got %+v", detail.Topology.Nodes)
	}

	var overview OverviewResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/overview", nil, &overview)
	if len(overview.Partitions) != 1 || overview.Partitions[0].Name != "demo" {
		t.Fatalf("unexpected overview partitions: %+v", overview.Partitions)
	}
	if overview.Summary.Partitions != 1 || overview.Summary.Intents != 1 || overview.Summary.Assets != 1 {
		t.Fatalf("unexpected overview summary: %+v", overview.Summary)
	}
}

func TestServerSaveBundleRemovesMissingIntents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	srv, err := New(Options{
		Store:       store,
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	requestJSON(t, httpSrv, http.MethodPut, "/api/partitions/demo/bundle", SaveBundleRequest{
		Partition: newPartition("demo"),
		Intents: []SaveIntentRequest{
			{Manifest: newConfigIntent("web", "web-config", "web")},
			{Manifest: newConfigIntent("jobs", "jobs-config", "jobs")},
		},
		RemoveMissingIntents: true,
	}, &SaveBundleResponse{})

	var saveResp SaveBundleResponse
	requestJSON(t, httpSrv, http.MethodPut, "/api/partitions/demo/bundle", SaveBundleRequest{
		Partition: newPartition("demo"),
		Intents: []SaveIntentRequest{
			{Manifest: newConfigIntent("web", "web-config", "web")},
		},
		RemoveMissingIntents: true,
	}, &saveResp)

	if !slices.Equal(saveResp.RemovedIntents, []string{"jobs"}) {
		t.Fatalf("expected removed intent list, got %+v", saveResp.RemovedIntents)
	}
	if _, err := store.ReadFile(ctx, paths.IntentManifest("demo", "jobs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected removed intent to be gone, got err=%v", err)
	}

	var detail PartitionDetailResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo", nil, &detail)
	if len(detail.Intents) != 1 || detail.Intents[0].Manifest.Metadata.Name != "web" {
		t.Fatalf("unexpected remaining intents: %+v", detail.Intents)
	}
}

func TestServerIndexIncludesTopologyZoomControls(t *testing.T) {
	t.Parallel()

	srv, err := New(Options{
		Store:       memory.New(),
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("index status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, needle := range []string{"topologyZoomOut", "topologyZoomValue", "topologyZoomIn", "attentionAssetsList", "topologyCanvas", "historyGroupToggle"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected index body to contain %q", needle)
		}
	}
}

func TestServerClientConfigReturnsMonoFSDiscovery(t *testing.T) {
	t.Parallel()

	srv, err := New(Options{
		Store:       memory.New(),
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
		ClientConfig: ClientConfig{MonoFS: &MonoFSClientConfig{
			RouterAddr:           "localhost:9090",
			Token:                "guardian-dev-token",
			UseExternalAddresses: true,
		}},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var cfg ClientConfig
	requestJSON(t, httpSrv, http.MethodGet, "/api/client-config", nil, &cfg)
	if cfg.MonoFS == nil {
		t.Fatalf("expected MonoFS discovery config")
	}
	if cfg.MonoFS.RouterAddr != "localhost:9090" || cfg.MonoFS.Token != "guardian-dev-token" || !cfg.MonoFS.UseExternalAddresses {
		t.Fatalf("unexpected client config: %+v", cfg.MonoFS)
	}
}

func TestServerClientConfigRejectsNonLoopback(t *testing.T) {
	t.Parallel()

	srv, err := New(Options{
		Store:       memory.New(),
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
		ClientConfig: ClientConfig{MonoFS: &MonoFSClientConfig{
			RouterAddr: "localhost:9090",
			Token:      "guardian-dev-token",
		}},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/client-config", nil)
	req.RemoteAddr = "10.0.0.8:12345"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestServerClientConfigAllowsMatchingDiscoveryToken(t *testing.T) {
	t.Parallel()

	srv, err := New(Options{
		Store:       memory.New(),
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
		ClientConfig: ClientConfig{MonoFS: &MonoFSClientConfig{
			RouterAddr: "router.example.com:9090",
			Token:      "guardian-dev-token",
		}},
		ClientConfigToken: "shared-token",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/client-config", nil)
	req.RemoteAddr = "10.0.0.8:12345"
	req.Header.Set("X-Guardian-Discovery-Token", "shared-token")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestServerBuildInfoReturnsStampedRelease(t *testing.T) {
	oldVersion, oldCommit, oldBuildTime := buildinfo.Version, buildinfo.Commit, buildinfo.BuildTime
	buildinfo.Version = "20260511-1200"
	buildinfo.Commit = "0123456789abcdef"
	buildinfo.BuildTime = "2026-05-11T12:00:00Z"
	t.Cleanup(func() {
		buildinfo.Version = oldVersion
		buildinfo.Commit = oldCommit
		buildinfo.BuildTime = oldBuildTime
	})

	srv, err := New(Options{
		Store:       memory.New(),
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var resp struct {
		Status string            `json:"status"`
		Data   map[string]string `json:"data"`
	}
	requestJSON(t, httpSrv, http.MethodGet, "/api/v1/status/buildinfo", nil, &resp)
	if resp.Status != "success" {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if got := resp.Data["version"]; got != "20260511-1200" {
		t.Fatalf("version = %q, want 20260511-1200", got)
	}
	if got := resp.Data["revision"]; got != "0123456789abcdef" {
		t.Fatalf("revision = %q, want 0123456789abcdef", got)
	}
	if got := resp.Data["buildDate"]; got != "2026-05-11T12:00:00Z" {
		t.Fatalf("buildDate = %q, want 2026-05-11T12:00:00Z", got)
	}
}

func TestDeriveIntentPresentationUsesObservedHealth(t *testing.T) {
	t.Parallel()

	status, displayStatus, health, summary := deriveIntentPresentation(&statedomain.IntentState{
		Status: statedomain.StatusHealthy,
		Health: &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "web: kubernetes deployment has pods in CrashLoopBackOff"},
	}, intentTaskRuntime{})

	if status != string(statedomain.StatusHealthy) {
		t.Fatalf("status = %q, want %q", status, statedomain.StatusHealthy)
	}
	if displayStatus != "Unhealthy" || health != "failing" {
		t.Fatalf("presentation = (%q, %q), want (Unhealthy, failing)", displayStatus, health)
	}
	if summary != "web: kubernetes deployment has pods in CrashLoopBackOff" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestDeriveAssetPresentationUsesAssetObservation(t *testing.T) {
	t.Parallel()

	status, displayStatus, health, summary := deriveAssetPresentation(&statedomain.IntentState{
		Status: statedomain.StatusHealthy,
		AssetObservations: map[string]*taskdomain.AssetObservation{
			"web": {
				Health: &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "docker container docker-ct-main-demo-stack-web-0 is not running"},
			},
		},
	}, "web", intentTaskRuntime{})

	if status != string(statedomain.StatusHealthy) {
		t.Fatalf("status = %q, want %q", status, statedomain.StatusHealthy)
	}
	if displayStatus != "Unhealthy" || health != "failing" {
		t.Fatalf("presentation = (%q, %q), want (Unhealthy, failing)", displayStatus, health)
	}
	if summary != "docker container docker-ct-main-demo-stack-web-0 is not running" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestDeriveAssetPresentationDriftedLockedIncludesObservedCause(t *testing.T) {
	t.Parallel()

	status, displayStatus, health, summary := deriveAssetPresentation(&statedomain.IntentState{
		Status: statedomain.StatusDriftedLocked,
		Drift:  &taskdomain.DriftReport{ChangedAssets: []string{"web"}},
		AssetObservations: map[string]*taskdomain.AssetObservation{
			"web": {
				Health: &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "kubernetes deployment missing"},
			},
		},
	}, "web", intentTaskRuntime{})

	if status != string(statedomain.StatusDriftedLocked) {
		t.Fatalf("status = %q, want %q", status, statedomain.StatusDriftedLocked)
	}
	if displayStatus != "Locked drift" || health != "attention" {
		t.Fatalf("presentation = (%q, %q), want (Locked drift, attention)", displayStatus, health)
	}
	if summary != "Drift exists but push is locked: kubernetes deployment missing" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestDeriveIntentPresentationDriftedLockedIncludesObservedCause(t *testing.T) {
	t.Parallel()

	status, displayStatus, health, summary := deriveIntentPresentation(&statedomain.IntentState{
		Status: statedomain.StatusDriftedLocked,
		Health: &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "fetcher-a: kubernetes deployment missing"},
	}, intentTaskRuntime{})

	if status != string(statedomain.StatusDriftedLocked) {
		t.Fatalf("status = %q, want %q", status, statedomain.StatusDriftedLocked)
	}
	if displayStatus != "Locked drift" || health != "attention" {
		t.Fatalf("presentation = (%q, %q), want (Locked drift, attention)", displayStatus, health)
	}
	if summary != "Drift detected but the intent is locked: fetcher-a: kubernetes deployment missing" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestBuildPartitionHealthUsesIntentHealth(t *testing.T) {
	t.Parallel()

	health := buildPartitionHealth([]IntentDocument{{
		Name:          "api",
		Health:        "failing",
		DisplayStatus: "Unhealthy",
		Summary:       "web: kubernetes deployment has pods in CrashLoopBackOff",
		Assets: []AssetDocument{{
			Name:          "web",
			Health:        "healthy",
			DisplayStatus: "No diff",
			Summary:       "Matches desired state",
		}},
	}}, nil)

	if health.Status != "failing" || health.DisplayStatus != "Needs action" {
		t.Fatalf("partition health = (%q, %q)", health.Status, health.DisplayStatus)
	}
	if health.Summary != "1 intent reports an unhealthy live state." {
		t.Fatalf("summary = %q", health.Summary)
	}
}

func TestServerOverviewCountsPrefixedYMLIntents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	seedRawFiles(t, ctx, store,
		guardianapi.PathWrite{LogicalPath: paths.PartitionConfig("demo"), Content: mustMarshalYAML(t, newPartition("demo"))},
		guardianapi.PathWrite{LogicalPath: "/partitions/demo/intents/01-web.yml", Content: mustMarshalYAML(t, newConfigIntent("web", "web-config", "web"))},
		guardianapi.PathWrite{LogicalPath: "/partitions/demo/intents/20-jobs.yml", Content: mustMarshalYAML(t, newConfigIntent("jobs", "jobs-config", "jobs"))},
	)

	srv, err := New(Options{
		Store:       store,
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var overview OverviewResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/overview", nil, &overview)
	if len(overview.Partitions) != 1 {
		t.Fatalf("unexpected overview partitions: %+v", overview.Partitions)
	}
	if got, want := overview.Partitions[0].IntentCount, 2; got != want {
		t.Fatalf("IntentCount = %d, want %d", got, want)
	}
	if got, want := overview.Partitions[0].AssetCount, 2; got != want {
		t.Fatalf("AssetCount = %d, want %d", got, want)
	}
	if got, want := overview.Summary.Intents, 2; got != want {
		t.Fatalf("overview summary intents = %d, want %d", got, want)
	}
	if got, want := overview.Summary.Assets, 2; got != want {
		t.Fatalf("overview summary assets = %d, want %d", got, want)
	}

	var detail PartitionDetailResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo", nil, &detail)
	if len(detail.Intents) != 2 {
		t.Fatalf("expected 2 intents in detail, got %+v", detail.Intents)
	}
	names := []string{detail.Intents[0].Name, detail.Intents[1].Name}
	slices.Sort(names)
	if !slices.Equal(names, []string{"jobs", "web"}) {
		t.Fatalf("unexpected intent names: %v", names)
	}
}

func TestServerOverviewUsesHealthyAssetCountsForConfigOnlyPartitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	seedRawFiles(t, ctx, store,
		guardianapi.PathWrite{LogicalPath: paths.PartitionConfig("demo"), Content: mustMarshalYAML(t, newPartition("demo"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentManifest("demo", "web"), Content: mustMarshalYAML(t, newConfigIntent("web", "web-config", "web"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentState("demo", "web"), Content: mustMarshalJSON(t, statedomain.IntentState{
			APIVersion: "guardian/v1alpha1",
			Kind:       "IntentState",
			Partition:  "demo",
			Intent:     "web",
			Status:     statedomain.StatusHealthy,
		})},
	)

	srv, err := New(Options{
		Store:       store,
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var overview OverviewResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/overview", nil, &overview)
	if got, want := overview.Summary.HealthyAssets, 1; got != want {
		t.Fatalf("overview summary healthyAssets = %d, want %d", got, want)
	}
	if got, want := overview.Summary.AttentionAssets, 0; got != want {
		t.Fatalf("overview summary attentionAssets = %d, want %d", got, want)
	}
	if got, want := overview.Summary.FailingAssets, 0; got != want {
		t.Fatalf("overview summary failingAssets = %d, want %d", got, want)
	}
	if got, want := overview.Partitions[0].HealthyAssets, 1; got != want {
		t.Fatalf("partition healthyAssets = %d, want %d", got, want)
	}
	if got, want := overview.Partitions[0].ServicesHealthy, 0; got != want {
		t.Fatalf("partition servicesHealthy = %d, want %d", got, want)
	}

	var detail PartitionDetailResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo", nil, &detail)
	if got, want := detail.Health.Healthy, 1; got != want {
		t.Fatalf("detail health healthy = %d, want %d", got, want)
	}
	if got, want := detail.Health.Summary, "1 asset matches desired state."; got != want {
		t.Fatalf("detail health summary = %q, want %q", got, want)
	}
	if got, want := len(detail.Health.Services), 0; got != want {
		t.Fatalf("detail health services = %d, want %d", got, want)
	}
}

func TestServerOverviewIgnoresBrokenArchiveReads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	seedRawFiles(t, ctx, store,
		guardianapi.PathWrite{LogicalPath: paths.PartitionConfig("demo"), Content: mustMarshalYAML(t, newPartition("demo"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentManifest("demo", "web"), Content: mustMarshalYAML(t, newConfigIntent("web", "web-config", "web"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentState("demo", "web"), Content: mustMarshalJSON(t, statedomain.IntentState{
			APIVersion: "guardian/v1alpha1",
			Kind:       "IntentState",
			Partition:  "demo",
			Intent:     "web",
			Status:     statedomain.StatusHealthy,
		})},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveState("demo", "web", "dep-bad"), Content: []byte("{not-json")},
	)

	srv, err := New(Options{
		Store:       store,
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var overview OverviewResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/overview", nil, &overview)
	if len(overview.Partitions) != 1 {
		t.Fatalf("unexpected overview partitions: %+v", overview.Partitions)
	}
	if got, want := overview.Partitions[0].Health, "healthy"; got != want {
		t.Fatalf("overview partition health = %q, want %q", got, want)
	}
	if got, want := overview.Partitions[0].DisplayStatus, "Stable"; got != want {
		t.Fatalf("overview partition displayStatus = %q, want %q", got, want)
	}
	if len(overview.Partitions[0].Errors) != 0 {
		t.Fatalf("expected overview to avoid archive read errors, got %+v", overview.Partitions[0].Errors)
	}
}

func TestServerExposesAssetReleaseVersionsInDetailAndHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	intent := newConfigIntentWithVersion("web", "web-config", "web", "v1.2.3-abc123-20260429")
	deploymentRevision := "dep_release_1"
	seedRawFiles(t, ctx, store,
		guardianapi.PathWrite{LogicalPath: paths.PartitionConfig("demo"), Content: mustMarshalYAML(t, newPartition("demo"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentManifest("demo", "web"), Content: mustMarshalYAML(t, intent)},
		guardianapi.PathWrite{LogicalPath: paths.IntentState("demo", "web"), Content: mustMarshalJSON(t, statedomain.IntentState{
			APIVersion:         "guardian/v1alpha1",
			Kind:               "IntentState",
			Partition:          "demo",
			Intent:             "web",
			Status:             statedomain.StatusHealthy,
			IntentVersionID:    "intent-v1",
			IntentSpecHash:     "spec-v1",
			PartitionRevision:  "part-v1",
			DeploymentRevision: deploymentRevision,
			TargetPusher:       "local",
			Target:             targetdomain.Placement{Cluster: "local"},
			AssetVersionIDs:    map[string]string{"web-config": "asset-v1"},
			AssetVersions:      map[string]string{"web-config": "v1.2.3-abc123-20260429"},
			Outputs:            map[string]string{},
		})},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveState("demo", "web", deploymentRevision), Content: mustMarshalJSON(t, historyDeploymentRecordWithVersion(deploymentRevision))},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveManifest("demo", "web", deploymentRevision), Content: mustMarshalYAML(t, intent)},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveLogs("demo", "web", deploymentRevision), Content: []byte("")},
	)

	srv, err := New(Options{
		Store:       store,
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var detail PartitionDetailResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo", nil, &detail)
	if got, want := detail.Intents[0].Assets[0].Version, "v1.2.3-abc123-20260429"; got != want {
		t.Fatalf("asset version = %q, want %q", got, want)
	}
	if facts := detail.Intents[0].Assets[0].QuickFacts; len(facts) == 0 || facts[0].Label != "Release" || facts[0].Value != "v1.2.3-abc123-20260429" {
		t.Fatalf("expected release quick fact, got %+v", facts)
	}

	var history PartitionHistoryResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo/history", nil, &history)
	if got, want := history.Deployments[0].Assets[0].Version, "v1.2.3-abc123-20260429"; got != want {
		t.Fatalf("deployment asset version = %q, want %q", got, want)
	}
}

func TestServerHistoryAppliesLimitAndTimeframe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	webOld := historyDeploymentRecordWithVersion("dep_web_old")
	webOld.CreatedAt = time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	webNew := historyDeploymentRecordWithVersion("dep_web_new")
	webNew.CreatedAt = time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	jobsOld := historyDeploymentRecordWithVersion("dep_jobs_old")
	jobsOld.Intent = "jobs"
	jobsOld.CreatedAt = time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	jobsNew := historyDeploymentRecordWithVersion("dep_jobs_new")
	jobsNew.Intent = "jobs"
	jobsNew.CreatedAt = time.Date(2026, 4, 30, 18, 0, 0, 0, time.UTC)
	seedRawFiles(t, ctx, store,
		guardianapi.PathWrite{LogicalPath: paths.PartitionConfig("demo"), Content: mustMarshalYAML(t, newPartition("demo"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentManifest("demo", "web"), Content: mustMarshalYAML(t, newConfigIntent("web", "web-config", "web"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentManifest("demo", "jobs"), Content: mustMarshalYAML(t, newConfigIntent("jobs", "jobs-config", "jobs"))},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveState("demo", "web", webOld.DeploymentRevision), Content: mustMarshalJSON(t, webOld)},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveLogs("demo", "web", webOld.DeploymentRevision), Content: []byte("")},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveState("demo", "web", webNew.DeploymentRevision), Content: mustMarshalJSON(t, webNew)},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveLogs("demo", "web", webNew.DeploymentRevision), Content: []byte("")},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveState("demo", "jobs", jobsOld.DeploymentRevision), Content: mustMarshalJSON(t, jobsOld)},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveLogs("demo", "jobs", jobsOld.DeploymentRevision), Content: []byte("")},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveState("demo", "jobs", jobsNew.DeploymentRevision), Content: mustMarshalJSON(t, jobsNew)},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveLogs("demo", "jobs", jobsNew.DeploymentRevision), Content: []byte("")},
		guardianapi.PathWrite{LogicalPath: paths.EventState("demo", "event-old"), Content: mustMarshalJSON(t, historydomain.EventRecord{
			APIVersion: "guardian/v1alpha1",
			Kind:       "EventRecord",
			EventID:    "event-old",
			Partition:  "demo",
			Intent:     "web",
			Type:       "intent.state.updated",
			Message:    "old",
			CreatedAt:  time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
		})},
		guardianapi.PathWrite{LogicalPath: paths.EventState("demo", "event-new"), Content: mustMarshalJSON(t, historydomain.EventRecord{
			APIVersion: "guardian/v1alpha1",
			Kind:       "EventRecord",
			EventID:    "event-new",
			Partition:  "demo",
			Intent:     "jobs",
			Type:       "intent.state.updated",
			Message:    "new",
			CreatedAt:  time.Date(2026, 4, 30, 20, 0, 0, 0, time.UTC),
		})},
	)

	srv, err := New(Options{Store: store, PrincipalID: "test-ui", Pushers: []string{"local"}})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var history PartitionHistoryResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo/history?limit=1&since=2026-04-29T00:00:00Z", nil, &history)
	if got, want := len(history.Deployments), 2; got != want {
		t.Fatalf("deployment count = %d, want %d", got, want)
	}
	if got, want := history.Deployments[0].DeploymentRevision, "dep_jobs_new"; got != want {
		t.Fatalf("latest deployment = %q, want %q", got, want)
	}
	if got, want := history.Deployments[1].DeploymentRevision, "dep_web_new"; got != want {
		t.Fatalf("second deployment = %q, want %q", got, want)
	}
	if got, want := len(history.Events), 1; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	if got, want := history.Events[0].Message, "new"; got != want {
		t.Fatalf("event message = %q, want %q", got, want)
	}
}

func TestServerRolloutsExposeNewIntentAndChangedAssets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	apiOld := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_api_old",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-config-v1"},
		AssetVersions:      map[string]string{"config": "config-v1"},
		CreatedAt:          time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
	}
	apiNew := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_api_new",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-config-v2", "binary": "asset-binary-v1"},
		AssetVersions:      map[string]string{"config": "config-v2", "binary": "binary-v1"},
		CreatedAt:          time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	}
	jobsInitial := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_jobs_initial",
		Partition:          "demo",
		Intent:             "jobs",
		AssetVersionIDs:    map[string]string{"jobs-config": "asset-jobs-config-v1"},
		AssetVersions:      map[string]string{"jobs-config": "jobs-config-v1"},
		CreatedAt:          time.Date(2026, 4, 29, 15, 0, 0, 0, time.UTC),
	}
	seedRawFiles(t, ctx, store,
		guardianapi.PathWrite{LogicalPath: paths.PartitionConfig("demo"), Content: mustMarshalYAML(t, newPartition("demo"))},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveState("demo", "api", apiOld.DeploymentRevision), Content: mustMarshalJSON(t, apiOld)},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveManifest("demo", "api", apiOld.DeploymentRevision), Content: mustMarshalYAML(t, intentdomain.Intent{Metadata: intentdomain.Metadata{Name: "api"}, Spec: intentdomain.IntentSpec{TargetPusher: "local", Assets: []intentdomain.AssetSpec{{Type: assetdomain.TypeConfig, Name: "config", Version: "config-v1"}}}})},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveState("demo", "api", apiNew.DeploymentRevision), Content: mustMarshalJSON(t, apiNew)},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveManifest("demo", "api", apiNew.DeploymentRevision), Content: mustMarshalYAML(t, intentdomain.Intent{Metadata: intentdomain.Metadata{Name: "api"}, Spec: intentdomain.IntentSpec{TargetPusher: "local", Assets: []intentdomain.AssetSpec{{Type: assetdomain.TypeConfig, Name: "config", Version: "config-v2"}, {Type: assetdomain.TypeCompute, Name: "binary", Version: "binary-v1"}}}})},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveState("demo", "jobs", jobsInitial.DeploymentRevision), Content: mustMarshalJSON(t, jobsInitial)},
		guardianapi.PathWrite{LogicalPath: paths.ArchiveManifest("demo", "jobs", jobsInitial.DeploymentRevision), Content: mustMarshalYAML(t, intentdomain.Intent{Metadata: intentdomain.Metadata{Name: "jobs"}, Spec: intentdomain.IntentSpec{TargetPusher: "local", Assets: []intentdomain.AssetSpec{{Type: assetdomain.TypeConfig, Name: "jobs-config", Version: "jobs-config-v1"}}}})},
	)

	srv, err := New(Options{Store: store, PrincipalID: "test-ui", Pushers: []string{"local"}})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var response PartitionRolloutsResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo/rollouts", nil, &response)
	if got, want := len(response.Rollouts), 3; got != want {
		t.Fatalf("rollout count = %d, want %d", got, want)
	}
	if got, want := response.Rollouts[0].DeploymentRevision, "dep_api_new"; got != want {
		t.Fatalf("latest rollout deployment = %q, want %q", got, want)
	}
	if response.Rollouts[0].NewIntent {
		t.Fatalf("expected updated rollout to not be marked as new")
	}
	if got, want := response.Rollouts[0].Assets[0].Change, "added"; got != want {
		t.Fatalf("first rollout asset change = %q, want %q", got, want)
	}
	if response.Rollouts[0].SelfHealing {
		t.Fatalf("expected updated rollout to not be marked self-healing")
	}
	if !response.Rollouts[1].NewIntent {
		t.Fatalf("expected jobs rollout to be marked as new")
	}
}

func TestServerDetailLeavesRecentEventsToHistoryEndpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	seedRawFiles(t, ctx, store,
		guardianapi.PathWrite{LogicalPath: paths.PartitionConfig("demo"), Content: mustMarshalYAML(t, newPartition("demo"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentManifest("demo", "web"), Content: mustMarshalYAML(t, newConfigIntent("web", "web-config", "web"))},
		guardianapi.PathWrite{LogicalPath: paths.EventState("demo", "event-new"), Content: mustMarshalJSON(t, historydomain.EventRecord{
			APIVersion: "guardian/v1alpha1",
			Kind:       "EventRecord",
			EventID:    "event-new",
			Partition:  "demo",
			Intent:     "web",
			Type:       "intent.state.updated",
			Message:    "new",
			CreatedAt:  time.Date(2026, 4, 30, 20, 0, 0, 0, time.UTC),
		})},
	)

	srv, err := New(Options{Store: store, PrincipalID: "test-ui", Pushers: []string{"local"}})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var detail PartitionDetailResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo", nil, &detail)
	if got := len(detail.RecentEvents); got != 0 {
		t.Fatalf("detail recent events = %d, want 0", got)
	}

	var history PartitionHistoryResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo/history", nil, &history)
	if got, want := len(history.Events), 1; got != want {
		t.Fatalf("history events = %d, want %d", got, want)
	}
	if got, want := history.Events[0].Message, "new"; got != want {
		t.Fatalf("history event message = %q, want %q", got, want)
	}
}

func TestServerPartitionDetailCacheExpires(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	backingStore := memory.New()
	seedRawFiles(t, ctx, backingStore,
		guardianapi.PathWrite{LogicalPath: paths.PartitionConfig("demo"), Content: mustMarshalYAML(t, newPartition("demo"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentManifest("demo", "web"), Content: mustMarshalYAML(t, newConfigIntent("web", "web-config", "web"))},
	)
	store := newCountingStore(backingStore)

	srv, err := New(Options{
		Store:        store,
		PrincipalID:  "test-ui",
		Pushers:      []string{"local"},
		DataCacheTTL: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	if _, err := srv.loadPartitionDetail(ctx, "demo"); err != nil {
		t.Fatalf("first detail load: %v", err)
	}
	firstReads := store.totalReads()
	if firstReads == 0 {
		t.Fatalf("expected first detail load to hit the store")
	}

	if _, err := srv.loadPartitionDetail(ctx, "demo"); err != nil {
		t.Fatalf("second detail load: %v", err)
	}
	if got := store.totalReads(); got != firstReads {
		t.Fatalf("cached detail load changed read count from %d to %d", firstReads, got)
	}

	time.Sleep(35 * time.Millisecond)
	if _, err := srv.loadPartitionDetail(ctx, "demo"); err != nil {
		t.Fatalf("post-expiry detail load: %v", err)
	}
	if got := store.totalReads(); got <= firstReads {
		t.Fatalf("expected cache expiry to trigger more reads, got %d after %d", got, firstReads)
	}
}

func TestServerPartitionHealthIncludesNonServiceDrift(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	seedRawFiles(t, ctx, store,
		guardianapi.PathWrite{LogicalPath: paths.PartitionConfig("demo"), Content: mustMarshalYAML(t, newPartition("demo"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentManifest("demo", "web"), Content: mustMarshalYAML(t, newConfigIntent("web", "web-config", "web"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentState("demo", "web"), Content: mustMarshalJSON(t, statedomain.IntentState{
			APIVersion: "guardian/v1alpha1",
			Kind:       "IntentState",
			Partition:  "demo",
			Intent:     "web",
			Status:     statedomain.StatusDrifted,
			Drift: &taskdomain.DriftReport{
				Status:        "drifted",
				Summary:       "web-config changed",
				ChangedAssets: []string{"web-config"},
			},
		})},
	)

	srv, err := New(Options{
		Store:       store,
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var detail PartitionDetailResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo", nil, &detail)
	if got, want := detail.Health.Status, "attention"; got != want {
		t.Fatalf("detail health status = %q, want %q", got, want)
	}
	if got, want := detail.Health.Attention, 1; got != want {
		t.Fatalf("detail health attention = %d, want %d", got, want)
	}
	if got, want := detail.Health.Summary, "1 asset needs attention."; got != want {
		t.Fatalf("detail health summary = %q, want %q", got, want)
	}
}

func TestServerMarksStaleActiveTaskAsAttention(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	queuedAt := time.Now().Add(-6 * time.Minute).UTC()
	seedRawFiles(t, ctx, store,
		guardianapi.PathWrite{LogicalPath: paths.PartitionConfig("demo"), Content: mustMarshalYAML(t, newPartition("demo"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentManifest("demo", "web"), Content: mustMarshalYAML(t, newConfigIntent("web", "web-config", "web"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentState("demo", "web"), Content: mustMarshalJSON(t, statedomain.IntentState{
			APIVersion:      "guardian/v1alpha1",
			Kind:            "IntentState",
			Partition:       "demo",
			Intent:          "web",
			Status:          statedomain.StatusHealthy,
			TargetPusher:    "local",
			LastTaskID:      "task-stalled",
			Timestamps:      statedomain.StateTimestamps{LastQueuedAt: queuedAt},
			Outputs:         map[string]string{},
			AssetVersionIDs: map[string]string{},
		})},
		guardianapi.PathWrite{LogicalPath: paths.QueueTask("local", "task-stalled"), Content: mustMarshalJSON(t, taskdomain.Task{
			APIVersion:   "guardian/v1alpha1",
			Kind:         "Task",
			TaskID:       "task-stalled",
			Partition:    "demo",
			Intent:       "web",
			Op:           taskdomain.OpCheck,
			TargetPusher: "local",
			CreatedAt:    queuedAt,
		})},
	)

	srv, err := New(Options{
		Store:       store,
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var detail PartitionDetailResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo", nil, &detail)
	if got, want := detail.Health.Status, "attention"; got != want {
		t.Fatalf("detail health status = %q, want %q", got, want)
	}
	if len(detail.Intents) != 1 {
		t.Fatalf("expected one intent, got %+v", detail.Intents)
	}
	if got, want := detail.Intents[0].DisplayStatus, "Attention"; got != want {
		t.Fatalf("intent display status = %q, want %q", got, want)
	}
	if !detail.Intents[0].TaskActive || !detail.Intents[0].TaskTimedOut {
		t.Fatalf("expected intent task runtime to show active timed out task, got %+v", detail.Intents[0])
	}
	if got := detail.Intents[0].Summary; !strings.Contains(strings.ToLower(got), "lease window") {
		t.Fatalf("intent summary = %q, want timeout summary", got)
	}
	if len(detail.Intents[0].Assets) != 1 {
		t.Fatalf("expected one asset, got %+v", detail.Intents[0].Assets)
	}
	if got, want := detail.Intents[0].Assets[0].Health, "attention"; got != want {
		t.Fatalf("asset health = %q, want %q", got, want)
	}
	if !detail.Intents[0].Assets[0].TaskActive || !detail.Intents[0].Assets[0].TaskTimedOut {
		t.Fatalf("expected asset task runtime to show active timed out task, got %+v", detail.Intents[0].Assets[0])
	}

	var activity IntentActivityResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo/intents/web/activity", nil, &activity)
	if !activity.TaskActive || !activity.TaskTimedOut {
		t.Fatalf("expected activity response to expose active timed out task, got %+v", activity)
	}
	if got, want := activity.DisplayStatus, "Attention"; got != want {
		t.Fatalf("activity display status = %q, want %q", got, want)
	}
}

func TestServerHonorsConfiguredStaleTaskAfter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	queuedAt := time.Now().Add(-6 * time.Minute).UTC()
	seedRawFiles(t, ctx, store,
		guardianapi.PathWrite{LogicalPath: paths.PartitionConfig("demo"), Content: mustMarshalYAML(t, newPartition("demo"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentManifest("demo", "web"), Content: mustMarshalYAML(t, newConfigIntent("web", "web-config", "web"))},
		guardianapi.PathWrite{LogicalPath: paths.IntentState("demo", "web"), Content: mustMarshalJSON(t, statedomain.IntentState{
			APIVersion:      "guardian/v1alpha1",
			Kind:            "IntentState",
			Partition:       "demo",
			Intent:          "web",
			Status:          statedomain.StatusHealthy,
			TargetPusher:    "local",
			LastTaskID:      "task-fresh-enough",
			Timestamps:      statedomain.StateTimestamps{LastQueuedAt: queuedAt},
			Outputs:         map[string]string{},
			AssetVersionIDs: map[string]string{},
		})},
		guardianapi.PathWrite{LogicalPath: paths.QueueTask("local", "task-fresh-enough"), Content: mustMarshalJSON(t, taskdomain.Task{
			APIVersion:   "guardian/v1alpha1",
			Kind:         "Task",
			TaskID:       "task-fresh-enough",
			Partition:    "demo",
			Intent:       "web",
			Op:           taskdomain.OpCheck,
			TargetPusher: "local",
			CreatedAt:    queuedAt,
		})},
	)

	srv, err := New(Options{
		Store:          store,
		PrincipalID:    "test-ui",
		Pushers:        []string{"local"},
		StaleTaskAfter: 10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var detail PartitionDetailResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/partitions/demo", nil, &detail)
	if got, want := detail.Intents[0].Health, "healthy"; got != want {
		t.Fatalf("intent health = %q, want %q", got, want)
	}
	if !detail.Intents[0].TaskActive {
		t.Fatalf("expected active task runtime to remain visible")
	}
	if detail.Intents[0].TaskTimedOut {
		t.Fatalf("expected custom staleTaskAfter to suppress timeout for this task")
	}
}

func TestServerSaveBundlePreservesExistingPrefixedYMLIntentPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	seedRawFiles(t, ctx, store,
		guardianapi.PathWrite{LogicalPath: paths.PartitionConfig("demo"), Content: mustMarshalYAML(t, newPartition("demo"))},
		guardianapi.PathWrite{LogicalPath: "/partitions/demo/intents/01-web.yml", Content: mustMarshalYAML(t, newConfigIntent("web", "web-config", "before"))},
		guardianapi.PathWrite{LogicalPath: "/partitions/demo/intents/02-jobs.yml", Content: mustMarshalYAML(t, newConfigIntent("jobs", "jobs-config", "jobs"))},
	)

	srv, err := New(Options{
		Store:       store,
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var saveResp SaveBundleResponse
	requestJSON(t, httpSrv, http.MethodPut, "/api/partitions/demo/bundle", SaveBundleRequest{
		Partition: newPartition("demo"),
		Intents: []SaveIntentRequest{
			{Manifest: newConfigIntent("web", "web-config", "after")},
		},
		RemoveMissingIntents: true,
	}, &saveResp)

	if !saveResp.Success {
		t.Fatalf("expected save success")
	}
	if saveResp.IntentVersionIDs["web"] == "" {
		t.Fatalf("expected version for saved intent, got %+v", saveResp.IntentVersionIDs)
	}
	if !slices.Equal(saveResp.RemovedIntents, []string{"jobs"}) {
		t.Fatalf("unexpected removed intents: %+v", saveResp.RemovedIntents)
	}

	content, err := store.ReadFile(ctx, "/partitions/demo/intents/01-web.yml")
	if err != nil {
		t.Fatalf("read preserved intent path: %v", err)
	}
	if !strings.Contains(string(content), "after") {
		t.Fatalf("expected preserved yml manifest to be updated, got %q", string(content))
	}
	if _, err := store.ReadFile(ctx, paths.IntentManifest("demo", "web")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected canonical yaml path to stay absent, got err=%v", err)
	}
	if _, err := store.ReadFile(ctx, "/partitions/demo/intents/02-jobs.yml"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected removed yml intent to be gone, got err=%v", err)
	}
}

func TestServerServesStaticAssets(t *testing.T) {
	t.Parallel()

	srv, err := New(Options{
		Store:       memory.New(),
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	tests := []struct {
		path         string
		contentType  string
		bodyFragment string
	}{
		{
			path:         "/static/app.css",
			contentType:  "text/css",
			bodyFragment: "box-sizing:border-box",
		},
		{
			path:         "/static/app.js",
			contentType:  "javascript",
			bodyFragment: "topologyZoomOut",
		},
	}

	for _, tt := range tests {
		req, err := http.NewRequest(http.MethodGet, httpSrv.URL+tt.path, nil)
		if err != nil {
			t.Fatalf("create request for %s: %v", tt.path, err)
		}

		resp, err := httpSrv.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", tt.path, err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("read %s: %v", tt.path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s returned %d: %s", tt.path, resp.StatusCode, string(body))
		}
		if got := resp.Header.Get("Content-Type"); !strings.Contains(got, tt.contentType) {
			t.Fatalf("%s Content-Type = %q, want substring %q", tt.path, got, tt.contentType)
		}
		if !strings.Contains(string(body), tt.bodyFragment) {
			t.Fatalf("%s body missing %q", tt.path, tt.bodyFragment)
		}
	}
}

func TestServerServesIndex(t *testing.T) {
	t.Parallel()

	srv, err := New(Options{
		Store:       memory.New(),
		PrincipalID: "test-ui",
		Pushers:     []string{"local"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	req, err := http.NewRequest(http.MethodGet, httpSrv.URL+"/", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	resp, err := httpSrv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET index: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read index body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index returned %d: %s", resp.StatusCode, string(body))
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("index Content-Type = %q, want HTML", got)
	}
	content := string(body)
	for _, fragment := range []string{`id="pageEyebrow"`, `id="headerContextPills"`, `id="attentionAssetsList"`, `id="topologyCanvas"`, `id="rolloutsTimeline"`, `id="historyGroupToggle"`} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("index body missing %q", fragment)
		}
	}
}

func requestJSON(t *testing.T, server *httptest.Server, method, path string, body any, out any) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		content, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(content)
	}

	req, err := http.NewRequest(method, server.URL+path, reader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s returned %d: %s", method, path, resp.StatusCode, string(content))
	}
	if out == nil {
		return
	}
	if err := json.Unmarshal(content, out); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, string(content))
	}
}

func newPartition(name string) partitiondomain.Partition {
	return partitiondomain.Partition{
		Metadata: partitiondomain.Metadata{Name: name},
		Spec: partitiondomain.Spec{
			DeletionPolicy: "orphan",
			Reconciliation: partitiondomain.ReconciliationSpec{
				Mode:     "manual",
				Interval: "15m",
			},
			Defaults: partitiondomain.PartitionDefaults{
				TargetPusher: "local",
				Target: targetdomain.Placement{
					Cluster: "local",
				},
			},
		},
	}
}

func newConfigIntent(intentName, assetName, content string) intentdomain.Intent {
	return intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: intentName},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "local",
			Target: targetdomain.Placement{
				Cluster: "local",
			},
			Assets: []intentdomain.AssetSpec{{
				Type: assetdomain.TypeConfig,
				Name: assetName,
				Properties: map[string]any{
					"format":  "text",
					"content": content,
				},
			}},
		},
	}
}

func newConfigIntentWithVersion(intentName, assetName, content, version string) intentdomain.Intent {
	intent := newConfigIntent(intentName, assetName, content)
	intent.Spec.Assets[0].Version = version
	return intent
}

func TestBuildIntentDocumentMergesYamlHintOverrides(t *testing.T) {
	manifest := intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "web"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Hints: []assetdomain.Hint{
				{Path: "outputs.url", Description: "Public URL for callers."},
				{Path: "assets.api.ports[0].containerPort", Description: "Primary HTTP port."},
			},
			Assets: []intentdomain.AssetSpec{{
				Type:  "Compute",
				Name:  "api",
				Hints: []assetdomain.Hint{{Path: "image", Description: "Manifest-specific workload image."}},
				Properties: map[string]any{
					"image": "ghcr.io/example/api:latest",
					"ports": []any{map[string]any{"containerPort": 8080}},
				},
			}},
		},
	}
	doc := buildIntentDocument(manifest, "", nil, intentTaskRuntime{}, []string{"api"}, nil)
	if len(doc.OutputHints) != 1 || doc.OutputHints[0].Path != "outputs.url" {
		t.Fatalf("unexpected output hints: %+v", doc.OutputHints)
	}
	if len(doc.Assets) != 1 {
		t.Fatalf("asset count = %d, want 1", len(doc.Assets))
	}
	imageFound := false
	portFound := false
	for _, hint := range doc.Assets[0].Hints {
		switch hint.Path {
		case "image":
			imageFound = hint.Description == "Manifest-specific workload image."
		case "ports[].containerPort":
			portFound = hint.Description == "Primary HTTP port."
		}
	}
	if !imageFound || !portFound {
		t.Fatalf("unexpected merged asset hints: %+v", doc.Assets[0].Hints)
	}
}

func historyDeploymentRecordWithVersion(deploymentRevision string) historydomain.DeploymentRecord {
	return historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: deploymentRevision,
		Partition:          "demo",
		Intent:             "web",
		Target:             targetdomain.Placement{Cluster: "local"},
		PartitionRevision:  "part-v1",
		IntentVersionID:    "intent-v1",
		AssetVersionIDs:    map[string]string{"web-config": "asset-v1"},
		AssetVersions:      map[string]string{"web-config": "v1.2.3-abc123-20260429"},
		TaskIDs:            []string{"task-1"},
		Outputs:            map[string]string{},
		CreatedAt:          time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	}
}

func mustMarshalYAML(t *testing.T, value any) []byte {
	t.Helper()

	switch typed := value.(type) {
	case partitiondomain.Partition:
		typed.APIVersion = "guardian/v1alpha1"
		typed.Kind = "Partition"
		value = typed
	case intentdomain.Intent:
		typed.APIVersion = "guardian/v1alpha1"
		typed.Kind = "Intent"
		value = typed
	}

	content, err := yaml.Marshal(value)
	if err != nil {
		t.Fatalf("marshal yaml: %v", err)
	}
	return content
}

func mustMarshalJSON(t *testing.T, value any) []byte {
	t.Helper()

	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return content
}

func seedRawFiles(t *testing.T, ctx context.Context, store *memory.Store, writes ...guardianapi.PathWrite) {
	t.Helper()

	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: writes,
		Context: guardianapi.MutationContext{
			PrincipalID: "test-ui",
			Reason:      "seed test fixtures",
		},
	}); err != nil {
		t.Fatalf("seed store: %v", err)
	}
}

type countingStore struct {
	guardianapi.Store

	mu    sync.Mutex
	reads int
}

func newCountingStore(store guardianapi.Store) *countingStore {
	return &countingStore{Store: store}
}

func (s *countingStore) ReadFile(ctx context.Context, logicalPath string) ([]byte, error) {
	s.recordRead()
	return s.Store.ReadFile(ctx, logicalPath)
}

func (s *countingStore) ListDir(ctx context.Context, logicalDir string) ([]guardianapi.DirEntry, error) {
	s.recordRead()
	return s.Store.ListDir(ctx, logicalDir)
}

func (s *countingStore) Stat(ctx context.Context, logicalPath string) (guardianapi.FileInfo, error) {
	s.recordRead()
	return s.Store.Stat(ctx, logicalPath)
}

func (s *countingStore) totalReads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

func (s *countingStore) recordRead() {
	s.mu.Lock()
	s.reads++
	s.mu.Unlock()
}
