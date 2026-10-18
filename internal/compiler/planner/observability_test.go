package planner

import (
	"context"
	"testing"
)

func TestCompileSynthesizesDefaultObservabilityAsset(t *testing.T) {
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
  defaults:
    observability:
      enabled: true
      autoInstrument: otelc
      cloudwatch: auto
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
  assets:
    - type: Compute
      name: app
      properties:
        image: app:v1
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
	var found bool
	for _, asset := range compiled.Intents["api"].Spec.Spec.Assets {
		if asset.Type != "Observability" {
			continue
		}
		found = true
		if asset.Name != "observability" {
			t.Fatalf("observability asset name = %q", asset.Name)
		}
		if asset.Properties["mode"] != "auto" {
			t.Fatalf("observability mode = %v", asset.Properties["mode"])
		}
		targets, _ := asset.Properties["targets"].([]string)
		if len(targets) != 1 || targets[0] != "app" {
			t.Fatalf("observability targets = %#v", asset.Properties["targets"])
		}
	}
	if !found {
		t.Fatal("expected synthesized observability asset")
	}
}
