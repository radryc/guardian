package assets

import "testing"

func TestDevDNSRouteValidateRequiresHostnameAndComputeTarget(t *testing.T) {
	definition := devDNSRouteDefinition{}
	ctx := ValidationContext{AssetTypes: map[string]string{"query": "Compute"}}
	spec := &DevDNSRouteSpec{Hostname: "doctor.strata", Target: "query", PortName: "http"}

	if err := definition.Validate(spec, ctx); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestDevDNSRouteValidateRejectsUnknownTarget(t *testing.T) {
	definition := devDNSRouteDefinition{}
	ctx := ValidationContext{AssetTypes: map[string]string{"config": "Config"}}
	spec := &DevDNSRouteSpec{Hostname: "doctor.strata", Target: "config"}

	err := definition.Validate(spec, ctx)
	if err == nil {
		t.Fatalf("Validate() error = nil, want target validation error")
	}
	if got, want := err.Error(), "property target must reference an existing Compute asset"; got != want {
		t.Fatalf("Validate() error = %q, want %q", got, want)
	}
}

func TestCatalogForDevDNSRouteIncludesHostnameHint(t *testing.T) {
	item, ok := CatalogFor("DevDNSRoute")
	if !ok {
		t.Fatalf("CatalogFor(DevDNSRoute) returned false")
	}
	for _, hint := range item.Hints {
		if hint.Path == "hostname" && hint.Description != "" {
			return
		}
	}
	t.Fatalf("expected hostname hint in %+v", item.Hints)
}
