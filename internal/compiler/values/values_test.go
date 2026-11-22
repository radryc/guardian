package values

import "testing"

func TestResolvePreservesTypesForExactReferences(t *testing.T) {
	vals := map[string]any{"replicas": 3, "enabled": true, "image": "app:v1"}
	got, err := ResolveMap(map[string]any{
		"replicas": "${value.replicas}",
		"enabled":  "${value.enabled}",
		"image":    "repo:${value.image}",
		"nested":   []any{"${value.image}", "${value.replicas}"},
	}, vals)
	if err != nil {
		t.Fatal(err)
	}
	if replicas, ok := got["replicas"].(int); !ok || replicas != 3 {
		t.Fatalf("replicas = %#v", got["replicas"])
	}
	if enabled, ok := got["enabled"].(bool); !ok || !enabled {
		t.Fatalf("enabled = %#v", got["enabled"])
	}
	if got["image"] != "repo:app:v1" {
		t.Fatalf("image = %#v", got["image"])
	}
}

func TestMergeOverridesNestedValues(t *testing.T) {
	got := Merge(
		map[string]any{"a": "one", "nested": map[string]any{"x": "1", "y": "2"}},
		map[string]any{"a": "two", "nested": map[string]any{"y": "3"}},
	)
	if got["a"] != "two" {
		t.Fatalf("a = %v", got["a"])
	}
	nested := got["nested"].(map[string]any)
	if nested["x"] != "1" || nested["y"] != "3" {
		t.Fatalf("nested = %#v", nested)
	}
}

func TestResolveUnknownValue(t *testing.T) {
	if _, err := ResolveString("repo:${value.missing}", map[string]any{"image": "x"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveLeavesOtherPlaceholders(t *testing.T) {
	got, err := ResolveString("${intent.core.outputs.url}/${value.name}/${capture}", map[string]any{"name": "api"})
	if err != nil {
		t.Fatal(err)
	}
	want := "${intent.core.outputs.url}/api/${capture}"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
