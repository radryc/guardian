//go:build integration

package gcpread

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestGCPReaderAgainstGCP exercises the real Cloud Monitoring, Logging, and
// Trace APIs with Application Default Credentials.
//
//	GCP_PROJECT=strata-gcp-dev GCP_REGION=europe-west1 \
//	go test -tags integration -run TestGCPReaderAgainstGCP ./internal/gcpread/ -v
func TestGCPReaderAgainstGCP(t *testing.T) {
	project := os.Getenv("GCP_PROJECT")
	if project == "" {
		t.Skip("set GCP_PROJECT to run the integration test")
	}
	region := os.Getenv("GCP_REGION")
	if region == "" {
		region = "europe-west1"
	}
	client, err := New(Config{Project: project, DefaultRegion: region})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	descriptors, err := client.ListMetrics(ctx, "run.googleapis.com", "request_count")
	if err != nil {
		t.Fatalf("ListMetrics: %v", err)
	}
	t.Logf("metric descriptors: %d", len(descriptors))
	if len(descriptors) == 0 {
		t.Log("no run.googleapis.com/request_count descriptor (the smoke service may not have received traffic yet)")
	}

	logNames, err := client.ListLogNames(ctx)
	if err != nil {
		t.Fatalf("ListLogNames: %v", err)
	}
	t.Logf("log names: %d %v", len(logNames), logNames)

	now := time.Now()
	records, err := client.QueryLogs(ctx, LogQuery{From: now.Add(-24 * time.Hour), To: now, Limit: 5})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	t.Logf("log records: %d", len(records))
	for i, record := range records {
		if i >= 3 {
			break
		}
		t.Logf("  %s %s %s", record.Timestamp.Format(time.RFC3339), record.LogGroup, record.Message)
	}

	summaries, err := client.QueryTraces(ctx, TraceQuery{From: now.Add(-24 * time.Hour), To: now, Limit: 10})
	if err != nil {
		t.Fatalf("QueryTraces: %v", err)
	}
	t.Logf("trace summaries: %d", len(summaries))

	services, err := client.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	t.Logf("services: %d %v", len(services), services)

	alerts, err := client.ListAlerts(ctx)
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	t.Logf("alert policies: %d", len(alerts))
}
