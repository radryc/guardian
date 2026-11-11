package gcpdriver

import (
	"context"
	"strings"
	"testing"

	targetdomain "github.com/radryc/guardian/internal/domain/target"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/registry"
)

func targetForTest(project, region string) targetdomain.Placement {
	return targetdomain.Placement{Account: project, Region: region}
}

type fakeBackend struct {
	lastReq   StackRequest
	state     StackState
	plan      PlanOutcome
	applied   map[string]string
	destroyed bool
}

func (f *fakeBackend) CheckEnvironment(_ context.Context, req StackRequest) error {
	f.lastReq = req
	return nil
}

func (f *fakeBackend) Plan(_ context.Context, req StackRequest) (PlanOutcome, error) {
	f.lastReq = req
	return f.plan, nil
}

func (f *fakeBackend) Apply(_ context.Context, req StackRequest) (map[string]string, error) {
	f.lastReq = req
	out := map[string]string{"guardian_hash": req.DesiredHash}
	if bucket, ok := req.Variables["bucket"].(string); ok {
		out["bucket"] = bucket
	}
	if secretID, ok := req.Variables["secret_id"].(string); ok {
		out["secret_id"] = secretID
		out["secret"] = secretID
	}
	if name, ok := req.Variables["name"].(string); ok {
		out["service"] = name
		out["network_name"] = name
	}
	switch req.Blueprint {
	case "objectstore":
		out["type"] = "gcs"
	case "network":
		out["type"] = "vpc"
	case "secret":
		out["type"] = "secretmanager"
	case "compute":
		out["type"] = "cloudrun"
	case "sqldatabase":
		out["type"] = "cloudsql"
		if engine, ok := req.Variables["engine"].(string); ok {
			out["engine"] = engine
		}
		if name, ok := req.Variables["name"].(string); ok {
			out["id"] = name
		}
	case "volume":
		out["type"] = "filestore"
		if name, ok := req.Variables["name"].(string); ok {
			out["volume_id"] = name
		}
	case "loadbalancer":
		out["type"] = "http-lb"
		out["lb_ip"] = "203.0.113.10"
	case "observability":
		out["type"] = "gateway"
		out["endpoint"] = "gateway.example:443"
	}
	if f.applied != nil {
		out = f.applied
	}
	return out, nil
}

func (f *fakeBackend) Destroy(_ context.Context, req StackRequest) error {
	f.lastReq = req
	f.destroyed = true
	return nil
}

func (f *fakeBackend) Get(_ context.Context, req StackRequest) (StackState, error) {
	f.lastReq = req
	return f.state, nil
}

func objectStoreInput() registry.AssetInput {
	return registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "api",
		Asset: taskdomain.AbstractAsset{
			Type: "ObjectStore",
			Name: "media",
			Properties: map[string]any{
				"engine":     "gcs",
				"versioning": true,
			},
		},
		Target: targetForTest("strata-gcp-dev", "europe-west1"),
	}
}

func TestBlueprintsExist(t *testing.T) {
	want := map[string][]string{
		"objectstore":   {"google_storage_bucket", "guardian-hash", "guardian_hash"},
		"network":       {"google_compute_network", "google_compute_subnetwork", "google_service_networking_connection"},
		"secret":        {"google_secret_manager_secret", "google_secret_manager_secret_version", "sensitive"},
		"compute":       {"google_cloud_run_v2_service", "vpc_access", "guardian_hash"},
		"sqldatabase":   {"google_sql_database_instance", "database_version", "deletion_protection"},
		"volume":        {"google_filestore_instance", "file_shares", "capacity_gb"},
		"loadbalancer":  {"google_compute_backend_service", "SERVERLESS", "global_forwarding_rule"},
		"observability": {"google_cloud_run_v2_service", "OTEL_CONFIG", "endpoint"},
	}
	for name, needles := range want {
		files, err := BlueprintFiles(name)
		if err != nil {
			t.Fatalf("BlueprintFiles(%q): %v", name, err)
		}
		main, ok := files["main.tf"]
		if !ok {
			t.Fatalf("%s blueprint has no main.tf: %v", name, files)
		}
		for _, needle := range needles {
			if !strings.Contains(main, needle) {
				t.Errorf("%s blueprint missing %q", name, needle)
			}
		}
	}
	names, err := BlueprintNames()
	if err != nil {
		t.Fatalf("BlueprintNames: %v", err)
	}
	if len(names) != len(want) {
		t.Fatalf("blueprints = %v, want %d entries", names, len(want))
	}
}

func TestBucketName(t *testing.T) {
	got := bucketName("Demo", "API", "Media_Bucket")
	if got != "guardian-demo-api-media-bucket" {
		t.Fatalf("bucketName = %q", got)
	}
	long := bucketName(strings.Repeat("partition-", 8), strings.Repeat("intent-", 8), "asset")
	if len(long) > 63 {
		t.Fatalf("bucket name too long: %d", len(long))
	}
	if strings.HasSuffix(long, "-") {
		t.Fatalf("bucket name ends with dash: %q", long)
	}
}

func TestObjectStoreApplyUsesBlueprintAndOutputs(t *testing.T) {
	backend := &fakeBackend{}
	driver := &ObjectStoreDriver{baseDriver{backend: backend}}

	result, err := driver.Apply(context.Background(), objectStoreInput())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if backend.lastReq.Blueprint != "objectstore" {
		t.Errorf("blueprint = %q", backend.lastReq.Blueprint)
	}
	if backend.lastReq.Project != "strata-gcp-dev" || backend.lastReq.Region != "europe-west1" {
		t.Errorf("project/region = %q/%q", backend.lastReq.Project, backend.lastReq.Region)
	}
	if backend.lastReq.Variables["versioning"] != true {
		t.Errorf("versioning = %v", backend.lastReq.Variables["versioning"])
	}
	if backend.lastReq.DesiredHash == "" {
		t.Error("desired hash is empty")
	}
	if result.Outputs["type"] != "gcs" || result.Outputs["bucket"] == "" {
		t.Errorf("outputs = %v", result.Outputs)
	}
}

func TestObjectStoreDiff(t *testing.T) {
	backend := &fakeBackend{state: StackState{Exists: false}}
	driver := &ObjectStoreDriver{baseDriver{backend: backend}}

	report, err := driver.Diff(context.Background(), objectStoreInput())
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if report.Status != "Changed" {
		t.Fatalf("missing bucket should be Changed, got %q", report.Status)
	}

	backend.state = StackState{
		Exists:  true,
		Outputs: map[string]string{"guardian_hash": shortHash(backend.lastReq.DesiredHash)},
	}
	report, err = driver.Diff(context.Background(), objectStoreInput())
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("matching hash should be InSync, got %q", report.Status)
	}
}

func TestObjectStoreDestroy(t *testing.T) {
	backend := &fakeBackend{}
	driver := &ObjectStoreDriver{baseDriver{backend: backend}}
	if err := driver.Destroy(context.Background(), objectStoreInput()); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if !backend.destroyed {
		t.Fatal("backend was not asked to destroy")
	}
}

func TestObjectStoreExternalEndpointIsNoop(t *testing.T) {
	backend := &fakeBackend{}
	driver := &ObjectStoreDriver{baseDriver{backend: backend}}
	in := objectStoreInput()
	in.Asset.Properties["endpoint"] = "https://storage.example.com"

	result, err := driver.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Outputs["type"] != "external" {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	if err := driver.Destroy(context.Background(), in); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if backend.destroyed {
		t.Fatal("external endpoint must not be destroyed")
	}
}
