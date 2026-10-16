package awsdriver

import (
	"context"
	"strings"
	"testing"

	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/pusher/secrets"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
)

func observabilityInput(computeProps, observabilityProps map[string]any) registry.AssetInput {
	compute := taskdomain.AbstractAsset{Type: "Compute", Name: "api", Properties: computeProps}
	observability := taskdomain.AbstractAsset{
		Type:       "Observability",
		Name:       "otel",
		DependsOn:  []string{"api"},
		Properties: observabilityProps,
	}
	return registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "api",
		Asset:         compute,
		Assets: map[string]taskdomain.AbstractAsset{
			"api":  compute,
			"otel": observability,
		},
		Target: targetdomain.Placement{Account: "123456789012", Region: "eu-west-1"},
		Store:  memory.New(),
	}
}

func TestComputeAppliesOtelSidecar(t *testing.T) {
	backend := newAdoptionBackend()
	driver := &ComputeDriver{baseDriver{backend: backend, resolver: secrets.NoopResolver{}}}
	in := observabilityInput(
		map[string]any{
			"image": "ghcr.io/example/api:latest",
			"ports": []any{map[string]any{"name": "http", "containerPort": 8080}},
		},
		map[string]any{
			"provider":  "otel",
			"targets":   []any{"api"},
			"exporters": []any{"awsemf"},
		},
	)

	result, err := driver.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Outputs["service"] == "" {
		t.Fatalf("Apply() outputs = %+v", result.Outputs)
	}
	if backend.lastService == nil {
		t.Fatal("expected ECS service upsert")
	}
	if len(backend.lastService.Sidecars) != 1 {
		t.Fatalf("sidecars = %+v, want 1", backend.lastService.Sidecars)
	}
	sidecar := backend.lastService.Sidecars[0]
	if sidecar.Name != "otel-collector" {
		t.Fatalf("sidecar name = %q", sidecar.Name)
	}
	if sidecar.Image != defaultOtelCollectorImage {
		t.Fatalf("sidecar image = %q", sidecar.Image)
	}
	if sidecar.Essential == nil || *sidecar.Essential {
		t.Fatalf("sidecar must be non-essential, got %v", sidecar.Essential)
	}
	config := sidecar.Env["AOT_CONFIG_CONTENT"]
	if !strings.Contains(config, "awsemf") {
		t.Fatalf("collector config missing awsemf exporter:\n%s", config)
	}
	if !strings.Contains(config, "guardian/demo/api") {
		t.Fatalf("collector config missing namespace:\n%s", config)
	}
	if got := backend.lastService.Container.Env["OTEL_EXPORTER_OTLP_ENDPOINT"]; got != "http://localhost:4317" {
		t.Fatalf("main container OTLP endpoint = %q", got)
	}
	if got := backend.lastService.Container.Env["OTEL_RESOURCE_ATTRIBUTES"]; !strings.Contains(got, "guardian.partition=demo") {
		t.Fatalf("main container resource attrs = %q", got)
	}

	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("Diff() = %+v, want InSync", report)
	}

	in.Assets["otel"] = taskdomain.AbstractAsset{
		Type:      "Observability",
		Name:      "otel",
		DependsOn: []string{"api"},
		Properties: map[string]any{
			"provider":  "otel",
			"targets":   []any{"api"},
			"exporters": []any{"awsemf"},
			"endpoint":  "http://collector.observability:4317",
		},
	}
	report, err = driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("Diff() after collector change error = %v", err)
	}
	if report.Status != "Changed" {
		t.Fatalf("Diff() after collector change = %+v, want Changed", report)
	}
}

func TestOtelSidecarXRaySegmentNameNormalization(t *testing.T) {
	backend := newAdoptionBackend()
	driver := &ComputeDriver{baseDriver{backend: backend, resolver: secrets.NoopResolver{}}}
	in := observabilityInput(
		map[string]any{"image": "ghcr.io/example/api:latest"},
		map[string]any{"provider": "otel", "targets": []any{"api"}, "exporters": []any{"awsemf", "xray"}},
	)
	if _, err := driver.Apply(context.Background(), in); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if backend.lastService == nil || len(backend.lastService.Sidecars) != 1 {
		t.Fatalf("expected one sidecar, got %+v", backend.lastService)
	}
	config := backend.lastService.Sidecars[0].Env["AOT_CONFIG_CONTENT"]
	for _, needle := range []string{"awsxray", "attributes/xray", "peer.service", "delete"} {
		if !strings.Contains(config, needle) {
			t.Fatalf("collector config missing %q:\n%s", needle, config)
		}
	}
}

func TestComputeWithoutObservabilityHasNoSidecar(t *testing.T) {
	backend := newAdoptionBackend()
	driver := &ComputeDriver{baseDriver{backend: backend, resolver: secrets.NoopResolver{}}}
	in := adoptionInput("Compute", "api", map[string]any{"image": "ghcr.io/example/api:latest"})
	in.Assets = map[string]taskdomain.AbstractAsset{"api": in.Asset}

	if _, err := driver.Apply(context.Background(), in); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if backend.lastService == nil {
		t.Fatal("expected ECS service upsert")
	}
	if len(backend.lastService.Sidecars) != 0 {
		t.Fatalf("sidecars = %+v, want none", backend.lastService.Sidecars)
	}
	if got := backend.lastService.Container.Env["OTEL_EXPORTER_OTLP_ENDPOINT"]; got != "" {
		t.Fatalf("unexpected OTLP endpoint %q", got)
	}
}

func TestComputeRejectsMultipleObservabilityTargets(t *testing.T) {
	backend := newAdoptionBackend()
	driver := &ComputeDriver{baseDriver{backend: backend, resolver: secrets.NoopResolver{}}}
	in := observabilityInput(
		map[string]any{"image": "ghcr.io/example/api:latest"},
		map[string]any{"provider": "otel", "targets": []any{"api"}, "exporters": []any{"awsemf"}},
	)
	in.Assets["otel-2"] = taskdomain.AbstractAsset{
		Type:       "Observability",
		Name:       "otel-2",
		DependsOn:  []string{"api"},
		Properties: map[string]any{"provider": "otel", "targets": []any{"api"}, "exporters": []any{"awsemf"}},
	}
	if _, err := driver.Apply(context.Background(), in); err == nil {
		t.Fatal("expected error for multiple observability targets")
	}
}

func TestOtelSidecarUsesConfigAsset(t *testing.T) {
	backend := newAdoptionBackend()
	driver := &ComputeDriver{baseDriver{backend: backend, resolver: secrets.NoopResolver{}}}
	const configContent = "receivers:\n  otlp:\n    protocols:\n      grpc:\n"
	in := observabilityInput(
		map[string]any{"image": "ghcr.io/example/api:latest"},
		map[string]any{"provider": "otel", "targets": []any{"api"}, "config": "otel-config"},
	)
	in.Assets["otel-config"] = taskdomain.AbstractAsset{
		Type:       "Config",
		Name:       "otel-config",
		Properties: map[string]any{"content": configContent},
	}

	if _, err := driver.Apply(context.Background(), in); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if backend.lastService == nil || len(backend.lastService.Sidecars) != 1 {
		t.Fatalf("expected one sidecar, got %+v", backend.lastService)
	}
	if got := backend.lastService.Sidecars[0].Env["AOT_CONFIG_CONTENT"]; got != configContent {
		t.Fatalf("collector config = %q, want Config asset content", got)
	}
}

func TestObservabilityDriverApplyRequiresTargets(t *testing.T) {
	backend := newAdoptionBackend()
	driver := &ObservabilityDriver{baseDriver{backend: backend, resolver: secrets.NoopResolver{}}}
	in := observabilityInput(
		map[string]any{"image": "ghcr.io/example/api:latest"},
		map[string]any{"provider": "otel", "exporters": []any{"awsemf"}},
	)
	in.Asset = in.Assets["otel"]

	if _, err := driver.Apply(context.Background(), in); err == nil {
		t.Fatal("expected error when no compute targets are set")
	}

	in.Asset.Properties["targets"] = []any{"api"}
	result, err := driver.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Outputs["endpoint"] != "http://localhost:4317" {
		t.Fatalf("endpoint output = %q", result.Outputs["endpoint"])
	}
	if result.Outputs["namespace"] != "guardian/demo/api" {
		t.Fatalf("namespace output = %q", result.Outputs["namespace"])
	}
	if !strings.HasSuffix(result.Outputs["logGroup"], "/otel") {
		t.Fatalf("logGroup output = %q", result.Outputs["logGroup"])
	}
	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("Diff() = %+v, want InSync", report)
	}
}
