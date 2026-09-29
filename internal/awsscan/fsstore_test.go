package awsscan

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	fsstore "github.com/rydzu/ainfra/guardian/internal/store/fs"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestScanRunnerAgainstFileStore(t *testing.T) {
	root := t.TempDir()
	store, err := fsstore.Open(filepath.Join(root, "store"))
	if err != nil {
		t.Fatalf("open fs store: %v", err)
	}
	requestsDir := filepath.Join(root, "store", ".scans", "aws-123456789012", "requests")
	if err := os.MkdirAll(requestsDir, 0o755); err != nil {
		t.Fatalf("create requests dir: %v", err)
	}
	request := ScanRequest{
		APIVersion: APIVersion,
		Kind:       RequestKind,
		ScanID:     "scan-fs",
		Account:    "123456789012",
		Regions:    []string{"eu-west-1"},
		CreatedAt:  time.Now().UTC(),
	}
	content, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := os.WriteFile(filepath.Join(requestsDir, "scan-fs.json"), content, 0o644); err != nil {
		t.Fatalf("write request: %v", err)
	}

	scanner := &fakeScanner{results: []*ScanResult{{
		APIVersion: APIVersion,
		Kind:       ResultKind,
		Status:     ScanStatusSucceeded,
		Regions:    []string{"eu-west-1"},
		Buckets:    []BucketResource{{Name: "fs-bucket", Region: "eu-west-1"}},
	}}}
	runner := &ScanRunner{
		PusherName:  "aws-123456789012",
		Account:     "123456789012",
		WorkerID:    "fs-worker",
		PrincipalID: "fs-principal",
		Store:       store,
		NewScanner:  func() Scanner { return scanner },
	}
	if err := runner.ProcessPending(context.Background()); err != nil {
		t.Fatalf("process pending: %v", err)
	}
	if scanner.calls != 1 {
		t.Fatalf("expected scanner execution, calls=%d", scanner.calls)
	}
	raw, err := store.ReadFile(context.Background(), "/.scans/aws-123456789012/.results/scan-fs.json")
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var result ScanResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != ScanStatusSucceeded || result.Pusher != "aws-123456789012" || result.ScanID != "scan-fs" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Summary.BucketCount != 1 {
		t.Fatalf("unexpected summary: %+v", result.Summary)
	}
	var _ guardianapi.Store = store
}
