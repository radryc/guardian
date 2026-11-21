package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func minimalConfig() string {
	return `metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`
}

func minimalIntent() string {
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

func TestLoadMinimalPartition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), minimalConfig())
	writeFile(t, filepath.Join(dir, "intents", "api.yaml"), minimalIntent())
	writeFile(t, filepath.Join(dir, "payloads", "app.yaml"), "kind: ConfigMap\n")
	writeFile(t, filepath.Join(dir, "alerts", "health.yaml"), "version: 1\n")
	writeFile(t, filepath.Join(dir, ".state", "images.json"), "{}")

	b, err := Load(dir, Options{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if b.PartitionName != "demo" {
		t.Fatalf("PartitionName = %q, want demo", b.PartitionName)
	}
	if got := b.IntentManifests["api"]; got == nil {
		t.Fatal("intent api missing")
	}
	wantPaths := []string{
		"/partitions/demo/config.yaml",
		"/partitions/demo/intents/api.yaml",
		"/partitions/demo/payloads/app.yaml",
		"/partitions/demo/alerts/health.yaml",
	}
	for _, path := range wantPaths {
		if _, ok := b.ManagedFiles[path]; !ok {
			t.Errorf("ManagedFiles missing %s (have %v)", path, keys(b.ManagedFiles))
		}
	}
	if _, ok := b.ManagedFiles["/partitions/demo/.state/images.json"]; ok {
		t.Error(".state must not be part of the managed bundle")
	}
	if !strings.Contains(string(b.ConfigContent), "apiVersion: guardian/v1alpha1") {
		t.Errorf("config not defaulted: %s", b.ConfigContent)
	}
	if !strings.Contains(string(b.ConfigContent), "kind: Partition") {
		t.Errorf("config kind not defaulted: %s", b.ConfigContent)
	}
}

func TestLoadIncludeSecrets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), minimalConfig())
	writeFile(t, filepath.Join(dir, "secrets", "tls.yaml"), "value: secret\n")

	b, err := Load(dir, Options{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := b.ManagedFiles["/partitions/demo/secrets/tls.yaml"]; ok {
		t.Fatal("secrets must be excluded by default")
	}

	b, err = Load(dir, Options{IncludeSecrets: true})
	if err != nil {
		t.Fatalf("Load(include secrets) error = %v", err)
	}
	if _, ok := b.ManagedFiles["/partitions/demo/secrets/tls.yaml"]; !ok {
		t.Fatal("secrets must be included when requested")
	}
}

func TestLoadDuplicateIntentNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), minimalConfig())
	writeFile(t, filepath.Join(dir, "intents", "a.yaml"), minimalIntent())
	writeFile(t, filepath.Join(dir, "intents", "b.yaml"), minimalIntent())

	_, err := Load(dir, Options{})
	if err == nil || !strings.Contains(err.Error(), "duplicate intent metadata.name") {
		t.Fatalf("Load() error = %v, want duplicate intent error", err)
	}
}

func TestValidateMissingPayload(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), minimalConfig())
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
      payload:
        app: /partitions/demo/payloads/missing.yaml
      properties:
        image: ghcr.io/example/api:1
`)

	_, err := Load(dir, Options{})
	if err == nil || !strings.Contains(err.Error(), "references missing file") {
		t.Fatalf("Load() error = %v, want missing payload error", err)
	}
}

func TestLoadCallsIntentRefResolver(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), minimalConfig())
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

	called := false
	b, err := Load(dir, Options{ResolveIntentRefs: func(_ string, files []IntentFile) error {
		called = true
		for i := range files {
			files[i].Content = []byte(strings.ReplaceAll(
				string(files[i].Content),
				"${intent.images.outputs.api.imageRef}",
				"registry.local/demo/api:sha256-abc",
			))
		}
		return nil
	}})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !called {
		t.Fatal("ResolveIntentRefs was not called")
	}
	props := b.IntentManifests["api"].Spec.Assets[0].Properties
	if got := props["image"]; got != "registry.local/demo/api:sha256-abc" {
		t.Fatalf("image = %v, want resolved ref", got)
	}
	// Disk content stays as a template; only the in-memory bundle is resolved.
	raw, err := os.ReadFile(filepath.Join(dir, "intents", "api.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "${intent.images.outputs.api.imageRef}") {
		t.Fatal("intent template on disk must not be rewritten")
	}
}

func TestManagedPartitionFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/partitions/demo/config.yaml", true},
		{"/partitions/demo/intents/api.yaml", true},
		{"/partitions/demo/payloads/app.yaml", true},
		{"/partitions/demo/alerts/a.yaml", true},
		{"/partitions/demo/.state/images.json", false},
		{"/partitions/demo/secrets/tls.yaml", false},
		{"/partitions/other/config.yaml", false},
	}
	for _, tc := range cases {
		if got := ManagedPartitionFile("demo", tc.path); got != tc.want {
			t.Errorf("ManagedPartitionFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestSortedContentKeysIsStable(t *testing.T) {
	got := SortedContentKeys(map[string][]byte{"b": nil, "a": nil, "c": nil})
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortedContentKeys = %v, want %v", got, want)
		}
	}
}

func keys(values map[string][]byte) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	return out
}

func TestResolveImageRefs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), minimalConfig())
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
        image: ${intent.images.outputs.api-build.imageRef}
        tag: ${intent.images.outputs.api-build.tag}
`)
	writeFile(t, filepath.Join(dir, ".state", "images.json"), `{
  "images": {
    "api-build": {"imageRef": "registry.local/demo/api:sha256-abc", "tag": "sha256-abc"}
  }
}`)

	b, err := Load(dir, Options{ResolveIntentRefs: ResolveImageRefs})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	props := b.IntentManifests["api"].Spec.Assets[0].Properties
	if got := props["image"]; got != "registry.local/demo/api:sha256-abc" {
		t.Fatalf("image = %v", got)
	}
	if got := props["tag"]; got != "sha256-abc" {
		t.Fatalf("tag = %v", got)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "intents", "api.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "${intent.images.outputs.api-build.imageRef}") {
		t.Fatal("intent template on disk must not be rewritten")
	}
}

func TestResolveImageRefsWithoutStateIsNoop(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), minimalConfig())
	writeFile(t, filepath.Join(dir, "intents", "api.yaml"), minimalIntent())

	b, err := Load(dir, Options{ResolveIntentRefs: ResolveImageRefs})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if b.PartitionName != "demo" {
		t.Fatalf("PartitionName = %q", b.PartitionName)
	}
}
