package gcpdriver

import (
	"context"
	"strings"
	"testing"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/registry"
)

func sqlInput(properties map[string]any) registry.AssetInput {
	return registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "api",
		Asset: taskdomain.AbstractAsset{
			Type:       "SQLDatabase",
			Name:       "db",
			Properties: properties,
		},
		Assets: map[string]taskdomain.AbstractAsset{
			"vpc": {Type: "Network", Name: "vpc"},
		},
		Target: targetForTest("strata-gcp-dev", "europe-west1"),
	}
}

func TestSQLEngineMapping(t *testing.T) {
	cases := []struct {
		engine  string
		version string
		want    string
		scheme  string
	}{
		{"postgres", "16", "POSTGRES_16", "postgres"},
		{"postgresql", "15.4", "POSTGRES_15", "postgres"},
		{"mysql", "8.0", "MYSQL_8_0", "mysql"},
		{"mysql", "", "MYSQL_8_0", "mysql"},
		{"sqlserver", "2019", "SQLSERVER_2019_STANDARD", "sqlserver"},
		{"postgres", "POSTGRES_16", "POSTGRES_16", "postgres"},
	}
	for _, testCase := range cases {
		info, err := resolveSQLEngine(&assetdefs.SQLDatabaseSpec{Engine: testCase.engine, Version: testCase.version})
		if err != nil {
			t.Fatalf("%s: %v", testCase.engine, err)
		}
		if info.Version != testCase.want || info.Scheme != testCase.scheme {
			t.Errorf("%s/%s => %+v, want version %s scheme %s", testCase.engine, testCase.version, info, testCase.want, testCase.scheme)
		}
	}
	if _, err := resolveSQLEngine(&assetdefs.SQLDatabaseSpec{Engine: "oracle"}); err == nil {
		t.Fatal("expected unsupported engine error")
	}
}

func TestSQLApply(t *testing.T) {
	backend := &fakeBackend{}
	driver := &SQLDatabaseDriver{baseDriver: baseDriver{backend: backend}, typeName: "SQLDatabase"}
	in := sqlInput(map[string]any{
		"engine":   "postgres",
		"version":  "16",
		"database": "app",
		"user":     "app",
		"networks": []any{"vpc"},
	})

	result, err := driver.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	req := backend.lastReq
	if req.Blueprint != "sqldatabase" {
		t.Errorf("blueprint = %q", req.Blueprint)
	}
	if req.Variables["engine"] != "POSTGRES" || req.Variables["database_version"] != "POSTGRES_16" {
		t.Errorf("engine/version = %v/%v", req.Variables["engine"], req.Variables["database_version"])
	}
	if req.Variables["network"] != "guardian-demo-api-vpc" {
		t.Errorf("network = %v", req.Variables["network"])
	}
	if req.Variables["port"] != 5432 {
		t.Errorf("port = %v", req.Variables["port"])
	}
	if result.Outputs["type"] != "cloudsql" || result.Outputs["engine"] != "POSTGRES" {
		t.Errorf("outputs = %v", result.Outputs)
	}
}

func TestSQLValidateRejectsUnsupportedEngine(t *testing.T) {
	driver := &SQLDatabaseDriver{baseDriver: baseDriver{backend: &fakeBackend{}}, typeName: "SQLDatabase"}
	if err := driver.Validate(map[string]any{"engine": "oracle"}); err != nil {
		t.Fatalf("generic validation should pass, engine support is checked at request time: %v", err)
	}
	if _, _, err := driver.request(sqlInput(map[string]any{"engine": "oracle"}), &assetdefs.SQLDatabaseSpec{Engine: "oracle"}); err == nil {
		t.Fatal("expected unsupported engine error at request time")
	}
}

func TestCapacityGB(t *testing.T) {
	cases := map[string]int{
		"":      100,
		"10Gi":  10,
		"1Ti":   1024,
		"512Mi": 1,
		"100":   100,
		"2T":    2048,
	}
	for input, want := range cases {
		if got := capacityGB(input); got != want {
			t.Errorf("capacityGB(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestFilestoreTier(t *testing.T) {
	cases := map[string]string{
		"":           "BASIC_HDD",
		"standard":   "BASIC_HDD",
		"premium":    "BASIC_SSD",
		"regional":   "REGIONAL",
		"enterprise": "REGIONAL",
	}
	for input, want := range cases {
		if got := filestoreTier(input); got != want {
			t.Errorf("filestoreTier(%q) = %q, want %q", input, got, want)
		}
	}
}

func volumeInput(properties map[string]any) registry.AssetInput {
	return registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "api",
		Asset: taskdomain.AbstractAsset{
			Type:       "Volume",
			Name:       "data",
			Properties: properties,
		},
		Target: targetForTest("strata-gcp-dev", "europe-west1"),
	}
}

func TestVolumeApply(t *testing.T) {
	backend := &fakeBackend{}
	driver := &VolumeDriver{baseDriver{backend: backend}}

	result, err := driver.Apply(context.Background(), volumeInput(map[string]any{
		"size":  "1Ti",
		"class": "premium",
	}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	req := backend.lastReq
	if req.Blueprint != "volume" {
		t.Errorf("blueprint = %q", req.Blueprint)
	}
	if req.Variables["capacity_gb"] != 1024 || req.Variables["tier"] != "BASIC_SSD" {
		t.Errorf("variables = %v", req.Variables)
	}
	if req.Variables["location"] != "europe-west1-a" {
		t.Errorf("location = %v", req.Variables["location"])
	}
	if result.Outputs["type"] != "filestore" {
		t.Errorf("outputs = %v", result.Outputs)
	}
}

func TestVolumeEphemeralAndAdoptedAreNoops(t *testing.T) {
	backend := &fakeBackend{}
	driver := &VolumeDriver{baseDriver{backend: backend}}

	result, err := driver.Apply(context.Background(), volumeInput(map[string]any{"ephemeral": true}))
	if err != nil {
		t.Fatalf("ephemeral Apply: %v", err)
	}
	if result.Outputs["type"] != "ephemeral" {
		t.Errorf("ephemeral outputs = %v", result.Outputs)
	}

	result, err = driver.Apply(context.Background(), volumeInput(map[string]any{"existingID": "projects/p/locations/r/instances/fs"}))
	if err != nil {
		t.Fatalf("adopted Apply: %v", err)
	}
	if result.Outputs["adopted"] != "true" || result.Outputs["volume_id"] == "" {
		t.Errorf("adopted outputs = %v", result.Outputs)
	}
	if backend.lastReq.Blueprint != "" {
		t.Errorf("no-op volumes must not touch the backend: %+v", backend.lastReq)
	}
}

func lbInput(properties map[string]any) registry.AssetInput {
	return registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "api",
		Asset: taskdomain.AbstractAsset{
			Type:       "LoadBalancer",
			Name:       "edge",
			Properties: properties,
		},
		Assets: map[string]taskdomain.AbstractAsset{
			"app": {Type: "Compute", Name: "app"},
		},
		Target: targetForTest("strata-gcp-dev", "europe-west1"),
	}
}

func TestLoadBalancerApply(t *testing.T) {
	backend := &fakeBackend{}
	driver := &LoadBalancerDriver{baseDriver{backend: backend}}

	result, err := driver.Apply(context.Background(), lbInput(map[string]any{
		"targets":   []any{"app"},
		"listeners": []any{map[string]any{"port": 8080, "protocol": "HTTP"}},
	}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	req := backend.lastReq
	if req.Blueprint != "loadbalancer" {
		t.Errorf("blueprint = %q", req.Blueprint)
	}
	targets, ok := req.Variables["targets"].([]string)
	if !ok || len(targets) != 1 || targets[0] != "guardian-demo-api-app" {
		t.Errorf("targets = %v", req.Variables["targets"])
	}
	if req.Variables["port"] != 8080 || req.Variables["name"] != "guardian-demo-api-edge" {
		t.Errorf("variables = %v", req.Variables)
	}
	if result.Outputs["type"] != "http-lb" || result.Outputs["lb_ip"] == "" {
		t.Errorf("outputs = %v", result.Outputs)
	}
}

func TestLoadBalancerRejectsNonHTTP(t *testing.T) {
	driver := &LoadBalancerDriver{baseDriver{backend: &fakeBackend{}}}
	err := driver.ValidateInContext(map[string]any{
		"targets":   []any{"app"},
		"listeners": []any{map[string]any{"port": 443, "protocol": "HTTPS"}},
	}, map[string]taskdomain.AbstractAsset{"app": {Type: "Compute", Name: "app"}})
	if err == nil || !strings.Contains(err.Error(), "HTTP only") {
		t.Fatalf("expected HTTP-only rejection, got %v", err)
	}
}

func TestLoadBalancerAdopted(t *testing.T) {
	backend := &fakeBackend{}
	driver := &LoadBalancerDriver{baseDriver{backend: backend}}
	in := registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "api",
		Asset: taskdomain.AbstractAsset{
			Type: "LoadBalancer",
			Name: "edge",
			Properties: map[string]any{
				"existingName": "prod-edge",
				"listeners":    []any{map[string]any{"port": 80}},
			},
		},
		Target: targetForTest("strata-gcp-dev", "europe-west1"),
	}
	result, err := driver.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Outputs["adopted"] != "true" || result.Outputs["lb_name"] != "prod-edge" {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	if backend.lastReq.Blueprint != "" {
		t.Fatalf("adopted LB must not touch the backend: %+v", backend.lastReq)
	}
}

func observabilityInput(properties map[string]any) registry.AssetInput {
	return registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "api",
		Asset: taskdomain.AbstractAsset{
			Type:       "Observability",
			Name:       "otel",
			Properties: properties,
		},
		Assets: map[string]taskdomain.AbstractAsset{
			"app": {Type: "Compute", Name: "app"},
		},
		Target: targetForTest("strata-gcp-dev", "europe-west1"),
	}
}

func TestObservabilityModes(t *testing.T) {
	cases := []struct {
		properties map[string]any
		want       string
	}{
		{map[string]any{"mode": "env", "endpoint": "http://otel:4317"}, "env"},
		{map[string]any{"targets": []any{"app"}}, "sidecar"},
		{map[string]any{"mode": "gateway", "exporters": []any{"googlecloud"}}, "gateway"},
		{map[string]any{"mode": "auto", "targets": []any{"app"}}, "gateway"},
	}
	for _, testCase := range cases {
		spec := mustObservabilitySpec(t, testCase.properties)
		if got := observabilityMode(spec); got != testCase.want {
			t.Errorf("mode(%v) = %q, want %q", testCase.properties, got, testCase.want)
		}
	}
}

func TestObservabilityEnvAndSidecarAreConfigOnly(t *testing.T) {
	backend := &fakeBackend{}
	driver := &ObservabilityDriver{baseDriver{backend: backend}}

	result, err := driver.Apply(context.Background(), observabilityInput(map[string]any{"mode": "env", "endpoint": "http://otel:4317"}))
	if err != nil {
		t.Fatalf("env Apply: %v", err)
	}
	if result.Outputs["type"] != "env" || result.Outputs["endpoint"] != "http://otel:4317" {
		t.Errorf("env outputs = %v", result.Outputs)
	}

	result, err = driver.Apply(context.Background(), observabilityInput(map[string]any{"targets": []any{"app"}}))
	if err != nil {
		t.Fatalf("sidecar Apply: %v", err)
	}
	if result.Outputs["type"] != "sidecar" || result.Outputs["endpoint"] != defaultOTLPEndpoint {
		t.Errorf("sidecar outputs = %v", result.Outputs)
	}
	if backend.lastReq.Blueprint != "" {
		t.Errorf("config-only modes must not touch the backend: %+v", backend.lastReq)
	}
}

func TestObservabilityGatewayApply(t *testing.T) {
	backend := &fakeBackend{}
	driver := &ObservabilityDriver{baseDriver{backend: backend}}

	result, err := driver.Apply(context.Background(), observabilityInput(map[string]any{"mode": "gateway"}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	req := backend.lastReq
	if req.Blueprint != "observability" {
		t.Errorf("blueprint = %q", req.Blueprint)
	}
	config, ok := req.Variables["config"].(string)
	if !ok || !strings.Contains(config, "googlecloud") {
		t.Errorf("config = %v", req.Variables["config"])
	}
	if req.Variables["port"] != 4318 {
		t.Errorf("port = %v", req.Variables["port"])
	}
	if result.Outputs["type"] != "gateway" {
		t.Errorf("outputs = %v", result.Outputs)
	}
}

func TestObservabilityConfigReference(t *testing.T) {
	backend := &fakeBackend{}
	driver := &ObservabilityDriver{baseDriver{backend: backend}}
	in := observabilityInput(map[string]any{"mode": "gateway", "config": "otel-config"})
	in.Assets["otel-config"] = taskdomain.AbstractAsset{
		Type: "Config",
		Name: "otel-config",
		Properties: map[string]any{
			"format":  "yaml",
			"content": "receivers:\n  otlp:\n    protocols:\n      grpc:\n",
		},
	}
	if _, err := driver.Apply(context.Background(), in); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	config, _ := backend.lastReq.Variables["config"].(string)
	if !strings.Contains(config, "receivers:") {
		t.Errorf("config not resolved from the Config asset: %q", config)
	}
}

func TestAssetTypeMap(t *testing.T) {
	got := assetTypeMap(map[string]taskdomain.AbstractAsset{"a": {Type: assetdomain.TypeCompute}})
	if got["a"] != assetdomain.TypeCompute {
		t.Fatalf("assetTypeMap = %v", got)
	}
}

func mustObservabilitySpec(t *testing.T, properties map[string]any) *assetdefs.ObservabilitySpec {
	t.Helper()
	typed, _, err := assetdefs.Decode(assetdomain.Spec{Type: assetdomain.TypeObservability, Properties: properties})
	if err != nil {
		t.Fatalf("decode observability spec: %v", err)
	}
	spec, ok := typed.(*assetdefs.ObservabilitySpec)
	if !ok {
		t.Fatalf("unexpected spec type %T", typed)
	}
	return spec
}
