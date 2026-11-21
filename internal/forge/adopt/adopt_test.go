package adopt

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	fsstore "github.com/radryc/guardian/internal/store/fs"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func TestRunCopiesPartition(t *testing.T) {
	root := t.TempDir()
	store, err := fsstore.Open(filepath.Join(root, "store"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		_, err := store.UpsertFiles(context.Background(), guardianapi.MutationBatch{
			Writes:  []guardianapi.PathWrite{{LogicalPath: path, Content: []byte(content)}},
			Context: guardianapi.MutationContext{PrincipalID: "test"},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	write("/partitions/demo/config.yaml", "kind: Partition\n")
	write("/partitions/demo/intents/api.yaml", "kind: Intent\n")
	write("/partitions/demo/.state/images.json", "{}")
	write("/partitions/demo/secrets/tls.yaml", "value: x\n")

	out := filepath.Join(root, "out")
	result, err := Run(context.Background(), Options{
		Root: root, Partition: "demo", OutDir: out, Store: store,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Files) != 2 {
		t.Fatalf("files = %v", result.Files)
	}
	for _, relative := range []string{"config.yaml", "intents/api.yaml"} {
		if _, err := os.Stat(filepath.Join(out, relative)); err != nil {
			t.Errorf("missing %s: %v", relative, err)
		}
	}
	for _, skipped := range []string{".state/images.json", "secrets/tls.yaml"} {
		if _, err := os.Stat(filepath.Join(out, skipped)); err == nil {
			t.Errorf("%s must be skipped", skipped)
		}
	}

	if _, err := Run(context.Background(), Options{Root: root, Partition: "demo", OutDir: out, Store: store}); err == nil {
		t.Fatal("expected overwrite refusal")
	}
	if _, err := Run(context.Background(), Options{Root: root, Partition: "demo", OutDir: out, Store: store, Force: true}); err != nil {
		t.Fatalf("force: %v", err)
	}
}
