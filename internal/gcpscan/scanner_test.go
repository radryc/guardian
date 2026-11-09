package gcpscan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radryc/guardian/internal/awsscan"
)

func testScanner(t *testing.T) *Scanner {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/demo:searchAllResources", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("pageToken") {
		case "":
			_, _ = w.Write([]byte(`{"results":[
				{"name":"//run.googleapis.com/projects/demo/locations/europe-west1/services/guardian-demo-api-app","assetType":"run.googleapis.com/Service","location":"europe-west1","displayName":"guardian-demo-api-app"},
				{"name":"//storage.googleapis.com/guardian-demo-api-media","assetType":"storage.googleapis.com/Bucket","location":"EU","displayName":"guardian-demo-api-media"}
			],"nextPageToken":"page2"}`))
		case "page2":
			_, _ = w.Write([]byte(`{"results":[
				{"name":"//compute.googleapis.com/projects/demo/zones/europe-west1-a/instances/vm-1","assetType":"compute.googleapis.com/Instance","location":"europe-west1-a"}
			]}`))
		default:
			_, _ = w.Write([]byte(`{"results":[]}`))
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &Scanner{Project: "demo", Region: "europe-west1", AssetURL: server.URL, HTTPClient: server.Client()}
}

func TestScanBuildsInventory(t *testing.T) {
	scanner := testScanner(t)
	result := scanner.Scan(context.Background(), &awsscan.ScanRequest{
		ScanID:  "scan-1",
		Account: "demo",
		Regions: []string{"europe-west1"},
	})
	if result.Status != awsscan.ScanStatusSucceeded {
		t.Fatalf("status = %q errors=%v", result.Status, result.Errors)
	}
	if result.ScanID != "scan-1" || result.Account != "demo" {
		t.Fatalf("result identity = %+v", result)
	}
	if got := result.Summary.InventoryCount; got != 3 {
		t.Fatalf("inventory count = %d", got)
	}
	services := result.Inventory["run.googleapis.com/Service"]["europe-west1"]
	if len(services) != 1 {
		t.Fatalf("services = %+v", services)
	}
	if services[0].Identifier == "" || len(services[0].Properties) == 0 {
		t.Fatalf("resource = %+v", services[0])
	}
}

func TestScanFiltersResourceTypes(t *testing.T) {
	scanner := testScanner(t)
	result := scanner.Scan(context.Background(), &awsscan.ScanRequest{
		ScanID:               "scan-2",
		IncludeResourceTypes: []string{"run.googleapis.com/Service"},
	})
	if got := result.Summary.InventoryCount; got != 1 {
		t.Fatalf("inventory count = %d", got)
	}
	if len(result.Inventory["storage.googleapis.com/Bucket"]) != 0 {
		t.Fatalf("bucket should be excluded: %+v", result.Inventory)
	}

	result = scanner.Scan(context.Background(), &awsscan.ScanRequest{
		ScanID:               "scan-3",
		ExcludeResourceTypes: []string{"compute.googleapis.com/Instance"},
	})
	if got := result.Summary.InventoryCount; got != 2 {
		t.Fatalf("exclude count = %d", got)
	}
}

func TestScanReportsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no permission", http.StatusForbidden)
	}))
	defer server.Close()
	scanner := &Scanner{Project: "demo", AssetURL: server.URL, HTTPClient: server.Client()}
	result := scanner.Scan(context.Background(), &awsscan.ScanRequest{ScanID: "scan-4"})
	if result.Status != awsscan.ScanStatusFailed {
		t.Fatalf("status = %q", result.Status)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("errors = %+v", result.Errors)
	}
}

func TestScanFiltersRequestedRegions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[
			{"name":"//run.googleapis.com/projects/demo/locations/europe-west1/services/svc","assetType":"run.googleapis.com/Service","location":"europe-west1"},
			{"name":"//storage.googleapis.com/bucket","assetType":"storage.googleapis.com/Bucket","location":"EU"},
			{"name":"//compute.googleapis.com/projects/demo/zones/us-central1-a/instances/vm","assetType":"compute.googleapis.com/Instance","location":"us-central1-a"}
		]}`))
	}))
	defer server.Close()
	scanner := &Scanner{Project: "demo", AssetURL: server.URL, HTTPClient: server.Client()}
	result := scanner.Scan(context.Background(), &awsscan.ScanRequest{
		ScanID:  "scan-region",
		Regions: []string{"europe-west1"},
	})
	if got := result.Summary.InventoryCount; got != 2 {
		t.Fatalf("inventory count = %d, want regional + multi-region resources only: %+v", got, result.Inventory)
	}
	if len(result.Inventory["compute.googleapis.com/Instance"]) != 0 {
		t.Fatalf("us-central1 resource should be filtered: %+v", result.Inventory)
	}
}
