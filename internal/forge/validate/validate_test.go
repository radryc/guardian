package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func config() string {
	return `metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`
}

func intent() string {
	return `apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  targetPusher: k8s-main
  target:
    cluster: k8s-main
    namespace: demo
  assets:
    - type: Compute
      name: api
      properties:
        image: ghcr.io/example/api:1
`
}

func alert(partition string) string {
	return `version: 1
groups:
  - name: health
    tenant: default
    partition: ` + partition + `
    interval: 30s
    rules:
      - alert: DemoDown
        expr: up == 0
        labels:
          severity: warning
        annotations:
          summary: demo is down
`
}

func TestRunValidPartition(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "partitions", "demo")
	writeFile(t, filepath.Join(dir, "config.yaml"), config())
	writeFile(t, filepath.Join(dir, "intents", "api.yaml"), intent())
	writeFile(t, filepath.Join(dir, "alerts", "health.yaml"), alert("demo"))

	result, err := Run(context.Background(), Options{Root: root})
	if err != nil {
		t.Fatalf("Run() error = %v, diagnostics = %+v", err, result.Diagnostics)
	}
	if !result.Success || result.Errors != 0 {
		t.Fatalf("result = %+v", result)
	}
	if result.PartitionsChecked != 1 {
		t.Fatalf("PartitionsChecked = %d", result.PartitionsChecked)
	}
}

func TestRunAlertPartitionMismatch(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "partitions", "demo")
	writeFile(t, filepath.Join(dir, "config.yaml"), config())
	writeFile(t, filepath.Join(dir, "alerts", "health.yaml"), alert("other"))

	result, err := Run(context.Background(), Options{Root: root})
	if err == nil {
		t.Fatal("Run() expected an error")
	}
	if result.Errors == 0 {
		t.Fatalf("result = %+v", result)
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Code == "FGE5002" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected FGE5002, got %+v", result.Diagnostics)
	}
}

func TestRunYAMLParseError(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "partitions", "demo")
	writeFile(t, filepath.Join(dir, "config.yaml"), "metadata: [\n")

	result, err := Run(context.Background(), Options{Root: root})
	if err == nil {
		t.Fatal("Run() expected an error")
	}
	if len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "FGE1001" {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}

func TestStrictPromotesWarnings(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "partitions", "demo")
	writeFile(t, filepath.Join(dir, "config.yaml"), config())
	writeFile(t, filepath.Join(dir, "intents", "api.yaml"), `apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  targetPusher: k8s-main
  target:
    cluster: k8s-main
    namespace: demo
  assets:
    - type: Compute
      name: api
      properties:
        image: ${intent.images.outputs.api.imageRef}
`)

	lax, err := Run(context.Background(), Options{Root: root})
	if err != nil {
		t.Fatalf("lax Run() error = %v", err)
	}
	if lax.Warnings == 0 || !lax.Success {
		t.Fatalf("lax result = %+v", lax)
	}

	strict, err := Run(context.Background(), Options{Root: root, Strict: true})
	if err == nil {
		t.Fatal("strict Run() expected an error")
	}
	if strict.Success {
		t.Fatalf("strict result = %+v", strict)
	}
}
