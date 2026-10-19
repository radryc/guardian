package observability

import (
	"context"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	memorystore "github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestIdentitiesDeriveCanonicalCatalog(t *testing.T) {
	ctx := context.Background()
	store := memorystore.New()
	partition := []byte(`apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  defaults:
    targetPusher: k8s-main
    target:
      cluster: main
`)
	intent := []byte(`apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: k8s-main
  target:
    cluster: main
  assets:
    - type: Compute
      name: app
      properties:
        image: example/app:latest
`)
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{
			{LogicalPath: paths.PartitionConfig("demo"), Content: partition},
			{LogicalPath: paths.IntentManifest("demo", "api"), Content: intent},
		},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "catalog test"},
	}); err != nil {
		t.Fatal(err)
	}

	records, err := Identities(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %+v", records)
	}
	record := records[0]
	if record.Service != "demo.api.app" || record.FullName != "demo.api.app" || record.Pusher != "k8s-main" || record.Cluster != "main" {
		t.Fatalf("unexpected record: %+v", record)
	}
}
