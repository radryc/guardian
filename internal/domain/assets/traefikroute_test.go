package assets

import "testing"

func TestTraefikRouteValidateRequiresHostnameAndComputeTarget(t *testing.T) {
	definition := traefikRouteDefinition{}
	ctx := ValidationContext{AssetTypes: map[string]string{"query": "Compute"}}
	spec := &TraefikRouteSpec{Hostname: "doctor.strata", Target: "query", PortName: "http"}

	if err := definition.Validate(spec, ctx); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestTraefikRouteValidateRejectsUnknownTarget(t *testing.T) {
	definition := traefikRouteDefinition{}
	ctx := ValidationContext{AssetTypes: map[string]string{"config": "Config"}}
	spec := &TraefikRouteSpec{Hostname: "doctor.strata", Target: "config"}

	err := definition.Validate(spec, ctx)
	if err == nil {
		t.Fatalf("Validate() error = nil, want target validation error")
	}
	if got, want := err.Error(), "property target must reference an existing Compute asset"; got != want {
		t.Fatalf("Validate() error = %q, want %q", got, want)
	}
}

func TestCatalogForTraefikRouteIncludesHostnameHint(t *testing.T) {
	item, ok := CatalogFor("TraefikRoute")
	if !ok {
		t.Fatalf("CatalogFor(TraefikRoute) returned false")
	}
	for _, hint := range item.Hints {
		if hint.Path == "hostname" && hint.Description != "" {
			return
		}
	}
	t.Fatalf("expected hostname hint in %+v", item.Hints)
}