package planner

import (
	"context"
	"testing"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
)

func TestCompileDeterministic(t *testing.T) {
	config := []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`)
	intents := map[string][]byte{
		"workers": []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: workers
spec:
  targetPusher: local
  target:
    cluster: local
  locked: false
  joins:
    - core
  assets:
    - type: Compute
      name: app
      dependsOn:
        - sidecar
      properties:
        image: repo:${intent.core.outputs.tag}
    - type: Compute
      name: sidecar
      properties:
        image: helper:v1
`),
		"core": []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: core
spec:
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Database
      name: db
      properties:
        engine: postgres
`),
	}

	input := CompileInput{
		PartitionName:    "demo",
		ConfigContent:    config,
		IntentContents:   intents,
		IntentVersionIDs: map[string]string{"workers": "v-workers", "core": "v-core"},
		ConfigVersionID:  "v-config",
		CurrentOutputs:   map[string]map[string]string{"core": {"tag": "v1"}},
	}

	first, err := Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	second, err := Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("Compile() second error = %v", err)
	}

	if first.PartitionRevision != second.PartitionRevision {
		t.Fatalf("PartitionRevision mismatch: %q vs %q", first.PartitionRevision, second.PartitionRevision)
	}
	if got, want := len(first.IntentOrder), 2; got != want {
		t.Fatalf("len(IntentOrder) = %d, want %d", got, want)
	}
	if first.IntentOrder[0] != "core" || first.IntentOrder[1] != "workers" {
		t.Fatalf("IntentOrder = %#v, want [core workers]", first.IntentOrder)
	}
	if first.Intents["workers"].AssetOrder[0] != "sidecar" || first.Intents["workers"].AssetOrder[1] != "app" {
		t.Fatalf("AssetOrder = %#v", first.Intents["workers"].AssetOrder)
	}
	props := first.Intents["workers"].Spec.Spec.Assets[0].Properties
	if props["image"] != "repo:v1" {
		t.Fatalf("resolved image = %v, want repo:v1", props["image"])
	}
}

func TestCompileAutofillsBlankAssetVersionFromAssetHash(t *testing.T) {
	config := []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`)
	intents := map[string][]byte{
		"api": []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Config
      name: config
      properties:
        format: yaml
        data:
          app.yaml: |
            hello: world
`),
	}

	compiled, err := Compile(context.Background(), CompileInput{
		PartitionName:    "demo",
		ConfigContent:    config,
		IntentContents:   intents,
		IntentVersionIDs: map[string]string{"api": "v-api"},
		IntentModTimes:   map[string]time.Time{"api": time.Date(2026, 5, 8, 13, 24, 0, 0, time.UTC)},
		ConfigVersionID:  "v-config",
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	intent := compiled.Intents["api"]
	if intent == nil {
		t.Fatal("compiled intent api not found")
	}
	assetVersionID := intent.AssetVersionIDs["config"]
	if assetVersionID == "" {
		t.Fatal("AssetVersionIDs[config] is empty")
	}
	if got, want := intent.AssetVersions["config"], revisions.DerivedAssetVersionAt(assetVersionID, time.Date(2026, 5, 8, 13, 24, 0, 0, time.UTC)); got != want {
		t.Fatalf("AssetVersions[config] = %q, want %q", got, want)
	}
	if got := intent.Spec.Spec.Assets[0].Version; got != "" {
		t.Fatalf("compiled spec version = %q, want empty string when manifest omits version", got)
	}
}

// TestCompileIntentCycle verifies that mutually-joined intents produce a cycle
// error rather than a silent bad ordering.
func TestCompileIntentCycle(t *testing.T) {
	cfg := []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: cycledemo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`)
	intents := map[string][]byte{
		"alpha": []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: alpha
spec:
  targetPusher: local
  target:
    cluster: local
  locked: false
  joins:
    - beta
  assets:
    - type: Compute
      name: a
      properties:
        image: img:v1
`),
		"beta": []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: beta
spec:
  targetPusher: local
  target:
    cluster: local
  locked: false
  joins:
    - alpha
  assets:
    - type: Compute
      name: b
      properties:
        image: img:v1
`),
	}

	_, err := Compile(context.Background(), CompileInput{
		PartitionName:    "cycledemo",
		ConfigContent:    cfg,
		IntentContents:   intents,
		IntentVersionIDs: map[string]string{"alpha": "v1", "beta": "v2"},
		ConfigVersionID:  "v-cfg",
	})
	if err == nil {
		t.Fatal("Compile() expected cycle error, got nil")
	}
}

// TestCompileMissingJoinReference verifies that referencing a non-existent
// intent in joins produces a clear error.
func TestCompileMissingJoinReference(t *testing.T) {
	cfg := []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: missingjoin
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`)
	intents := map[string][]byte{
		"worker": []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: worker
spec:
  targetPusher: local
  target:
    cluster: local
  locked: false
  joins:
    - nonexistent
  assets:
    - type: Compute
      name: w
      properties:
        image: img:v1
`),
	}

	_, err := Compile(context.Background(), CompileInput{
		PartitionName:    "missingjoin",
		ConfigContent:    cfg,
		IntentContents:   intents,
		IntentVersionIDs: map[string]string{"worker": "v1"},
		ConfigVersionID:  "v-cfg",
	})
	if err == nil {
		t.Fatal("Compile() expected missing-join error, got nil")
	}
}

// TestCompileOutputRefPreservesPlaceholder verifies that when upstream outputs
// are unavailable the planner succeeds (deferred resolution is intentional) but
// records the OutputRef so the orchestrator can block dispatch until the
// upstream intent is healthy.
func TestCompileOutputRefPreservesPlaceholder(t *testing.T) {
	cfg := []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: missingref
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`)
	intents := map[string][]byte{
		"dep": []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: dep
spec:
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Compute
      name: server
      properties:
        image: img:v1
        db_url: ${intent.upstream.outputs.db_url}
`),
	}

	compiled, err := Compile(context.Background(), CompileInput{
		PartitionName:    "missingref",
		ConfigContent:    cfg,
		IntentContents:   intents,
		IntentVersionIDs: map[string]string{"dep": "v1"},
		ConfigVersionID:  "v-cfg",
		CurrentOutputs:   map[string]map[string]string{},
	})
	if err != nil {
		t.Fatalf("Compile() error = %v (planner must not fail on unresolved refs)", err)
	}
	ci := compiled.Intents["dep"]
	if ci == nil {
		t.Fatal("compiled intent dep not found")
	}
	if len(ci.OutputRefs) != 1 {
		t.Fatalf("OutputRefs len = %d, want 1", len(ci.OutputRefs))
	}
	ref := ci.OutputRefs[0]
	if ref.IntentName != "upstream" || ref.OutputKey != "db_url" {
		t.Fatalf("OutputRef = %+v, want upstream/db_url", ref)
	}
	// The raw placeholder must still be in the compiled properties because
	// resolution was deferred.
	dbURL := ci.Spec.Spec.Assets[0].Properties["db_url"]
	if dbURL != "${intent.upstream.outputs.db_url}" {
		t.Fatalf("db_url property = %v, want raw placeholder", dbURL)
	}
}

func TestCompileCapturesAssetReleaseVersion(t *testing.T) {
	config := []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`)
	intents := map[string][]byte{
		"api": []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Compute
      name: backend
      version: v1.2.3-abc123-20260429
      properties:
        image: repo/backend:v1.2.3
`),
	}

	compiled, err := Compile(context.Background(), CompileInput{
		PartitionName:    "demo",
		ConfigContent:    config,
		IntentContents:   intents,
		IntentVersionIDs: map[string]string{"api": "v-api"},
		ConfigVersionID:  "v-config",
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	intent := compiled.Intents["api"]
	if intent == nil {
		t.Fatal("compiled intent api not found")
	}
	if got, want := intent.AssetVersions["backend"], "v1.2.3-abc123-20260429"; got != want {
		t.Fatalf("AssetVersions[backend] = %q, want %q", got, want)
	}

	changed, err := Compile(context.Background(), CompileInput{
		PartitionName: "demo",
		ConfigContent: config,
		IntentContents: map[string][]byte{
			"api": []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Compute
      name: backend
      version: v1.2.4-def456-20260430
      properties:
        image: repo/backend:v1.2.3
`),
		},
		IntentVersionIDs: map[string]string{"api": "v-api"},
		ConfigVersionID:  "v-config",
	})
	if err != nil {
		t.Fatalf("Compile() changed error = %v", err)
	}
	if intent.AssetVersionIDs["backend"] == changed.Intents["api"].AssetVersionIDs["backend"] {
		t.Fatalf("AssetVersionID did not change when release version changed")
	}
}
