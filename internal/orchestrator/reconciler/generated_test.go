package reconciler

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/cdkgen"
	"github.com/rydzu/ainfra/guardian/internal/compiler/planner"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/dispatcher"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func compiledCDKIntent(assets []assetdomain.Spec) *planner.CompiledPartition {
	return &planner.CompiledPartition{
		Intents: map[string]*planner.CompiledIntent{
			"demo-stack": {
				Name: "demo-stack",
				Spec: &intentdomain.Intent{
					APIVersion: "guardian/v1alpha1",
					Kind:       "Intent",
					Metadata:   intentdomain.Metadata{Name: "demo-stack"},
					Spec: intentdomain.IntentSpec{
						IntentType:     intentdomain.TypeStandard,
						Implementation: intentdomain.ImplementationCDK,
						TargetPusher:   "aws-975049940689",
						Target:         targetdomain.Placement{Account: "975049940689", Region: "us-east-1"},
						Assets:         assets,
					},
				},
				AssetOrder: []string{"data", "web"},
			},
		},
	}
}

func TestSyncGeneratedAWSPayloadsWritesStore(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reconciler := NewReconciler(store, dispatcher.NewDispatcher(store, "guardian"), time.Minute)

	compiled := compiledCDKIntent([]assetdomain.Spec{
		{Type: "Config", Name: "page", Properties: map[string]any{"content": "hello"}},
		{Type: "Volume", Name: "data", Properties: map[string]any{"size": "1Gi"}},
		{Type: "Compute", Name: "web", Properties: map[string]any{"image": "nginx"}},
	})
	errs := reconciler.syncGeneratedAWSPayloads(ctx, "aws-demo", compiled)
	if len(errs) != 0 {
		t.Fatalf("unexpected generation errors: %v", errs)
	}
	dir := paths.GeneratedAWSPayloadDir("aws-demo", "demo-stack")
	for _, rel := range []string{"bin/app.ts", "lib/stack.ts", "package.json", cdkgen.PayloadManifestFileName} {
		if _, err := store.ReadFile(ctx, dir+"/"+rel); err != nil {
			t.Errorf("missing generated file %s: %v", rel, err)
		}
	}
	manifest, err := store.ReadFile(ctx, dir+"/"+cdkgen.PayloadManifestFileName)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(manifest), "\"data\"") || !strings.Contains(string(manifest), "\"web\"") {
		t.Fatalf("manifest missing generated assets: %s", manifest)
	}

	// A second sync with identical input must not rewrite files.
	before, _ := store.Stat(ctx, dir+"/lib/stack.ts")
	errs = reconciler.syncGeneratedAWSPayloads(ctx, "aws-demo", compiled)
	if len(errs) != 0 {
		t.Fatalf("unexpected second-pass errors: %v", errs)
	}
	after, _ := store.Stat(ctx, dir+"/lib/stack.ts")
	if before.VersionID != after.VersionID {
		t.Fatalf("unchanged payload was rewritten: %s -> %s", before.VersionID, after.VersionID)
	}
}

func TestSyncGeneratedAWSPayloadsBlocksOnGenerationError(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reconciler := NewReconciler(store, dispatcher.NewDispatcher(store, "guardian"), time.Minute)

	compiled := compiledCDKIntent([]assetdomain.Spec{
		{Type: "Compute", Name: "web", Properties: map[string]any{"image": "${intent.other.outputs.web.id}"}},
	})
	errs := reconciler.syncGeneratedAWSPayloads(ctx, "aws-demo", compiled)
	if errs["demo-stack"] == nil {
		t.Fatal("expected generation error for unresolved reference")
	}
	if _, err := store.ReadFile(ctx, paths.GeneratedAWSPayloadDir("aws-demo", "demo-stack")+"/"+cdkgen.PayloadManifestFileName); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("payload should not be written on error, got %v", err)
	}
}

// TestSyncGeneratedAWSPayloadsUsesPartitionOverlay verifies the reconciler
// layers partition-scoped template overrides over the embedded baseline and
// records the overlay source in the payload manifest.
func TestSyncGeneratedAWSPayloadsUsesPartitionOverlay(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	reconciler := NewReconciler(store, dispatcher.NewDispatcher(store, "guardian"), time.Minute)

	overlayEntry := `id: aws.objectstore.s3
assetTypes: [ObjectStore]
export: objectStoreS3
module: objectstore.s3/construct
version: 9
outputs: [bucket, region, type]
params:
  unsupported: [endpoint, existingBucket]
`
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{
			{LogicalPath: "/partitions/aws-demo/templates/aws/objectstore.s3/construct.ts", Content: []byte("// partition overlay object store")},
			{LogicalPath: "/partitions/aws-demo/templates/aws/objectstore.s3/template.yaml", Content: []byte(overlayEntry)},
		},
	}); err != nil {
		t.Fatalf("seed overlay: %v", err)
	}

	compiled := compiledCDKIntent([]assetdomain.Spec{
		{Type: "ObjectStore", Name: "data", Properties: map[string]any{"versioning": true}},
	})
	errs := reconciler.syncGeneratedAWSPayloads(ctx, "aws-demo", compiled)
	if len(errs) != 0 {
		t.Fatalf("unexpected generation errors: %v", errs)
	}

	dir := paths.GeneratedAWSPayloadDir("aws-demo", "demo-stack")
	content, err := store.ReadFile(ctx, dir+"/templates/objectstore.s3/construct.ts")
	if err != nil {
		t.Fatalf("read overlay template: %v", err)
	}
	if string(content) != "// partition overlay object store" {
		t.Fatalf("overlay template not used: %q", string(content))
	}
	manifest, err := store.ReadFile(ctx, dir+"/"+cdkgen.PayloadManifestFileName)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(manifest), `"source": "partition"`) {
		t.Fatalf("manifest missing partition overlay source: %s", manifest)
	}
}
