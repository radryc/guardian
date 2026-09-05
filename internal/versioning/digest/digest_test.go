package digest

import "testing"

func TestContentHashStable(t *testing.T) {
	a := ContentHash([]byte("guardian"))
	b := ContentHash([]byte("guardian"))
	c := ContentHash([]byte("other"))

	if a != b {
		t.Fatalf("hashes differ for same content: %q vs %q", a, b)
	}
	if a == c {
		t.Fatalf("hashes match for different content: %q", a)
	}
}

func TestNormalizedHashStable(t *testing.T) {
	first := map[string]any{"b": "2", "a": "1"}
	second := map[string]any{"a": "1", "b": "2"}
	if MustNormalizedHash(first) != MustNormalizedHash(second) {
		t.Fatalf("NormalizedHash should be stable for equivalent maps")
	}
}
