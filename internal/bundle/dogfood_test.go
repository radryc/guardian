package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadStratatoolsPartitions dogfoods the loader over the platform's real
// partition tree. It is skipped when the sibling checkout is not present, so
// the package stays self-contained in CI for the guardian repo alone.
func TestLoadStratatoolsPartitions(t *testing.T) {
	root := filepath.Join("..", "..", "..", "stratatools", "partitions")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("sibling checkout not present: %s", root)
		}
		t.Fatalf("read %s: %v", root, err)
	}

	checked := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// _bootstrap is the documented bootstrap exception: Guardian deploys it
		// before a pipeline exists, and its name is intentionally outside the
		// partition name grammar.
		if len(entry.Name()) > 0 && entry.Name()[0] == '_' {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
			if _, partErr := os.Stat(filepath.Join(dir, "partition.yaml")); partErr != nil {
				continue
			}
		}
		checked++
		t.Run(entry.Name(), func(t *testing.T) {
			if _, err := Load(dir, Options{}); err != nil {
				t.Fatalf("Load(%s): %v", dir, err)
			}
		})
	}
	if checked == 0 {
		t.Fatalf("no partition directories found under %s", root)
	}
}
