package resolver

import "testing"

func TestFindRefs(t *testing.T) {
	properties := map[string]any{
		"url": "${intent.core.outputs.url}",
		"nested": map[string]any{
			"image": "repo:${intent.base.outputs.tag}",
		},
		"list": []any{"${intent.core.outputs.url}"},
	}

	refs := FindRefs(properties)
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(refs))
	}
}

func TestResolveProperties(t *testing.T) {
	properties := map[string]any{
		"url":  "${intent.core.outputs.url}",
		"dsn":  "postgres://${intent.core.outputs.user}@${intent.core.outputs.url}",
		"list": []any{"${intent.base.outputs.tag}"},
	}
	outputs := map[string]map[string]string{
		"core": {"url": "db.local", "user": "guardian"},
		"base": {"tag": "v1"},
	}

	got, err := ResolveProperties(properties, outputs)
	if err != nil {
		t.Fatalf("ResolveProperties() error = %v", err)
	}

	if got["url"] != "db.local" {
		t.Fatalf("url = %v, want db.local", got["url"])
	}
	if got["dsn"] != "postgres://guardian@db.local" {
		t.Fatalf("dsn = %v", got["dsn"])
	}
	list := got["list"].([]any)
	if list[0] != "v1" {
		t.Fatalf("list[0] = %v, want v1", list[0])
	}
}

func TestResolvePropertiesMissingOutput(t *testing.T) {
	_, err := ResolveProperties(map[string]any{
		"url": "${intent.core.outputs.url}",
	}, map[string]map[string]string{})
	if err == nil {
		t.Fatalf("ResolveProperties() expected error")
	}
}
