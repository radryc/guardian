package driverutil

import (
	"strings"
	"testing"

	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
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

func TestApplyObservabilityEnvRequiresTargetingAsset(t *testing.T) {
	in := registry.AssetInput{
		PartitionName: "doctor",
		IntentName:    "query",
		PusherName:    "k8s-main",
		Asset:         taskdomain.AbstractAsset{Type: "Compute", Name: "query"},
		Target:        targetdomain.Placement{Cluster: "main"},
	}
	env := ApplyObservabilityEnv(map[string]string{"EXISTING": "1"}, in, "kubernetes", "otel-collector.otel.svc.cluster.local:4317")
	if _, ok := env["OTEL_SERVICE_NAME"]; ok {
		t.Fatalf("observability env injected without a targeting asset: %#v", env)
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
