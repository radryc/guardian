package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/awsscan"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func newScanTestServer(t *testing.T) (*Server, *httptest.Server, *memory.Store) {
	t.Helper()
	store := memory.New()
	srv, err := New(Options{
		Store:       store,
		PrincipalID: "test-ui",
		Pushers:     []string{"aws-123456789012"},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)
	return srv, httpSrv, store
}

func TestScanCreateQueuesRequest(t *testing.T) {
	_, httpSrv, store := newScanTestServer(t)

	var create CreateScanResponse
	requestJSON(t, httpSrv, http.MethodPost, "/api/scans", CreateScanRequest{
		Pusher:    "aws-123456789012",
		Regions:   []string{"eu-west-1"},
		Inventory: true,
	}, &create)
	if create.ScanID == "" || create.Status != string(awsscan.ScanStatusQueued) {
		t.Fatalf("unexpected create response %+v", create)
	}
	if create.Account != "123456789012" {
		t.Fatalf("expected account derived from pusher, got %q", create.Account)
	}

	raw, err := store.ReadFile(context.Background(), paths.ScanRequest("aws-123456789012", create.ScanID))
	if err != nil {
		t.Fatalf("read scan request: %v", err)
	}
	var request awsscan.ScanRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatalf("decode scan request: %v", err)
	}
	if request.Account != "123456789012" || request.InventoryDetail != awsscan.InventoryDetailSummary {
		t.Fatalf("unexpected persisted request %+v", request)
	}
	if len(request.Regions) != 1 || request.Regions[0] != "eu-west-1" {
		t.Fatalf("unexpected regions %v", request.Regions)
	}

	var list ScanListResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/scans", nil, &list)
	if len(list.Scans) != 1 || list.Scans[0].Status != string(awsscan.ScanStatusQueued) {
		t.Fatalf("unexpected scan list %+v", list.Scans)
	}
}

func TestScanCreateRejectsUnknownPusherAndMissingAccount(t *testing.T) {
	_, httpSrv, _ := newScanTestServer(t)

	resp, err := httpSrv.Client().Post(httpSrv.URL+"/api/scans", "application/json", jsonBody(CreateScanRequest{Pusher: "aws-999999999999"}))
	if err != nil {
		t.Fatalf("post scan: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unconfigured pusher, got %d", resp.StatusCode)
	}

	httpSrv2, _, _ := newScanTestServerWithPushers(t, nil)
	defer httpSrv2.Close()
	resp2, err := httpSrv2.Client().Post(httpSrv2.URL+"/api/scans", "application/json", jsonBody(CreateScanRequest{Pusher: "notaws"}))
	if err != nil {
		t.Fatalf("post scan: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for pusher without account, got %d", resp2.StatusCode)
	}
}

func newScanTestServerWithPushers(t *testing.T, pushers []string) (*httptest.Server, *memory.Store, *Server) {
	t.Helper()
	store := memory.New()
	srv, err := New(Options{Store: store, PrincipalID: "test-ui", Pushers: pushers})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	httpSrv := httptest.NewServer(srv)
	return httpSrv, store, srv
}

func TestScanListGetAndBundleRoundTrip(t *testing.T) {
	_, httpSrv, store := newScanTestServer(t)
	ctx := context.Background()

	scanID := "scan-roundtrip"
	result := &awsscan.ScanResult{
		APIVersion: awsscan.APIVersion,
		Kind:       awsscan.ResultKind,
		ScanID:     scanID,
		Pusher:     "aws-123456789012",
		Account:    "123456789012",
		Status:     awsscan.ScanStatusSucceeded,
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
		Regions:    []string{"eu-west-1"},
		Buckets: []awsscan.BucketResource{{
			Name: "legacy-bucket", Region: "eu-west-1", Versioned: true,
		}},
	}
	result.Summary = awsscan.ScanSummary{
		RegionCount: 1, BucketCount: 1,
	}
	content, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: paths.ScanResult("aws-123456789012", scanID),
			Content:     content,
		}},
	}); err != nil {
		t.Fatalf("write result: %v", err)
	}

	var list ScanListResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/scans", nil, &list)
	if len(list.Scans) != 1 {
		t.Fatalf("expected one scan, got %+v", list.Scans)
	}
	entry := list.Scans[0]
	if entry.Status != string(awsscan.ScanStatusSucceeded) || entry.Summary == nil || entry.Summary.BucketCount != 1 {
		t.Fatalf("unexpected list entry %+v", entry)
	}

	var fetched awsscan.ScanResult
	requestJSON(t, httpSrv, http.MethodGet, "/api/scans/"+scanID, nil, &fetched)
	if fetched.ScanID != scanID || len(fetched.Buckets) != 1 {
		t.Fatalf("unexpected scan detail %+v", fetched)
	}

	var bundleResp ScanBundleResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/scans/"+scanID+"/bundle?partition=imported", nil, &bundleResp)
	if bundleResp.ScanID != scanID {
		t.Fatalf("unexpected bundle response scanID %q", bundleResp.ScanID)
	}
	if len(bundleResp.Bundle.Intents) != 1 || len(bundleResp.Bundle.Intents[0].Manifest.Spec.Assets) != 1 {
		t.Fatalf("expected generated bundle with one intent, got %+v", bundleResp.Bundle)
	}
	asset := bundleResp.Bundle.Intents[0].Manifest.Spec.Assets[0]
	if asset.Properties["existingBucket"] != "legacy-bucket" {
		t.Fatalf("expected adoption field in generated asset, got %+v", asset)
	}
	if bundleResp.Draft == nil || len(bundleResp.Draft.Unmapped) != 0 {
		t.Fatalf("unexpected draft %+v", bundleResp.Draft)
	}

	bundleResp.Bundle.RemoveMissingIntents = false
	var saveResp SaveBundleResponse
	requestJSON(t, httpSrv, http.MethodPut, "/api/partitions/imported/bundle", bundleResp.Bundle, &saveResp)
	if !saveResp.Success {
		t.Fatalf("expected bundle save success, got %+v", saveResp)
	}

	manifest, err := store.ReadFile(ctx, paths.IntentManifest("imported", bundleResp.Bundle.Intents[0].Manifest.Metadata.Name))
	if err != nil {
		t.Fatalf("read saved intent: %v", err)
	}
	if !jsonSafeContains(string(manifest), "existingBucket") {
		t.Fatalf("expected adoption field persisted in manifest, got:\n%s", manifest)
	}

	var regenerate ScanBundleResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/scans/"+scanID+"/bundle?partition=imported", nil, &regenerate)
	if len(regenerate.Draft.Intents) != 0 {
		t.Fatalf("expected regeneration to cross-reference already imported bucket, got %+v", regenerate.Draft.Intents)
	}
	if len(regenerate.Draft.Managed) != 1 || !regenerate.Draft.Managed[0].ExistingIntent {
		t.Fatalf("expected managed cross-reference entry, got %+v", regenerate.Draft.Managed)
	}
}

func TestScanBundleRejectsMissingScanAndPartition(t *testing.T) {
	_, httpSrv, _ := newScanTestServer(t)

	resp, err := http.Get(httpSrv.URL + "/api/scans/missing/bundle?partition=imported")
	if err != nil {
		t.Fatalf("get bundle: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing scan, got %d", resp.StatusCode)
	}

	httpSrv2, _, _ := newScanTestServerWithPushers(t, nil)
	defer httpSrv2.Close()
	resp2, err := http.Get(httpSrv2.URL + "/api/scans/missing/bundle")
	if err != nil {
		t.Fatalf("get bundle: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing partition param, got %d", resp2.StatusCode)
	}
}

func TestScanBundleStackFilter(t *testing.T) {
	_, httpSrv, store := newScanTestServer(t)
	ctx := context.Background()

	scanID := "scan-stacks"
	result := &awsscan.ScanResult{
		APIVersion: awsscan.APIVersion,
		Kind:       awsscan.ResultKind,
		ScanID:     scanID,
		Pusher:     "aws-123456789012",
		Account:    "123456789012",
		Status:     awsscan.ScanStatusSucceeded,
		Regions:    []string{"eu-west-1"},
		Buckets: []awsscan.BucketResource{
			{Name: "analytics-data", Region: "eu-west-1", Stack: "analytics"},
			{Name: "billing-data", Region: "eu-west-1", Stack: "billing"},
		},
	}
	content, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: paths.ScanResult("aws-123456789012", scanID),
			Content:     content,
		}},
	}); err != nil {
		t.Fatalf("write result: %v", err)
	}

	var filtered ScanBundleResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/scans/"+scanID+"/bundle?partition=imported&stacks=billing", nil, &filtered)
	if len(filtered.Bundle.Intents) != 1 || filtered.Bundle.Intents[0].Manifest.Metadata.Name != "import-billing" {
		t.Fatalf("expected only the billing stack intent, got %+v", filtered.Bundle.Intents)
	}

	var all ScanBundleResponse
	requestJSON(t, httpSrv, http.MethodGet, "/api/scans/"+scanID+"/bundle?partition=imported", nil, &all)
	if len(all.Bundle.Intents) != 2 {
		t.Fatalf("expected both stacks without a filter, got %+v", all.Bundle.Intents)
	}
}

func jsonBody(value any) *bytes.Reader {
	content, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return bytes.NewReader(content)
}

func jsonSafeContains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
