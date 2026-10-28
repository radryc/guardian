package driverutil

import (
	"strings"
	"testing"

	targetdomain "github.com/radryc/guardian/internal/domain/target"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/registry"
)

func TestCanonicalServiceIdentity(t *testing.T) {
	identity := CanonicalServiceIdentity("doctor", "ingest", "ingest")
	if identity.Name != "doctor.ingest.ingest" || identity.FullName != identity.Name || identity.Truncated {
		t.Fatalf("unexpected identity: %+v", identity)
	}
}

func TestCanonicalServiceIdentityTruncatesDeterministically(t *testing.T) {
	long := strings.Repeat("a", 220)
	first := CanonicalServiceIdentity(long, "intent", "asset")
	second := CanonicalServiceIdentity(long, "intent", "asset")
	if !first.Truncated {
		t.Fatal("expected truncated identity")
	}
	if first.Name != second.Name {
		t.Fatalf("non-deterministic names: %q vs %q", first.Name, second.Name)
	}
	if len(first.Name) > MaxServiceNameLength {
		t.Fatalf("name length = %d, want <= %d", len(first.Name), MaxServiceNameLength)
	}
	if first.FullName != long+".intent.asset" {
		t.Fatalf("full name = %q", first.FullName)
	}
}

func lbAssetInput(name string) registry.AssetInput {
	return registry.AssetInput{
		PartitionName: "doctor",
		IntentName:    "ingest",
		PusherName:    "k8s-main",
		Asset:         taskdomain.AbstractAsset{Type: "LoadBalancer", Name: name},
		Target:        targetdomain.Placement{Cluster: "main"},
	}
}

func TestApplyLBOtelEnvUsesCanonicalIdentity(t *testing.T) {
	t.Setenv("GUARDIAN_OTEL_ENDPOINT", "collector.otel.svc.cluster.local:4317")
	t.Setenv("GUARDIAN_OTEL_INSECURE", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")

	env := ApplyLBOtelEnv(nil, lbAssetInput("ingest-edge"))
	if got := env["LB_OTEL_ENDPOINT"]; got != "collector.otel.svc.cluster.local:4317" {
		t.Fatalf("LB_OTEL_ENDPOINT = %q", got)
	}
	if got := env["LB_OTEL_SERVICE_NAME"]; got != "doctor.ingest.ingest-edge" {
		t.Fatalf("LB_OTEL_SERVICE_NAME = %q", got)
	}
	if got := env["OTEL_SERVICE_NAME"]; got != "doctor.ingest.ingest-edge" {
		t.Fatalf("OTEL_SERVICE_NAME = %q", got)
	}
	if got := env["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"]; got != "http://collector.otel.svc.cluster.local:4318/v1/traces" {
		t.Fatalf("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT = %q", got)
	}
	if env["OTEL_METRICS_EXPORTER"] != "none" || env["OTEL_LOGS_EXPORTER"] != "none" {
		t.Fatalf("auto-exporters not disabled: %#v", env)
	}
}

func TestApplyLBOtelEnvPreservesExplicitValues(t *testing.T) {
	t.Setenv("GUARDIAN_OTEL_ENDPOINT", "collector.otel.svc.cluster.local:4317")
	t.Setenv("GUARDIAN_OTEL_INSECURE", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")

	env := ApplyLBOtelEnv(map[string]string{
		"OTEL_METRICS_EXPORTER":              "otlp",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://custom:4318/v1/traces",
	}, lbAssetInput("lb-edge"))
	if env["OTEL_METRICS_EXPORTER"] != "otlp" {
		t.Fatalf("explicit metrics exporter overwritten: %#v", env)
	}
	if env["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"] != "http://custom:4318/v1/traces" {
		t.Fatalf("explicit traces endpoint overwritten: %#v", env)
	}
}

func TestApplyObservabilityEnvInjectsIdentityWithoutTargetingAsset(t *testing.T) {
	in := registry.AssetInput{
		PartitionName: "doctor",
		IntentName:    "query",
		PusherName:    "k8s-main",
		Asset:         taskdomain.AbstractAsset{Type: "Compute", Name: "query"},
		Target:        targetdomain.Placement{Cluster: "main"},
	}
	env := ApplyObservabilityEnv(map[string]string{"EXISTING": "1"}, in, "kubernetes", "otel-collector.otel.svc.cluster.local:4317")
	if got := env["OTEL_SERVICE_NAME"]; got != "doctor.query.query" {
		t.Fatalf("OTEL_SERVICE_NAME = %q, want doctor.query.query", got)
	}
	attrs := parseOtelResourceAttrs(env["OTEL_RESOURCE_ATTRIBUTES"])
	if attrs["guardian.asset.type"] != "Compute" {
		t.Fatalf("guardian.asset.type = %q, want Compute", attrs["guardian.asset.type"])
	}
	if attrs["guardian.identity.kind"] != "managed" {
		t.Fatalf("guardian.identity.kind = %q, want managed", attrs["guardian.identity.kind"])
	}
	if _, ok := env["OTEL_EXPORTER_OTLP_ENDPOINT"]; ok {
		t.Fatalf("endpoint injected without a targeting asset: %#v", env)
	}
	if env["EXISTING"] != "1" {
		t.Fatalf("explicit env was not preserved: %#v", env)
	}
}

func TestApplyObservabilityEnvUsesServiceNameOverride(t *testing.T) {
	in := registry.AssetInput{
		PartitionName: "doctor",
		IntentName:    "query",
		PusherName:    "k8s-main",
		Asset:         taskdomain.AbstractAsset{Type: "Compute", Name: "query"},
		Assets: map[string]taskdomain.AbstractAsset{
			"observability": {
				Type:       "Observability",
				Name:       "observability",
				Properties: map[string]any{"mode": "auto", "targets": []string{"query"}, "serviceName": "custom.service"},
			},
		},
		Target: targetdomain.Placement{Cluster: "main"},
	}
	env := ApplyObservabilityEnv(nil, in, "kubernetes", "otel-collector.otel.svc.cluster.local:4317")
	if got := env["OTEL_SERVICE_NAME"]; got != "doctor.query.query" {
		t.Fatalf("OTEL_SERVICE_NAME = %q", got)
	}
	attrs := parseOtelResourceAttrs(env["OTEL_RESOURCE_ATTRIBUTES"])
	if got := attrs["service.alias"]; got != "custom.service" {
		t.Fatalf("service.alias = %q", got)
	}
	if got := env["OTEL_PROPAGATORS"]; got != "tracecontext,baggage,xray" {
		t.Fatalf("OTEL_PROPAGATORS = %q", got)
	}
	if got := env["OTEL_TRACES_SAMPLER"]; got != "parentbased_always_on" {
		t.Fatalf("OTEL_TRACES_SAMPLER = %q", got)
	}
}

func TestApplyOtelEnvPreservesExplicitValues(t *testing.T) {
	in := registry.AssetInput{
		PartitionName: "doctor",
		IntentName:    "query",
		PusherName:    "k8s-main",
		Asset: taskdomain.AbstractAsset{
			Type: "Compute",
			Name: "query",
		},
		Target: targetdomain.Placement{Cluster: "main"},
	}
	env := ApplyOtelEnv(map[string]string{
		"OTEL_SERVICE_NAME":        "custom-service",
		"OTEL_RESOURCE_ATTRIBUTES": "guardian.asset=explicit,service.name=explicit",
	}, in, "kubernetes")

	if got := env["OTEL_SERVICE_NAME"]; got != "doctor.query.query" {
		t.Fatalf("OTEL_SERVICE_NAME = %q", got)
	}
	attrs := parseOtelResourceAttrs(env["OTEL_RESOURCE_ATTRIBUTES"])
	if got := attrs["service.name"]; got != "doctor.query.query" {
		t.Fatalf("service.name = %q", got)
	}
	if got := attrs["service.alias"]; got != "custom-service" {
		t.Fatalf("service.alias = %q", got)
	}
	if got := attrs["guardian.asset"]; got != "explicit" {
		t.Fatalf("explicit guardian.asset = %q", got)
	}
	if got := attrs["guardian.partition"]; got != "doctor" {
		t.Fatalf("guardian.partition = %q", got)
	}
	if got := attrs["guardian.pusher"]; got != "k8s-main" {
		t.Fatalf("guardian.pusher = %q", got)
	}
	if got := attrs["guardian.cluster"]; got != "main" {
		t.Fatalf("guardian.cluster = %q", got)
	}
	if got := attrs["k8s.cluster.name"]; got != "main" {
		t.Fatalf("k8s.cluster.name = %q", got)
	}
	if got := attrs["guardian.service.full_name"]; got != "doctor.query.query" {
		t.Fatalf("guardian.service.full_name = %q", got)
	}
}
