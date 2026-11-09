//go:build integration

package gcpobserve

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestObservedAgainstGCP verifies Cloud Asset Inventory and the state bucket.
//
//	GCP_PROJECT=strata-gcp-dev GCP_REGION=europe-west1 \
//	TF_STATE_BUCKET=strata-gcp-dev-tofu-state \
//	go test -tags integration -run TestObservedAgainstGCP ./internal/gcpobserve/ -v
func TestObservedAgainstGCP(t *testing.T) {
	project := os.Getenv("GCP_PROJECT")
	if project == "" {
		t.Skip("set GCP_PROJECT to run the integration test")
	}
	region := os.Getenv("GCP_REGION")
	if region == "" {
		region = "europe-west1"
	}
	refresher := NewRefresher(os.Getenv("TF_STATE_BUCKET"), region)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client, err := refresher.httpClient(ctx)
	if err != nil {
		t.Fatalf("httpClient: %v", err)
	}
	resources, err := refresher.listResources(ctx, client, project)
	if err != nil {
		t.Fatalf("listResources: %v", err)
	}
	t.Logf("Cloud Asset Inventory resources: %d", len(resources))
	if len(resources) == 0 {
		t.Fatal("expected at least one Cloud Asset Inventory resource")
	}

	stacks, err := refresher.Refresh(ctx, project, region, nil)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	t.Logf("state-bucket stacks: %d", len(stacks))
}
