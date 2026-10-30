package assets

import (
	"testing"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
)

func TestProbeSpecDecodeJourney(t *testing.T) {
	decoded, _, err := Decode(assetdomain.Spec{
		Type: assetdomain.TypeProbe,
		Name: "journey",
		Properties: map[string]any{
			"target": "api",
			"steps": []any{
				map[string]any{
					"name":    "read",
					"method":  "GET",
					"path":    "/foo/bar",
					"capture": map[string]any{"foo": "json:$"},
				},
				map[string]any{
					"name":         "update",
					"method":       "PUT",
					"path":         "/foo/bar/${foo.id}",
					"body":         map[string]any{"status": "modified"},
					"expectStatus": []any{200},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := decoded.(*ProbeSpec)
	if !ok {
		t.Fatalf("decoded type = %T", decoded)
	}
	if len(spec.Steps) != 2 {
		t.Fatalf("steps = %#v", spec.Steps)
	}
	if spec.Steps[0].Capture["foo"] != "json:$" {
		t.Fatalf("capture = %#v", spec.Steps[0].Capture)
	}
	if spec.Steps[1].Body.(map[string]any)["status"] != "modified" {
		t.Fatalf("body = %#v", spec.Steps[1].Body)
	}
	if len(spec.Steps[1].ExpectStatus) != 1 || spec.Steps[1].ExpectStatus[0] != 200 {
		t.Fatalf("expectStatus = %#v", spec.Steps[1].ExpectStatus)
	}
}

func TestProbeSpecValidJourney(t *testing.T) {
	spec := &ProbeSpec{
		Target: "api",
		Steps: []ProbeStep{
			{Name: "read", Type: "http", Method: "GET", Path: "/foo/${id}", Capture: map[string]string{"id": "json:$.id"}},
			{Name: "update", Type: "http", Method: "PUT", Path: "/foo/${id}", ExpectStatus: []int{200}},
			{Name: "call", Type: "grpc", Service: "demo.v1.Api", GRPCMethod: "Get", Request: map[string]any{"id": "${id}"}, ExpectStatus: []int{0}},
		},
	}
	ctx := ValidationContext{AssetTypes: map[string]string{"api": "Compute"}}
	if err := (probeDefinition{}).Validate(spec, ctx); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestProbeSpecJourneyWithStepTargets(t *testing.T) {
	spec := &ProbeSpec{
		Steps: []ProbeStep{
			{Type: "http", URL: "https://example.com/foo", Method: "GET"},
		},
	}
	if err := (probeDefinition{}).Validate(spec, ValidationContext{}); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestProbeSpecJourneyValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		spec *ProbeSpec
	}{
		{
			name: "missing target",
			spec: &ProbeSpec{Steps: []ProbeStep{{Type: "http", Path: "/foo"}}},
		},
		{
			name: "bad http method",
			spec: &ProbeSpec{Target: "api", Steps: []ProbeStep{{Type: "http", Method: "GET POST", Path: "/foo"}}},
		},
		{
			name: "bad grpc status",
			spec: &ProbeSpec{Target: "api", Steps: []ProbeStep{{Type: "grpc", Service: "demo.v1.Api", GRPCMethod: "Get", ExpectStatus: []int{99}}}},
		},
		{
			name: "grpc method without service",
			spec: &ProbeSpec{Target: "api", Steps: []ProbeStep{{Type: "grpc", GRPCMethod: "Get"}}},
		},
		{
			name: "duplicate step",
			spec: &ProbeSpec{Target: "api", Steps: []ProbeStep{
				{Name: "a", Type: "http", Path: "/a"},
				{Name: "a", Type: "http", Path: "/b"},
			}},
		},
		{
			name: "bad capture name",
			spec: &ProbeSpec{Target: "api", Steps: []ProbeStep{
				{Name: "a", Type: "http", Path: "/a", Capture: map[string]string{"bad-name": "json:$.id"}},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ValidationContext{AssetTypes: map[string]string{"api": "Compute"}}
			if err := (probeDefinition{}).Validate(tt.spec, ctx); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
