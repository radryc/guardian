package awsdriver

import (
	"context"
	"testing"
)

func TestSynthTemplateFromFilesValidatesInput(t *testing.T) {
	ctx := context.Background()
	if _, err := SynthTemplateFromFiles(ctx, t.TempDir(), nil, "", "", "Stack", nil); err == nil {
		t.Fatalf("expected error for empty source files")
	}
	if _, err := SynthTemplateFromFiles(ctx, t.TempDir(), map[string]string{"../evil.ts": "x"}, "", "", "Stack", nil); err == nil {
		t.Fatalf("expected error for path traversal")
	}
	if _, err := SynthTemplateFromFiles(ctx, t.TempDir(), map[string]string{"/abs.ts": "x"}, "", "", "Stack", nil); err == nil {
		t.Fatalf("expected error for absolute path")
	}
}
