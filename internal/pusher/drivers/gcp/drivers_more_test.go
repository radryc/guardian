package gcpdriver

import (
	"context"
	"errors"
	"strings"
	"testing"

	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/registry"
)

type staticResolver struct {
	value string
	err   error
}

func (r staticResolver) Resolve(_ context.Context, _ string) (string, error) {
	return r.value, r.err
}

func networkInput() registry.AssetInput {
	return registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "api",
		Asset: taskdomain.AbstractAsset{
			Type: "Network",
			Name: "vpc",
			Properties: map[string]any{
				"cidr":     "10.10.0.0/16",
				"internal": true,
			},
		},
		Target: targetForTest("strata-gcp-dev", "europe-west1"),
	}
}

func TestNetworkApply(t *testing.T) {
	backend := &fakeBackend{}
	driver := &NetworkDriver{baseDriver{backend: backend}}

	result, err := driver.Apply(context.Background(), networkInput())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	req := backend.lastReq
	if req.Blueprint != "network" {
		t.Errorf("blueprint = %q", req.Blueprint)
	}
	if req.Variables["cidr"] != "10.10.0.0/16" || req.Variables["internal"] != true {
		t.Errorf("variables = %v", req.Variables)
	}
	if req.Variables["name"] != "guardian-demo-api-vpc" {
		t.Errorf("name = %v", req.Variables["name"])
	}
	if req.Variables["subnet"] != "guardian-demo-api-vpc-subnet" {
		t.Errorf("subnet = %v", req.Variables["subnet"])
	}
	if result.Outputs["type"] != "vpc" {
		t.Errorf("outputs = %v", result.Outputs)
	}
}

func TestNetworkDiff(t *testing.T) {
	backend := &fakeBackend{state: StackState{Exists: false}}
	driver := &NetworkDriver{baseDriver{backend: backend}}

	report, err := driver.Diff(context.Background(), networkInput())
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if report.Status != "Changed" {
		t.Fatalf("missing VPC should be Changed, got %q", report.Status)
	}

	backend.state = StackState{Exists: true, Outputs: map[string]string{"guardian_hash": shortHash(backend.lastReq.DesiredHash)}}
	report, err = driver.Diff(context.Background(), networkInput())
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("matching hash should be InSync, got %q", report.Status)
	}
}

func TestNetworkValidateRejectsBadCIDR(t *testing.T) {
	driver := &NetworkDriver{baseDriver{backend: &fakeBackend{}}}
	if err := driver.Validate(map[string]any{"cidr": "not-a-cidr"}); err == nil {
		t.Fatal("expected an invalid CIDR to be rejected")
	}
}

func secretInput(properties map[string]any) registry.AssetInput {
	return registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "api",
		Asset: taskdomain.AbstractAsset{
			Type:       "Secret",
			Name:       "db-password",
			Properties: properties,
		},
		Target: targetForTest("strata-gcp-dev", "europe-west1"),
	}
}

func TestSecretApplyResolvesReference(t *testing.T) {
	backend := &fakeBackend{}
	driver := &SecretDriver{baseDriver{backend: backend, resolver: staticResolver{value: "resolved-value"}}}

	result, err := driver.Apply(context.Background(), secretInput(map[string]any{"secretRef": "monofs-secret://shared/db"}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	req := backend.lastReq
	if req.Blueprint != "secret" {
		t.Errorf("blueprint = %q", req.Blueprint)
	}
	if req.Variables["value"] != "resolved-value" {
		t.Errorf("value = %v", req.Variables["value"])
	}
	if req.Variables["secret_id"] != "guardian-demo-api-db-password" {
		t.Errorf("secret_id = %v", req.Variables["secret_id"])
	}
	if result.Outputs["type"] != "secretmanager" {
		t.Errorf("outputs = %v", result.Outputs)
	}
	if _, leaked := result.Outputs["value"]; leaked {
		t.Error("secret value must not appear in outputs")
	}
}

func TestSecretApplyPropagatesResolverError(t *testing.T) {
	backend := &fakeBackend{}
	driver := &SecretDriver{baseDriver{backend: backend, resolver: staticResolver{err: errors.New("missing secret")}}}
	if _, err := driver.Apply(context.Background(), secretInput(map[string]any{"secretRef": "monofs-secret://shared/db"})); err == nil {
		t.Fatal("expected resolver error")
	}
}

func TestSecretAdoptedIsNoop(t *testing.T) {
	backend := &fakeBackend{}
	driver := &SecretDriver{baseDriver{backend: backend}}

	result, err := driver.Apply(context.Background(), secretInput(map[string]any{"existingSecret": "prod-db-password"}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Outputs["adopted"] != "true" || result.Outputs["secret_id"] != "prod-db-password" {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	if err := driver.Destroy(context.Background(), secretInput(map[string]any{"existingSecret": "prod-db-password"})); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if backend.destroyed {
		t.Fatal("adopted secret must not be destroyed")
	}
	if backend.lastReq.Blueprint != "" {
		t.Fatalf("adopted secret must not touch the backend: %+v", backend.lastReq)
	}
}

func TestSecretDiff(t *testing.T) {
	backend := &fakeBackend{state: StackState{Exists: false}}
	driver := &SecretDriver{baseDriver{backend: backend}}
	in := secretInput(map[string]any{"value": "s3cr3t"})

	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if report.Status != "Changed" {
		t.Fatalf("missing secret should be Changed, got %q", report.Status)
	}
	backend.state = StackState{Exists: true, Outputs: map[string]string{"guardian_hash": shortHash(backend.lastReq.DesiredHash)}}
	report, err = driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("matching hash should be InSync, got %q", report.Status)
	}
}

func computeInput(properties map[string]any) registry.AssetInput {
	return registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "api",
		Asset: taskdomain.AbstractAsset{
			Type:       "Compute",
			Name:       "app",
			Properties: properties,
		},
		Assets: map[string]taskdomain.AbstractAsset{
			"vpc": {Type: "Network", Name: "vpc"},
		},
		Target: targetForTest("strata-gcp-dev", "europe-west1"),
	}
}

func TestComputeApplyMapsCloudRunVariables(t *testing.T) {
	backend := &fakeBackend{}
	driver := &ComputeDriver{baseDriver{backend: backend}}
	in := computeInput(map[string]any{
		"image":    "europe-west1-docker.pkg.dev/p/r/app:1",
		"replicas": 2,
		"env":      map[string]any{"LOG_LEVEL": "info", "PORT": 8080},
		"command":  []any{"serve"},
		"args":     []any{"--verbose"},
		"ports":    []any{map[string]any{"containerPort": 9090}},
		"networks": []any{"vpc"},
		"resources": map[string]any{
			"limits": map[string]any{"cpu": "1", "memory": "512Mi"},
		},
	})

	result, err := driver.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	req := backend.lastReq
	if req.Blueprint != "compute" {
		t.Errorf("blueprint = %q", req.Blueprint)
	}
	if req.Variables["image"] != "europe-west1-docker.pkg.dev/p/r/app:1" {
		t.Errorf("image = %v", req.Variables["image"])
	}
	if req.Variables["replicas"] != 2 {
		t.Errorf("replicas = %v", req.Variables["replicas"])
	}
	env := req.Variables["env"].(map[string]string)
	if env["LOG_LEVEL"] != "info" || env["PORT"] != "8080" {
		t.Errorf("env = %v", env)
	}
	if req.Variables["port"] != 9090 {
		t.Errorf("port = %v", req.Variables["port"])
	}
	if req.Variables["network"] != "guardian-demo-api-vpc" || req.Variables["subnet"] != "guardian-demo-api-vpc-subnet" {
		t.Errorf("network/subnet = %v/%v", req.Variables["network"], req.Variables["subnet"])
	}
	if req.Variables["cpu"] != "1" || req.Variables["memory"] != "512Mi" {
		t.Errorf("resources = %v/%v", req.Variables["cpu"], req.Variables["memory"])
	}
	if len(req.Variables["name"].(string)) > 49 {
		t.Errorf("Cloud Run name too long: %q", req.Variables["name"])
	}
	if result.Outputs["type"] != "cloudrun" {
		t.Errorf("outputs = %v", result.Outputs)
	}
}

func TestComputeValidateRejectsUnsupported(t *testing.T) {
	driver := &ComputeDriver{baseDriver{backend: &fakeBackend{}}}
	cases := []map[string]any{
		{"image": "img", "privileged": true},
		{"image": "img", "volumeMounts": []any{map[string]any{"volume": "v", "path": "/data"}}},
		{"image": "img", "configMounts": []any{map[string]any{"config": "c", "path": "/etc/c"}}},
		{"image": "img", "hostBindMounts": []any{map[string]any{"source": "/a", "target": "/b"}}},
		{"image": "img", "gpus": "1"},
		{"image": "img", "capabilities": []any{"NET_ADMIN"}},
		{"image": "img", "imagePullPolicy": "Never"},
		{"image": "img", "cluster": "main"},
	}
	for _, props := range cases {
		if err := driver.Validate(props); err == nil {
			t.Errorf("expected %v to be rejected", props)
		}
	}
	if err := driver.Validate(map[string]any{"image": "img"}); err != nil {
		t.Errorf("minimal compute should validate: %v", err)
	}
}

func TestComputeRejectsMissingNetworkReference(t *testing.T) {
	driver := &ComputeDriver{baseDriver{backend: &fakeBackend{}}}
	in := computeInput(map[string]any{"image": "img", "networks": []any{"missing"}})
	if err := driver.Check(context.Background(), in); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected a missing network error, got %v", err)
	}
}

func TestComputeAdoptedIsNoop(t *testing.T) {
	backend := &fakeBackend{}
	driver := &ComputeDriver{baseDriver{backend: backend}}
	in := computeInput(map[string]any{
		"image":               "img",
		"observeExisting":     true,
		"existingServiceName": "existing-app",
	})
	result, err := driver.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Outputs["adopted"] != "true" || result.Outputs["service"] != "existing-app" {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	if backend.lastReq.Blueprint != "" {
		t.Fatalf("adopted compute must not touch the backend: %+v", backend.lastReq)
	}
}

func TestComputeDiff(t *testing.T) {
	backend := &fakeBackend{state: StackState{Exists: false}}
	driver := &ComputeDriver{baseDriver{backend: backend}}
	in := computeInput(map[string]any{"image": "img"})

	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if report.Status != "Changed" {
		t.Fatalf("missing service should be Changed, got %q", report.Status)
	}
	backend.state = StackState{Exists: true, Outputs: map[string]string{"guardian_hash": shortHash(backend.lastReq.DesiredHash)}}
	report, err = driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("matching hash should be InSync, got %q", report.Status)
	}
}
