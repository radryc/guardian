package observed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/dispatcher"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

const desiredTemplate = `{
  "Resources": {
    "Bucket": {
      "Type": "AWS::S3::Bucket",
      "Metadata": {"aws:cdk:path": "Stack/Bucket/Resource"},
      "Properties": {"BucketName": "demo"}
    }
  }
}`

const observedInSyncTemplate = `{
  "Resources": {
    "Bucket": {
      "Type": "AWS::S3::Bucket",
      "Properties": {"BucketName": "demo"}
    }
  }
}`

const observedDriftedTemplate = `{
  "Resources": {
    "Bucket": {
      "Type": "AWS::S3::Bucket",
      "Properties": {"BucketName": "changed-outside-guardian"}
    }
  }
}`

func TestCompareTemplatesIgnoresCDKMetadata(t *testing.T) {
	changed, err := CompareTemplates(desiredTemplate, observedInSyncTemplate)
	if err != nil {
		t.Fatalf("CompareTemplates() error = %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want none", changed)
	}
}

func TestCompareTemplatesDetectsPropertyDrift(t *testing.T) {
	changed, err := CompareTemplates(desiredTemplate, observedDriftedTemplate)
	if err != nil {
		t.Fatalf("CompareTemplates() error = %v", err)
	}
	if len(changed) != 1 || changed[0] != "changed:Bucket" {
		t.Fatalf("changed = %v, want [changed:Bucket]", changed)
	}
}

func TestHealthFor(t *testing.T) {
	health, _ := HealthFor("UPDATE_COMPLETE", "IN_SYNC", 0)
	if health.Status != "Healthy" {
		t.Fatalf("health = %q, want Healthy", health.Status)
	}
	health, _ = HealthFor("UPDATE_ROLLBACK_COMPLETE", "", 0)
	if health.Status != "Unhealthy" {
		t.Fatalf("rollback health = %q, want Unhealthy", health.Status)
	}
	health, _ = HealthFor("CREATE_IN_PROGRESS", "", 0)
	if health.Status != "Degraded" {
		t.Fatalf("in-progress health = %q, want Degraded", health.Status)
	}
}

func seedObservedPartition(t *testing.T, ctx context.Context, store guardianapi.Store) {
	t.Helper()
	seedObservedPartitionTemplate(t, ctx, store, desiredTemplate)
}

func seedObservedPartitionTemplate(t *testing.T, ctx context.Context, store guardianapi.Store, template string) {
	t.Helper()
	seed := func(logicalPath string, content string) {
		t.Helper()
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: []byte(content)}},
		}); err != nil {
			t.Fatalf("seed %s: %v", logicalPath, err)
		}
	}
	seed(paths.IntentManifest("strata-aws", "aws-pusher"), `
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: aws-pusher
spec:
  intentType: observed
  targetPusher: aws-123456789012
  target:
    account: "123456789012"
    region: eu-west-1
  assets:
    - type: CDKStack
      name: pusher
      payload:
        aws: /partitions/strata-aws/payloads/aws/pusher.yaml
`)
	seed("/partitions/strata-aws/payloads/aws/pusher.yaml", `
sourceType: cdk-ts
prebuiltAssemblyDir: /partitions/strata-aws/payloads/aws/cdk.out
stackName: AwsPusherStack
stackID: AwsPusherStack
packageManager: none
`)
	seed("/partitions/strata-aws/payloads/aws/cdk.out/manifest.json", `{
  "version": "39.0.0",
  "artifacts": {
    "AwsPusherStack.assets": {"type": "cdk:asset-manifest", "properties": {"file": "AwsPusherStack.assets.json"}},
    "AwsPusherStack": {
      "type": "aws:cloudformation:stack",
      "properties": {"templateFile": "AwsPusherStack.template.json", "stackName": "AwsPusherStack"}
    }
  }
}`)
	seed("/partitions/strata-aws/payloads/aws/cdk.out/AwsPusherStack.template.json", template)
}

func fakePusherServer(t *testing.T, observedTemplate string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/refresh"):
			writeFakeJSON(t, w, map[string]any{"stacks": []map[string]any{{
				"account": "123456789012", "region": "eu-west-1",
				"stackId":   "arn:aws:cloudformation:eu-west-1:123456789012:stack/AwsPusherStack/abc",
				"stackName": "AwsPusherStack", "stackStatus": "UPDATE_COMPLETE",
				"driftStatus": "IN_SYNC",
			}}})
		case strings.HasSuffix(r.URL.Path, "/template"):
			writeFakeJSON(t, w, map[string]any{"stackName": "AwsPusherStack", "template": observedTemplate})
		case strings.Contains(r.URL.Path, "/observed/stacks/"):
			writeFakeJSON(t, w, map[string]any{
				"account": "123456789012", "region": "eu-west-1",
				"stackId":   "arn:aws:cloudformation:eu-west-1:123456789012:stack/AwsPusherStack/abc",
				"stackName": "AwsPusherStack", "stackStatus": "UPDATE_COMPLETE",
				"driftStatus": "IN_SYNC", "lastEventType": "CloudFormation Stack Status Change",
			})
		case strings.Contains(r.URL.Path, "/observed/events"):
			writeFakeJSON(t, w, map[string]any{"events": []map[string]any{{
				"id": "evt-1", "account": "123456789012", "region": "eu-west-1",
				"stackName": "AwsPusherStack", "detailType": "CloudFormation Stack Status Change",
				"status": "UPDATE_COMPLETE", "time": time.Now().UTC().Format(time.RFC3339),
			}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func writeFakeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode fake response: %v", err)
	}
}

func newTestCollector(t *testing.T, store guardianapi.Store, baseURL string) *Collector {
	t.Helper()
	return &Collector{
		Store:      store,
		Dispatcher: dispatcher.NewDispatcher(store, "guardiand"),
		Endpoints: map[string]Endpoint{
			"aws-123456789012": {Name: "aws-123456789012", Client: NewClient(baseURL, "", 5*time.Second)},
		},
		Interval: time.Minute,
		Timeout:  5 * time.Second,
	}
}

func TestCollectorInSyncMarksObserved(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	seedObservedPartition(t, ctx, store)
	server := fakePusherServer(t, observedInSyncTemplate, http.StatusOK)
	defer server.Close()

	collector := newTestCollector(t, store, server.URL)
	if err := collector.CollectOnce(ctx); err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}

	state, err := common.LoadIntentState(ctx, store, "strata-aws", "aws-pusher")
	if err != nil {
		t.Fatalf("LoadIntentState: %v", err)
	}
	if state.Status != statedomain.StatusObserved {
		t.Fatalf("status = %q, want Observed", state.Status)
	}
	if state.IntentType != "observed" {
		t.Fatalf("intentType = %q, want observed", state.IntentType)
	}
	if _, err := store.ReadFile(ctx, paths.ObservedSnapshot("strata-aws", "aws-pusher", "pusher")); err != nil {
		t.Fatalf("snapshot missing: %v", err)
	}
	if len(state.Resources) != 1 {
		t.Fatalf("resources = %+v, want one", state.Resources)
	}
	resource := state.Resources[0]
	if resource.LogicalID != "Bucket" || resource.CfnType != "AWS::S3::Bucket" ||
		resource.AssetName != "res-bucket" || resource.Status != statedomain.ResourceInSync {
		t.Fatalf("resource = %+v", resource)
	}
}

func TestCollectorDriftMarksObservedDrifted(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	seedObservedPartition(t, ctx, store)
	server := fakePusherServer(t, observedDriftedTemplate, http.StatusOK)
	defer server.Close()

	collector := newTestCollector(t, store, server.URL)
	if err := collector.CollectOnce(ctx); err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}

	state, err := common.LoadIntentState(ctx, store, "strata-aws", "aws-pusher")
	if err != nil {
		t.Fatalf("LoadIntentState: %v", err)
	}
	if state.Status != statedomain.StatusObservedDrifted {
		t.Fatalf("status = %q, want ObservedDrifted", state.Status)
	}
	if state.Drift == nil || len(state.Drift.ChangedAssets) != 2 {
		t.Fatalf("drift = %+v", state.Drift)
	}
	changed := map[string]bool{}
	for _, name := range state.Drift.ChangedAssets {
		changed[name] = true
	}
	if !changed["pusher"] || !changed["res-bucket"] {
		t.Fatalf("changedAssets = %v, want pusher and res-bucket", state.Drift.ChangedAssets)
	}
	if len(state.Resources) != 1 || state.Resources[0].Status != statedomain.ResourceChanged {
		t.Fatalf("resources = %+v", state.Resources)
	}
	entries, err := store.ListDir(ctx, paths.QueueDir("aws-123456789012"))
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir {
				t.Fatalf("observed collector must not queue tasks, found %q", entry.Name)
			}
		}
	}
}

func TestCollectorBuildsResourceInventoryAndChangedAssets(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	seedObservedPartitionTemplate(t, ctx, store, `{
	  "Resources": {
	    "Bucket": {"Type": "AWS::S3::Bucket", "Properties": {"BucketName": "same"}},
	    "Queue": {"Type": "AWS::SQS::Queue"},
	    "Table": {"Type": "AWS::DynamoDB::Table", "Properties": {"BillingMode": "PAY_PER_REQUEST"}}
	  }
	}`)
	server := fakePusherServer(t, `{
	  "Resources": {
	    "Bucket": {"Type": "AWS::S3::Bucket", "Properties": {"BucketName": "same"}},
	    "Table": {"Type": "AWS::DynamoDB::Table", "Properties": {"BillingMode": "PROVISIONED"}}
	  }
	}`, http.StatusOK)
	defer server.Close()

	collector := newTestCollector(t, store, server.URL)
	if err := collector.CollectOnce(ctx); err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}

	state, err := common.LoadIntentState(ctx, store, "strata-aws", "aws-pusher")
	if err != nil {
		t.Fatalf("LoadIntentState: %v", err)
	}
	if len(state.Resources) != 3 {
		t.Fatalf("resources = %+v, want 3", state.Resources)
	}
	statuses := map[string]string{}
	for _, resource := range state.Resources {
		statuses[resource.LogicalID] = resource.Status
	}
	if statuses["Bucket"] != statedomain.ResourceInSync || statuses["Queue"] != statedomain.ResourceAdded || statuses["Table"] != statedomain.ResourceChanged {
		t.Fatalf("statuses = %+v", statuses)
	}
	changed := map[string]bool{}
	for _, name := range state.Drift.ChangedAssets {
		changed[name] = true
	}
	if !changed["pusher"] || !changed["res-queue"] || !changed["res-table"] {
		t.Fatalf("changedAssets = %v", state.Drift.ChangedAssets)
	}
	if changed["res-bucket"] {
		t.Fatalf("in-sync resource marked changed: %v", state.Drift.ChangedAssets)
	}
}

func TestCollectorPusherErrorMarksDiffFailed(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	seedObservedPartition(t, ctx, store)
	server := fakePusherServer(t, observedInSyncTemplate, http.StatusInternalServerError)
	defer server.Close()

	collector := newTestCollector(t, store, server.URL)
	if err := collector.CollectOnce(ctx); err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}

	state, err := common.LoadIntentState(ctx, store, "strata-aws", "aws-pusher")
	if err != nil {
		t.Fatalf("LoadIntentState: %v", err)
	}
	if state.Status != statedomain.StatusDiffFailed {
		t.Fatalf("status = %q, want DiffFailed", state.Status)
	}
	if state.LastError == nil || *state.LastError == "" {
		t.Fatalf("LastError = %v, want set", state.LastError)
	}
}

func TestCollectorSourceDirUsesPusherDiff(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	seed := func(logicalPath, content string) {
		t.Helper()
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: []byte(content)}},
		}); err != nil {
			t.Fatalf("seed %s: %v", logicalPath, err)
		}
	}
	seed(paths.IntentManifest("strata-aws", "aws-pusher"), `
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: aws-pusher
spec:
  intentType: observed
  targetPusher: aws-123456789012
  target:
    account: "123456789012"
    region: eu-west-1
  assets:
    - type: CDKStack
      name: pusher
      payload:
        aws: /partitions/strata-aws/payloads/aws/pusher.yaml
`)
	seed("/partitions/strata-aws/payloads/aws/pusher.yaml", `
sourceType: cdk-ts
sourceDir: /partitions/strata-aws/payloads/aws/src
stackName: AwsPusherStack
stackID: AwsPusherStack
packageManager: none
`)
	seed("/partitions/strata-aws/payloads/aws/src/package.json", `{"name":"app"}`)
	seed("/partitions/strata-aws/payloads/aws/src/bin/app.ts", `// app`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/refresh"):
			writeFakeJSON(t, w, map[string]any{"stacks": []map[string]any{{
				"account": "123456789012", "region": "eu-west-1",
				"stackName": "AwsPusherStack", "stackStatus": "UPDATE_COMPLETE", "driftStatus": "IN_SYNC",
			}}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/diff"):
			writeFakeJSON(t, w, map[string]any{
				"stackName":        "AwsPusherStack",
				"changedResources": []string{"changed:Bucket"},
				"summary":          "1 resource(s) differ from desired",
				"observed":         map[string]any{"stackName": "AwsPusherStack"},
			})
		case strings.Contains(r.URL.Path, "/observed/stacks/"):
			writeFakeJSON(t, w, map[string]any{
				"account": "123456789012", "region": "eu-west-1",
				"stackName": "AwsPusherStack", "stackStatus": "UPDATE_COMPLETE", "driftStatus": "IN_SYNC",
			})
		case strings.Contains(r.URL.Path, "/observed/events"):
			writeFakeJSON(t, w, map[string]any{"events": []map[string]any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	collector := newTestCollector(t, store, server.URL)
	if err := collector.CollectOnce(ctx); err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}
	state, err := common.LoadIntentState(ctx, store, "strata-aws", "aws-pusher")
	if err != nil {
		t.Fatalf("LoadIntentState: %v", err)
	}
	if state.Status != statedomain.StatusObservedDrifted {
		t.Fatalf("status = %q, want ObservedDrifted", state.Status)
	}
	if state.Drift == nil || !strings.Contains(state.Drift.Summary, "1 resource(s) differ") {
		t.Fatalf("drift = %+v", state.Drift)
	}
}
