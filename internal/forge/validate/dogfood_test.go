package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestDogfoodStratatools runs validation over the platform's real partition
// tree. It skips when the sibling checkout is absent.
func TestDogfoodStratatools(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "stratatools", "partitions")
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("sibling checkout not present: %s", root)
		}
		t.Fatal(err)
	}

	result, err := Run(context.Background(), Options{Root: root})
	if err != nil {
		t.Fatalf("Run(%s) error = %v\ndiagnostics:\n%s", root, err, formatDiagnostics(result))
	}
	if result.PartitionsChecked < 18 {
		t.Fatalf("checked %d partitions, want at least 18", result.PartitionsChecked)
	}
	if result.Errors != 0 {
		t.Fatalf("errors = %d\ndiagnostics:\n%s", result.Errors, formatDiagnostics(result))
	}
	for _, d := range result.Diagnostics {
		if d.Code == "FGE7002" {
			t.Fatalf("unexpected discovery error: %+v", d)
		}
	}
	t.Logf("validated %d partitions, %d warnings", result.PartitionsChecked, result.Warnings)
}

func formatDiagnostics(result Result) string {
	out := ""
	for _, d := range result.Diagnostics {
		out += d.Error() + "\n"
	}
	return out
}
