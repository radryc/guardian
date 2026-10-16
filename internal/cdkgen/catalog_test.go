package cdkgen

import (
	"strings"
	"testing"
)

func TestEmbeddedCatalogValid(t *testing.T) {
	catalog := defaultCatalog
	if catalog.Version == "" {
		t.Fatal("catalog version is empty")
	}
	if len(catalog.Templates) == 0 {
		t.Fatal("catalog has no templates")
	}
	for _, entry := range catalog.Templates {
		if entry.ID == "" || entry.Module == "" || entry.Export == "" {
			t.Errorf("incomplete template entry: %+v", entry)
		}
	}
	if !catalog.SupportsAssetType("Compute") {
		t.Error("catalog must support Compute")
	}
	if catalog.SupportsAssetType("Secret") {
		t.Error("catalog must not support Secret")
	}
}

func TestCatalogSelectVariants(t *testing.T) {
	catalog := defaultCatalog
	cases := []struct {
		name       string
		assetType  string
		properties map[string]any
		wantID     string
	}{
		{
			name:       "load balancer http defaults to alb",
			assetType:  "LoadBalancer",
			properties: map[string]any{"listeners": []any{map[string]any{"port": float64(80), "protocol": "http"}}},
			wantID:     "aws.loadbalancer.alb",
		},
		{
			name:       "load balancer tcp selects nlb",
			assetType:  "LoadBalancer",
			properties: map[string]any{"listeners": []any{map[string]any{"port": float64(80), "protocol": "TCP"}}},
			wantID:     "aws.loadbalancer.nlb",
		},
		{
			name:       "mixed listeners with tcp select nlb",
			assetType:  "LoadBalancer",
			properties: map[string]any{"listeners": []any{map[string]any{"protocol": "http"}, map[string]any{"protocol": "tcp"}}},
			wantID:     "aws.loadbalancer.nlb",
		},
		{
			name:       "database postgres engine",
			assetType:  "Database",
			properties: map[string]any{"engine": "aurora-postgresql"},
			wantID:     "aws.database.aurora-postgres",
		},
		{
			name:       "database default engine is mysql",
			assetType:  "SQLDatabase",
			properties: map[string]any{},
			wantID:     "aws.database.aurora-mysql",
		},
		{
			name:       "aws resource fallback",
			assetType:  "AWSResource",
			properties: map[string]any{"cfnType": "AWS::SQS::Queue"},
			wantID:     "aws.fallback.cfn-resource",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry, err := catalog.Select(tc.assetType, tc.properties)
			if err != nil {
				t.Fatalf("Select: %v", err)
			}
			if entry.ID != tc.wantID {
				t.Fatalf("selected %s, want %s", entry.ID, tc.wantID)
			}
		})
	}
}

func TestCatalogSelectUnknownType(t *testing.T) {
	if _, err := defaultCatalog.Select("Secret", map[string]any{}); err == nil {
		t.Fatal("expected error for unsupported asset type")
	}
}

func TestMatchRuleOperators(t *testing.T) {
	properties := map[string]any{
		"engine": "Aurora-PostgreSQL",
		"image":  "public.ecr.aws/nginx/nginx:latest",
		"empty":  "",
		"listeners": []any{
			map[string]any{"protocol": "HTTP"},
			map[string]any{"protocol": "tcp"},
		},
		"nested": map[string]any{"deep": map[string]any{"value": "yes"}},
	}
	cases := []struct {
		rule MatchRule
		want bool
	}{
		{MatchRule{Path: "engine", Op: "equals", Value: "aurora-postgresql"}, true},
		{MatchRule{Path: "engine", Op: "equals", Value: "mysql"}, false},
		{MatchRule{Path: "engine", Op: "in", Value: "mysql,aurora-postgresql"}, true},
		{MatchRule{Path: "image", Op: "contains", Value: "nginx"}, true},
		{MatchRule{Path: "listeners[*].protocol", Op: "equals", Value: "TCP"}, true},
		{MatchRule{Path: "listeners[*].protocol", Op: "equals", Value: "udp"}, false},
		{MatchRule{Path: "empty", Op: "exists"}, true},
		{MatchRule{Path: "missing", Op: "exists"}, false},
		{MatchRule{Path: "missing", Op: "notexists"}, true},
		{MatchRule{Path: "empty", Op: "notexists"}, false},
		{MatchRule{Path: "nested.deep.value", Op: "equals", Value: "yes"}, true},
	}
	for _, tc := range cases {
		if got := matchRule(properties, tc.rule); got != tc.want {
			t.Errorf("matchRule(%+v) = %v, want %v", tc.rule, got, tc.want)
		}
	}
}

func TestOutputsForExpandsPatterns(t *testing.T) {
	entry, err := defaultCatalog.Select("LoadBalancer", map[string]any{
		"listeners": []any{map[string]any{"port": float64(80)}, map[string]any{"port": float64(443)}},
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	outputs := entry.OutputsFor(map[string]any{
		"listeners": []any{map[string]any{"port": float64(80)}, map[string]any{"port": float64(443)}},
	})
	joined := strings.Join(outputs, ",")
	for _, want := range []string{"lb_arn", "lb_name", "dns_name", "tg_0_arn", "tg_1_arn"} {
		if !strings.Contains(joined, want) {
			t.Errorf("outputs missing %s: %v", want, outputs)
		}
	}
	if strings.Contains(joined, "tg_2_arn") {
		t.Errorf("outputs expanded too far: %v", outputs)
	}
}

func TestOutputsForType(t *testing.T) {
	outputs := OutputsForType("LoadBalancer", map[string]any{
		"listeners": []any{map[string]any{"port": float64(80)}},
	})
	joined := strings.Join(outputs, ",")
	for _, want := range []string{"lb_arn", "tg_0_arn"} {
		if !strings.Contains(joined, want) {
			t.Errorf("outputs missing %s: %v", want, outputs)
		}
	}
	if OutputsForType("Secret", map[string]any{}) != nil {
		t.Error("unsupported asset type must have no outputs")
	}
}

func TestValidateProps(t *testing.T) {
	compute, err := defaultCatalog.Select("Compute", map[string]any{})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if err := compute.ValidateProps("c", map[string]any{"image": "nginx"}); err != nil {
		t.Fatalf("valid props rejected: %v", err)
	}
	if err := compute.ValidateProps("c", map[string]any{}); err == nil {
		t.Fatal("missing image must fail")
	}
	if err := compute.ValidateProps("c", map[string]any{"image": "nginx", "privileged": true}); err == nil {
		t.Fatal("privileged compute must fail")
	}
	if err := compute.ValidateProps("c", map[string]any{"image": "nginx", "privileged": false}); err != nil {
		t.Fatalf("privileged=false must pass: %v", err)
	}

	observability, err := defaultCatalog.Select("Observability", map[string]any{})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if err := observability.ValidateProps("o", map[string]any{"targets": []any{}}); err == nil {
		t.Fatal("empty targets must fail")
	}
	if err := observability.ValidateProps("o", map[string]any{"targets": []any{"api"}}); err != nil {
		t.Fatalf("targets must pass: %v", err)
	}
}

func TestParseCatalogRejectsInvalid(t *testing.T) {
	_, err := ParseCatalog([]byte("apiVersion: guardian/v1alpha1\ntemplates:\n  - id: dup\n    module: a/b\n    export: x\n    assetTypes: [Compute]\n  - id: dup\n    module: c/d\n    export: y\n    assetTypes: [Volume]\n"))
	if err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("expected duplicate id error, got %v", err)
	}

	_, err = ParseCatalog([]byte("templates:\n  - id: bad\n    module: a/b\n    export: x\n    assetTypes: [Compute]\n    match:\n      - path: x\n        op: bogus\n"))
	if err == nil || !strings.Contains(err.Error(), "unsupported match op") {
		t.Fatalf("expected match op error, got %v", err)
	}

	_, err = ParseCatalog([]byte("templates:\n  - id: bad\n    export: x\n    assetTypes: [Compute]\n"))
	if err == nil || !strings.Contains(err.Error(), "module and export") {
		t.Fatalf("expected module error, got %v", err)
	}
}

func TestTemplateDigestStable(t *testing.T) {
	files := map[string][]byte{"construct.ts": []byte("a"), "extra.ts": []byte("b")}
	first := TemplateDigest(files)
	second := TemplateDigest(map[string][]byte{"extra.ts": []byte("b"), "construct.ts": []byte("a")})
	if first != second {
		t.Fatalf("digest not stable across map order: %s vs %s", first, second)
	}
	if third := TemplateDigest(map[string][]byte{"construct.ts": []byte("changed")}); third == first {
		t.Fatal("digest must change when content changes")
	}
}
