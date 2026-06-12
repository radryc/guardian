package assets

import (
	"fmt"
	"testing"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

func TestDecodeReturnsTypedSpecs(t *testing.T) {
	tests := []struct {
		name    string
		spec    assetdomain.Spec
		wantTyp string
	}{
		{
			name: "compute",
			spec: assetdomain.Spec{
				Type: assetdomain.TypeCompute,
				Name: "router",
				Properties: map[string]any{
					"image":    "router:v1",
					"replicas": 2,
				},
			},
			wantTyp: "*assets.ComputeSpec",
		},
		{
			name: "image build",
			spec: assetdomain.Spec{
				Type: assetdomain.TypeImageBuild,
				Name: "api-image",
				Properties: map[string]any{
					"repository": "demo-api",
					"registry":   "registry.strata.local:5000",
					"sourceDir":  "/partitions/demo/payloads/sources/api",
				},
			},
			wantTyp: "*assets.ImageBuildSpec",
		},
		{
			name: "cdk stack",
			spec: assetdomain.Spec{
				Type: assetdomain.TypeCDKStack,
				Name: "network",
				Properties: map[string]any{
					"context": map[string]any{
						"envName": "prod",
					},
				},
			},
			wantTyp: "*assets.CDKStackSpec",
		},
		{
			name: "sql database",
			spec: assetdomain.Spec{
				Type: assetdomain.TypeSQLDatabase,
				Name: "postgres",
				Properties: map[string]any{
					"engine": "postgres",
					"port":   5432,
				},
			},
			wantTyp: "*assets.SQLDatabaseSpec",
		},
		{
			name: "observability",
			spec: assetdomain.Spec{
				Type: assetdomain.TypeObservability,
				Name: "otel",
				Properties: map[string]any{
					"provider":  "otel",
					"endpoint":  "otel-collector:4317",
					"exporters": []any{"otlp"},
				},
			},
			wantTyp: "*assets.ObservabilitySpec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := Decode(tt.spec)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if actual := typeName(got); actual != tt.wantTyp {
				t.Fatalf("Decode() type = %s, want %s", actual, tt.wantTyp)
			}
		})
	}
}

func TestCatalogContainsKnownTypesAndHints(t *testing.T) {
	items := Catalog()
	if got, want := len(items), len(assetdomain.KnownTypes()); got != want {
		t.Fatalf("catalog size = %d, want %d", got, want)
	}
	compute, ok := CatalogFor(assetdomain.TypeCompute)
	if !ok {
		t.Fatalf("CatalogFor(%q) returned false", assetdomain.TypeCompute)
	}
	if len(compute.Hints) == 0 {
		t.Fatalf("expected compute hints, got none")
	}
	foundImage := false
	foundNestedPort := false
	for _, hint := range compute.Hints {
		switch hint.Path {
		case "image":
			foundImage = hint.Description != ""
		case "ports[].containerPort":
			foundNestedPort = hint.Description != ""
		}
	}
	if !foundImage {
		t.Fatalf("expected image hint in %+v", compute.Hints)
	}
	if !foundNestedPort {
		t.Fatalf("expected nested container port hint in %+v", compute.Hints)
	}
}

func TestResolveAssetHintsPrefersManifestOverrides(t *testing.T) {
	hints := ResolveAssetHints(assetdomain.TypeCompute, "api",
		[]assetdomain.Hint{{Path: "image", Title: "App image", Description: "Asset override wins."}},
		[]assetdomain.Hint{
			{Path: "assets.api.image", Description: "Intent override loses to asset override."},
			{Path: "assets.api.ports[0].containerPort", Description: "Intent-level port override."},
		},
	)
	imageFound := false
	portFound := false
	for _, hint := range hints {
		switch hint.Path {
		case "image":
			imageFound = hint.Description == "Asset override wins."
		case "ports[].containerPort":
			portFound = hint.Description == "Intent-level port override."
		}
	}
	if !imageFound {
		t.Fatalf("expected asset override to win in %+v", hints)
	}
	if !portFound {
		t.Fatalf("expected intent asset-scoped override in %+v", hints)
	}
}

func typeName(v any) string {
	return fmt.Sprintf("%T", v)
}
