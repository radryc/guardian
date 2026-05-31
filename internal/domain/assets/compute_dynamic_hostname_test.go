package assets

import "testing"

func TestComputeValidateAllowsDynamicHostnameWithoutPublishedPort(t *testing.T) {
	port := 8080
	definition := computeDefinition{}
	spec := &ComputeSpec{
		Image: "demo:v1",
		Ports: []PortSpec{{
			Port:            &port,
			DynamicHostname: "app.home.arpa",
		}},
	}

	if err := definition.Validate(spec, ValidationContext{}); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestComputeValidateRejectsDynamicHostnameWithoutListenerPort(t *testing.T) {
	definition := computeDefinition{}
	spec := &ComputeSpec{
		Image: "demo:v1",
		Ports: []PortSpec{{
			DynamicHostname: "app.home.arpa",
		}},
	}

	err := definition.Validate(spec, ValidationContext{})
	if err == nil {
		t.Fatalf("Validate() error = nil, want listener port validation error")
	}
	if got, want := err.Error(), "property ports[0] requires either port or containerPort"; got != want {
		t.Fatalf("Validate() error = %q, want %q", got, want)
	}
}

func TestComputeValidateRejectsHostPortWithDynamicHostname(t *testing.T) {
	containerPort := 8080
	hostPort := 18080
	definition := computeDefinition{}
	spec := &ComputeSpec{
		Image: "demo:v1",
		Ports: []PortSpec{{
			ContainerPort:   &containerPort,
			HostPort:        &hostPort,
			DynamicHostname: "app.home.arpa",
		}},
	}

	err := definition.Validate(spec, ValidationContext{})
	if err == nil {
		t.Fatalf("Validate() error = nil, want dynamic hostname hostPort validation error")
	}
	if got, want := err.Error(), "property ports[0].hostPort cannot be set when dynamicHostname is used"; got != want {
		t.Fatalf("Validate() error = %q, want %q", got, want)
	}
}

func TestCatalogForComputeIncludesDynamicHostnameHint(t *testing.T) {
	compute, ok := CatalogFor("Compute")
	if !ok {
		t.Fatalf("CatalogFor(Compute) returned false")
	}
	for _, hint := range compute.Hints {
		if hint.Path == "ports[].dynamicHostname" && hint.Description != "" {
			return
		}
	}
	t.Fatalf("expected dynamic hostname hint in %+v", compute.Hints)
}
