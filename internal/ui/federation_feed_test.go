package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func TestM2MFederationPartitionsPaginatesAndDerivesAWSPlacement(t *testing.T) {
	ctx := context.Background()
	files := memory.New()
	seedFederationPartition(t, ctx, files, "div-a-orders", "780603797542", "us-east-1", "aws-div-780603797542")
	seedFederationPartition(t, ctx, files, "div-b-inventory", "755370915520", "us-west-2", "aws-div-755370915520")

	srv, err := New(Options{Store: files, PrincipalID: "test-ui"})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	var first federationPartitionPage
	getFederationFeed(t, httpSrv, "/api/m2m/federation/partitions?limit=1", &first)
	if len(first.Partitions) != 1 || first.Partitions[0].Name != "div-a-orders" {
		t.Fatalf("first page = %#v", first)
	}
	if !first.HasMore || first.NextCursor != "div-a-orders" {
		t.Fatalf("first page cursor = %#v", first)
	}
	if first.Partitions[0].Account != "780603797542" ||
		first.Partitions[0].Region != "us-east-1" ||
		first.Partitions[0].Provider != "aws" {
		t.Fatalf("first placement = %#v", first.Partitions[0])
	}

	var second federationPartitionPage
	getFederationFeed(t, httpSrv, "/api/m2m/federation/partitions?limit=1&cursor=div-a-orders", &second)
	if len(second.Partitions) != 1 || second.Partitions[0].Name != "div-b-inventory" || second.HasMore {
		t.Fatalf("second page = %#v", second)
	}
	if second.Partitions[0].Account != "755370915520" || second.Partitions[0].Region != "us-west-2" {
		t.Fatalf("second placement = %#v", second.Partitions[0])
	}
}

func seedFederationPartition(t *testing.T, ctx context.Context, files *memory.Store, name, account, region, pusher string) {
	t.Helper()
	config := []byte(`apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: ` + name + `
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 1m
`)
	intent := []byte(`apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: app
spec:
  intentType: observed
  targetPusher: ` + pusher + `
  target:
    account: "` + account + `"
    region: ` + region + `
  assets: []
`)
	if _, err := files.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{
			{LogicalPath: paths.PartitionConfig(name), Content: config},
			{LogicalPath: paths.IntentManifest(name, "app"), Content: intent},
		},
		Context: guardianapi.MutationContext{PrincipalID: "test"},
	}); err != nil {
		t.Fatalf("seed partition %s: %v", name, err)
	}
}

func getFederationFeed(t *testing.T, server *httptest.Server, path string, out *federationPartitionPage) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if strings.TrimSpace(out.Partitions[0].Name) == "" {
		t.Fatalf("empty partition name in %#v", out)
	}
}
