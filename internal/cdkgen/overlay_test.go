package cdkgen

import (
	"context"
	"os"
	"strings"
	"testing"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type fakeOverlayStore struct {
	files map[string][]byte
	fail  map[string]error
}

func newFakeOverlayStore() *fakeOverlayStore {
	return &fakeOverlayStore{files: map[string][]byte{}, fail: map[string]error{}}
}

func (f *fakeOverlayStore) ReadFile(_ context.Context, path string) ([]byte, error) {
	if err, ok := f.fail[path]; ok {
		return nil, err
	}
	raw, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return raw, nil
}

func (f *fakeOverlayStore) ListDir(_ context.Context, dir string) ([]guardianapi.DirEntry, error) {
	if err, ok := f.fail[dir]; ok {
		return nil, err
	}
	prefix := strings.TrimSuffix(dir, "/") + "/"
	seen := map[string]bool{}
	entries := []guardianapi.DirEntry{}
	for path := range f.files {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(path, prefix)
		name := rest
		isDir := false
		if idx := strings.Index(rest, "/"); idx >= 0 {
			name = rest[:idx]
			isDir = true
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		entries = append(entries, guardianapi.DirEntry{Name: name, IsDir: isDir})
	}
	if len(entries) == 0 {
		return nil, os.ErrNotExist
	}
	return entries, nil
}

const overlayEntryTemplate = `id: aws.compute.fargate
assetTypes: [Compute]
export: computeFargate
module: compute.fargate/construct
version: 2
requires: [network]
outputs: [service, launchType, cluster]
params:
  required: [image]
`

func TestOverlayProviderWithoutOverlays(t *testing.T) {
	store := newFakeOverlayStore()
	provider := NewOverlayProvider(context.Background(), store, "demo", nil)
	if provider.Catalog().Version != defaultCatalog.Version {
		t.Fatalf("catalog version = %q, want %q", provider.Catalog().Version, defaultCatalog.Version)
	}
	files, source, err := provider.ResolveDir("compute.fargate")
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}
	if source != TemplateSourceEmbedded {
		t.Fatalf("source = %q, want embedded", source)
	}
	if len(files) == 0 {
		t.Fatal("expected embedded template files")
	}
	if len(provider.Warnings()) != 0 {
		t.Fatalf("unexpected warnings: %v", provider.Warnings())
	}
}

func TestOverlayProviderGlobalOverride(t *testing.T) {
	store := newFakeOverlayStore()
	store.files["/.templates/aws/compute.fargate/template.yaml"] = []byte(overlayEntryTemplate)
	store.files["/.templates/aws/compute.fargate/construct.ts"] = []byte("// overridden compute")

	provider := NewOverlayProvider(context.Background(), store, "demo", nil)
	entry, ok := provider.Catalog().Entry("aws.compute.fargate")
	if !ok {
		t.Fatal("catalog entry missing")
	}
	if entry.Version != 2 {
		t.Fatalf("entry version = %d, want 2", entry.Version)
	}
	files, source, err := provider.ResolveDir("compute.fargate")
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}
	if source != TemplateSourceGlobal {
		t.Fatalf("source = %q, want global", source)
	}
	if string(files["construct.ts"]) != "// overridden compute" {
		t.Fatalf("files not overridden: %q", string(files["construct.ts"]))
	}
	if _, ok := files["template.yaml"]; ok {
		t.Fatal("template.yaml must not be part of the module files")
	}
}

func TestOverlayProviderPartitionWins(t *testing.T) {
	store := newFakeOverlayStore()
	store.files["/.templates/aws/compute.fargate/construct.ts"] = []byte("// global")
	store.files["/partitions/demo/templates/aws/compute.fargate/construct.ts"] = []byte("// partition")

	provider := NewOverlayProvider(context.Background(), store, "demo", nil)
	files, source, err := provider.ResolveDir("compute.fargate")
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}
	if source != TemplateSourcePartition {
		t.Fatalf("source = %q, want partition", source)
	}
	if string(files["construct.ts"]) != "// partition" {
		t.Fatalf("files = %q, want partition override", string(files["construct.ts"]))
	}
}

func TestOverlayProviderInvalidEntryFallsBack(t *testing.T) {
	store := newFakeOverlayStore()
	store.files["/.templates/aws/compute.fargate/template.yaml"] = []byte("not: [valid")
	store.files["/.templates/aws/compute.fargate/construct.ts"] = []byte("// broken")

	provider := NewOverlayProvider(context.Background(), store, "demo", nil)
	if _, ok := provider.Catalog().Entry("aws.compute.fargate"); !ok {
		t.Fatal("catalog entry missing")
	}
	entry, _ := provider.Catalog().Entry("aws.compute.fargate")
	if entry.Version != 1 {
		t.Fatalf("entry version = %d, want embedded 1", entry.Version)
	}
	files, source, err := provider.ResolveDir("compute.fargate")
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}
	if source != TemplateSourceEmbedded {
		t.Fatalf("source = %q, want embedded fallback", source)
	}
	if strings.Contains(string(files["construct.ts"]), "broken") {
		t.Fatal("invalid overlay files must not be used")
	}
	if len(provider.Warnings()) == 0 {
		t.Fatal("expected a warning for the invalid overlay entry")
	}
}

func TestOverlayProviderAddsNewTemplate(t *testing.T) {
	store := newFakeOverlayStore()
	store.files["/.templates/aws/queue.sqs/template.yaml"] = []byte(`id: custom.queue.sqs
assetTypes: [AWSResource]
export: cfnResource
module: queue.sqs/construct
version: 1
priority: 100
fallback: true
params:
  required: [cfnType]
`)
	store.files["/.templates/aws/queue.sqs/construct.ts"] = []byte("// custom queue template")

	provider := NewOverlayProvider(context.Background(), store, "demo", nil)
	entry, err := provider.Catalog().Select("AWSResource", map[string]any{"cfnType": "AWS::SQS::Queue"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if entry.ID != "custom.queue.sqs" {
		t.Fatalf("selected %s, want custom.queue.sqs", entry.ID)
	}
	result, err := Generate(Input{
		Partition: "p",
		Intent:    "i",
		Assets: []assetdomain.Spec{
			{Type: "AWSResource", Name: "queue", Properties: map[string]any{"cfnType": "AWS::SQS::Queue"}},
		},
		Templates: provider,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, ok := result.Files["templates/queue.sqs/construct.ts"]; !ok {
		t.Fatal("custom template file missing from payload")
	}
	found := false
	for _, ref := range result.Manifest.Templates {
		if ref.ID == "custom.queue.sqs" {
			found = true
			if ref.Source != TemplateSourceGlobal {
				t.Fatalf("ref source = %q, want global", ref.Source)
			}
		}
	}
	if !found {
		t.Fatal("manifest missing custom template ref")
	}
}

func TestOverlayProviderSharedOverride(t *testing.T) {
	store := newFakeOverlayStore()
	store.files["/partitions/demo/templates/aws/_shared/helpers.ts"] = []byte("// overridden helpers")

	provider := NewOverlayProvider(context.Background(), store, "demo", nil)
	result, err := Generate(Input{
		Partition: "p",
		Intent:    "i",
		Assets: []assetdomain.Spec{
			{Type: "ObjectStore", Name: "bucket", Properties: map[string]any{}},
		},
		Templates: provider,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := string(result.Files["templates/_shared/helpers.ts"]); got != "// overridden helpers" {
		t.Fatalf("shared helpers = %q, want partition override", got)
	}
}

func TestOverlayProviderWarnsOnStoreErrors(t *testing.T) {
	store := newFakeOverlayStore()
	store.fail["/.templates/aws"] = os.ErrPermission

	provider := NewOverlayProvider(context.Background(), store, "demo", nil)
	_ = provider.Catalog()
	if len(provider.Warnings()) == 0 {
		t.Fatal("expected warning for unreadable overlay root")
	}
	if !strings.Contains(provider.Warnings()[0], "permission") {
		t.Fatalf("warning = %q, want permission error", provider.Warnings()[0])
	}
}

func TestOverlayDigestChangesWithContent(t *testing.T) {
	base := newFakeOverlayStore()
	baseProvider := NewOverlayProvider(context.Background(), base, "demo", nil)
	baseResult, err := Generate(Input{
		Partition: "p",
		Intent:    "i",
		Assets:    []assetdomain.Spec{{Type: "ObjectStore", Name: "bucket", Properties: map[string]any{}}},
		Templates: baseProvider,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	overlay := newFakeOverlayStore()
	overlay.files["/.templates/aws/objectstore.s3/construct.ts"] = []byte("// changed object store")
	overlayProvider := NewOverlayProvider(context.Background(), overlay, "demo", nil)
	overlayResult, err := Generate(Input{
		Partition: "p",
		Intent:    "i",
		Assets:    []assetdomain.Spec{{Type: "ObjectStore", Name: "bucket", Properties: map[string]any{}}},
		Templates: overlayProvider,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	baseDigest := templateDigestByID(t, baseResult, "aws.objectstore.s3")
	overlayDigest := templateDigestByID(t, overlayResult, "aws.objectstore.s3")
	if baseDigest == overlayDigest {
		t.Fatal("digest must change when overlay content changes")
	}
}

func templateDigestByID(t *testing.T, result *Result, id string) string {
	t.Helper()
	for _, ref := range result.Manifest.Templates {
		if ref.ID == id {
			return ref.Digest
		}
	}
	t.Fatalf("template %s not found in manifest", id)
	return ""
}
