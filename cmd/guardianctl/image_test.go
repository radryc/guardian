package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadImageBuildAssets(t *testing.T) {
	dir := t.TempDir()
	intentsDir := filepath.Join(dir, "intents")
	os.MkdirAll(intentsDir, 0755)

	imagesYAML := `
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: images
spec:
  intentType: standard
  targetPusher: k8s-main
  target:
    cluster: k8s-main
    namespace: test
  locked: false
  assets:
    - type: ImageBuild
      name: my-build
      version: dev
      properties:
        buildContext: ~/src/myapp
        dockerfile: Dockerfile
        registry: registry.strata.local:5000
        repository: myapp
        buildArgs:
          GO_VERSION: "1.22"
`
	os.WriteFile(filepath.Join(intentsDir, "images.yaml"), []byte(imagesYAML), 0644)

	assets, err := loadImageBuildAssets(dir)
	if err != nil {
		t.Fatalf("loadImageBuildAssets: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(assets))
	}
	if assets[0].AssetName != "my-build" {
		t.Errorf("expected my-build, got %s", assets[0].AssetName)
	}
	if assets[0].Repository != "myapp" {
		t.Errorf("expected myapp, got %s", assets[0].Repository)
	}
	if assets[0].Registry != "registry.strata.local:5000" {
		t.Errorf("unexpected registry: %s", assets[0].Registry)
	}
	if assets[0].BuildContext != "~/src/myapp" {
		t.Errorf("unexpected buildContext: %s", assets[0].BuildContext)
	}
	if assets[0].Dockerfile != "Dockerfile" {
		t.Errorf("unexpected dockerfile: %s", assets[0].Dockerfile)
	}
	if assets[0].BuildArgs["GO_VERSION"] != "1.22" {
		t.Errorf("unexpected build-arg: %s", assets[0].BuildArgs["GO_VERSION"])
	}
}

func TestLoadImageBuildAssetsNoBuildContext(t *testing.T) {
	dir := t.TempDir()
	intentsDir := filepath.Join(dir, "intents")
	os.MkdirAll(intentsDir, 0755)

	imagesYAML := `
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: images
spec:
  assets:
    - type: ImageBuild
      name: upstream-build
      version: dev
      properties:
        imageFromUpstream: grafana/grafana:latest
        sourceImage: grafana:latest
        repository: grafana
`
	os.WriteFile(filepath.Join(intentsDir, "images.yaml"), []byte(imagesYAML), 0644)

	assets, err := loadImageBuildAssets(dir)
	if err != nil {
		t.Fatalf("loadImageBuildAssets: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("expected 1 asset (imageFromUpstream), got %d", len(assets))
	}
}

func TestLoadImageBuildAssetsMultiple(t *testing.T) {
	dir := t.TempDir()
	intentsDir := filepath.Join(dir, "intents")
	os.MkdirAll(intentsDir, 0754)

	imagesYAML := `
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: images
spec:
  assets:
    - type: ImageBuild
      name: build-a
      version: dev
      properties:
        buildContext: /src/a
        dockerfile: Dockerfile
        repository: a
    - type: ImageBuild
      name: build-b
      version: dev
      properties:
        buildContext: /src/b
        dockerfile: Dockerfile
        repository: b
`
	os.WriteFile(filepath.Join(intentsDir, "images.yaml"), []byte(imagesYAML), 0644)

	assets, err := loadImageBuildAssets(dir)
	if err != nil {
		t.Fatalf("loadImageBuildAssets: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(assets))
	}
}

func TestExpandBuildContext(t *testing.T) {
	home, _ := os.UserHomeDir()
	tests := []struct {
		input    string
		contains string
	}{
		{"~/src/myapp", home},
		{"$HOME/src/myapp", home},
		{"/absolute/path", "/absolute/path"},
	}
	for _, tc := range tests {
		result := expandBuildContext(tc.input)
		if !filepath.IsAbs(result) {
			t.Errorf("expandBuildContext(%q) = %q, expected absolute path", tc.input, result)
		}
		_ = tc.contains
	}
}

func TestResolveDockerfile(t *testing.T) {
	tests := []struct {
		dockerfile   string
		buildContext string
		expected     string
	}{
		{"Dockerfile", "/src/app", "/src/app/Dockerfile"},
		{"/absolute/path/Dockerfile", "/src/app", "/absolute/path/Dockerfile"},
		{"build/Dockerfile", "/src/app", "/src/app/build/Dockerfile"},
		{"", "/src/app", "/src/app/Dockerfile"},
	}
	for _, tc := range tests {
		result := resolveDockerfile(tc.dockerfile, tc.buildContext, "/tmp")
		if result != tc.expected {
			t.Errorf("resolveDockerfile(%q, %q) = %q, expected %q", tc.dockerfile, tc.buildContext, result, tc.expected)
		}
	}
}

func TestImageStateRoundtrip(t *testing.T) {
	dir := t.TempDir()
	state := &imageState{
		Images: map[string]imageEntry{
			"my-build": {
				AssetName:   "my-build",
				IntentFile:  "/tmp/intents/images.yaml",
				LocalTag:    "myapp:abc123",
				Repository:  "myapp",
				Registry:    "registry.strata.local:5000",
				Dockerfile:  "/src/app/Dockerfile",
				Context:     "/src/app",
				ImageRef:    "registry.strata.local:5000/myapp:sha256-abc12345",
				Tag:         "sha256-abc12345",
				BuildArgs:   map[string]string{"GO_VERSION": "1.22"},
				Target:      "",
				Platform:    "linux/amd64",
			},
		},
	}
	if err := saveImageState(dir, state); err != nil {
		t.Fatalf("saveImageState: %v", err)
	}

	loaded, err := loadImageState(dir)
	if err != nil {
		t.Fatalf("loadImageState: %v", err)
	}
	if len(loaded.Images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(loaded.Images))
	}
	entry := loaded.Images["my-build"]
	if entry.Tag != "sha256-abc12345" {
		t.Errorf("expected sha256-abc12345, got %s", entry.Tag)
	}
	if entry.ImageRef != "registry.strata.local:5000/myapp:sha256-abc12345" {
		t.Errorf("unexpected imageRef: %s", entry.ImageRef)
	}
	if entry.Platform != "linux/amd64" {
		t.Errorf("unexpected platform: %s", entry.Platform)
	}
}

func TestImageStampIntentTransformation(t *testing.T) {
	dir := t.TempDir()
	intentsDir := filepath.Join(dir, "intents")
	os.MkdirAll(intentsDir, 0755)
	stateDir := filepath.Join(dir, ".state")
	os.MkdirAll(stateDir, 0755)

	originalYAML := `
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: images
spec:
  assets:
    - type: ImageBuild
      name: my-build
      version: dev
      properties:
        buildContext: ~/src/myapp
        dockerfile: Dockerfile
        repository: myapp
        registry: registry.strata.local:5000
`
	intentPath := filepath.Join(intentsDir, "images.yaml")
	os.WriteFile(intentPath, []byte(originalYAML), 0644)

	state := &imageState{
		Images: map[string]imageEntry{
			"my-build": {
				AssetName: "my-build",
				ImageRef:  "registry.strata.local:5000/myapp:sha256-abc12345",
				Tag:       "sha256-abc12345",
			},
		},
	}
	saveImageState(dir, state)

	stampCmd := imageStampCommand()
	ctx := stampCmd.Flags
	ctx.Parse([]string{"--dir", dir})
	if err := stampCmd.Run(nil, nil); err != nil {
		t.Fatalf("image stamp: %v", err)
	}

	written, err := os.ReadFile(intentPath)
	if err != nil {
		t.Fatalf("read stamped intent: %v", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(written, &doc); err != nil {
		t.Fatalf("parse stamped intent: %v", err)
	}

	assetsNode := findNode(doc.Content[0], "spec", "assets")
	if assetsNode == nil || len(assetsNode.Content) == 0 {
		t.Fatal("assets not found")
	}

	for _, assetNode := range assetsNode.Content {
		name := findNode(assetNode, "name")
		if name == nil || name.Value != "my-build" {
			continue
		}

		version := findNode(assetNode, "version")
		if version == nil || version.Value != "sha256-abc12345" {
			t.Errorf("expected version sha256-abc12345, got %q", version.Value)
		}

		props := findNode(assetNode, "properties")
		sourceImage := getStringNode(props, "sourceImage")
		if sourceImage != "registry.strata.local:5000/myapp:sha256-abc12345" {
			t.Errorf("expected sourceImage, got %q", sourceImage)
		}

		stampOnly := getStringNode(props, "stampOnly")
		if stampOnly != "" {
			t.Errorf("expected no stampOnly, got %q", stampOnly)
		}

		buildContext := getStringNode(props, "buildContext")
		if buildContext != "~/src/myapp" {
			t.Errorf("expected buildContext preserved, got %q", buildContext)
		}

		dockerfile := getStringNode(props, "dockerfile")
		if dockerfile != "Dockerfile" {
			t.Errorf("expected dockerfile preserved, got %q", dockerfile)
		}

		return
	}
	t.Fatal("my-build asset not found in stamped intent")
}

func TestImageStampIntentTarMode(t *testing.T) {
	dir := t.TempDir()
	intentsDir := filepath.Join(dir, "intents")
	os.MkdirAll(intentsDir, 0755)
	stateDir := filepath.Join(dir, ".state")
	os.MkdirAll(stateDir, 0755)

	originalYAML := `
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: images
spec:
  assets:
    - type: ImageBuild
      name: tar-build
      version: dev
      properties:
        buildContext: ~/src/myapp
        dockerfile: Dockerfile
        repository: myapp
`
	intentPath := filepath.Join(intentsDir, "images.yaml")
	os.WriteFile(intentPath, []byte(originalYAML), 0644)

	state := &imageState{
		Images: map[string]imageEntry{
			"tar-build": {
				AssetName:   "tar-build",
				Tag:         "sha256-abc12345",
				SourceImage: "myapp:sha256-abc12345",
				Repository:  "myapp",
				ImageRef:    "registry.strata.local:5000/myapp:sha256-abc12345",
			},
		},
	}
	saveImageState(dir, state)

	stampCmd := imageStampCommand()
	ctx := stampCmd.Flags
	ctx.Parse([]string{"--dir", dir})
	if err := stampCmd.Run(nil, nil); err != nil {
		t.Fatalf("image stamp tar: %v", err)
	}

	written, err := os.ReadFile(intentPath)
	if err != nil {
		t.Fatalf("read stamped intent: %v", err)
	}

	var doc yaml.Node
	yaml.Unmarshal(written, &doc)
	assetsNode := findNode(doc.Content[0], "spec", "assets")
	for _, assetNode := range assetsNode.Content {
		name := findNode(assetNode, "name")
		if name == nil || name.Value != "tar-build" {
			continue
		}
		props := findNode(assetNode, "properties")
		srcImg := getStringNode(props, "sourceImage")
		if srcImg != "registry.strata.local:5000/myapp:sha256-abc12345" {
			t.Errorf("expected sourceImage, got %q", srcImg)
		}
		buildContext := getStringNode(props, "buildContext")
		if buildContext != "~/src/myapp" {
			t.Errorf("expected buildContext preserved, got %q", buildContext)
		}
		return
	}
	t.Fatal("tar-build asset not found")
}

func TestImageCommandsRegistered(t *testing.T) {
	cmds := imageCommands()
	names := make(map[string]bool)
	for _, c := range cmds {
		if c.Cmd == nil {
			t.Errorf("nil command for %s %s", c.Group, c.Name)
		}
		if c.Group != "image" {
			t.Errorf("expected group 'image', got %s", c.Group)
		}
		if c.Cmd.Description == "" {
			t.Errorf("empty description for %s %s", c.Group, c.Name)
		}
		names[c.Name] = true
	}

	expected := []string{"build", "push", "stamp", "release"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("image commands missing '%s'", name)
		}
	}

	if len(cmds) != 4 {
		t.Errorf("expected 4 image commands, got %d", len(cmds))
	}
}

func TestNodeHelpers(t *testing.T) {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "key1"},
			{Kind: yaml.ScalarNode, Value: "val1"},
		},
	}

	if getStringNode(node, "key1") != "val1" {
		t.Error("getStringNode failed")
	}
	if getStringNode(node, "missing") != "" {
		t.Error("getStringNode should return empty for missing")
	}

	setStringNode(node, "key2", "val2")
	if getStringNode(node, "key2") != "val2" {
		t.Error("setStringNode failed")
	}

	setStringNode(node, "key1", "updated")
	if getStringNode(node, "key1") != "updated" {
		t.Error("setStringNode update failed")
	}

	removeNode(node, "key2")
	if getStringNode(node, "key2") != "" {
		t.Error("removeNode failed")
	}

	setBoolNode(node, "enabled", true)
	val := getStringNode(node, "enabled")
	if val != "true" {
		t.Errorf("setBoolNode: expected 'true', got %q", val)
	}

	removeNode(node, "enabled")
}

func TestSortedKeys(t *testing.T) {
	m := map[string]string{"c": "3", "a": "1", "b": "2"}
	keys := sortedKeys(m)
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Errorf("sortedKeys returned %v", keys)
	}
}

func TestResolveImageBuildArgs(t *testing.T) {
	state := &imageState{
		Images: map[string]imageEntry{
			"monofs-client-build": {
				AssetName:  "monofs-client-build",
				LocalTag:   "monofs-client:20260701-113740",
				Repository: "monofs-client",
				Registry:   "registry.strata.local:5000",
			},
			"pushed-build": {
				AssetName: "pushed-build",
				LocalTag:  "myapp:localsha",
				ImageRef:  "registry.strata.local:5000/myapp:sha256-abc123",
				Tag:       "sha256-abc123",
			},
		},
	}

	tests := []struct {
		name     string
		input    map[string]string
		expected map[string]string
	}{
		{
			name: "imageRef falls back to LocalTag when ImageRef is empty",
			input: map[string]string{
				"MONOFS_CLIENT_IMAGE": "${intent.images.outputs.monofs-client-build.imageRef}",
			},
			expected: map[string]string{
				"MONOFS_CLIENT_IMAGE": "monofs-client:20260701-113740",
			},
		},
		{
			name: "imageRef uses ImageRef when set",
			input: map[string]string{
				"BASE": "${intent.images.outputs.pushed-build.imageRef}",
			},
			expected: map[string]string{
				"BASE": "registry.strata.local:5000/myapp:sha256-abc123",
			},
		},
		{
			name: "localTag field",
			input: map[string]string{
				"LOCAL": "${intent.images.outputs.monofs-client-build.localTag}",
			},
			expected: map[string]string{
				"LOCAL": "monofs-client:20260701-113740",
			},
		},
		{
			name: "repository field",
			input: map[string]string{
				"REPO": "${intent.images.outputs.monofs-client-build.repository}",
			},
			expected: map[string]string{
				"REPO": "monofs-client",
			},
		},
		{
			name: "unknown asset leaves placeholder intact",
			input: map[string]string{
				"X": "${intent.images.outputs.nonexistent.imageRef}",
			},
			expected: map[string]string{
				"X": "${intent.images.outputs.nonexistent.imageRef}",
			},
		},
		{
			name: "non-placeholder values are unchanged",
			input: map[string]string{
				"GO_VERSION": "1.22",
			},
			expected: map[string]string{
				"GO_VERSION": "1.22",
			},
		},
		{
			name:     "nil state returns input unchanged",
			input:    map[string]string{"K": "${intent.images.outputs.x.imageRef}"},
			expected: map[string]string{"K": "${intent.images.outputs.x.imageRef}"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := state
			if tc.name == "nil state returns input unchanged" {
				st = nil
			}
			got := resolveImageBuildArgs(tc.input, st)
			for k, want := range tc.expected {
				if got[k] != want {
					t.Errorf("key %q: got %q, want %q", k, got[k], want)
				}
			}
		})
	}
}

func TestResolveImageBuildArgsUnresolved(t *testing.T) {
	// When the upstream asset is NOT yet in state, the placeholder must stay intact
	// so that the caller can detect and report it as an error rather than silently
	// passing a garbage value to Docker.
	state := &imageState{Images: map[string]imageEntry{}}

	got := resolveImageBuildArgs(map[string]string{
		"MONOFS_CLIENT_IMAGE": "${intent.images.outputs.monofs-client-build.imageRef}",
	}, state)

	want := "${intent.images.outputs.monofs-client-build.imageRef}"
	if got["MONOFS_CLIENT_IMAGE"] != want {
		t.Errorf("expected placeholder to be preserved, got %q", got["MONOFS_CLIENT_IMAGE"])
	}

	if !intentOutputRe.MatchString(got["MONOFS_CLIENT_IMAGE"]) {
		t.Errorf("intentOutputRe should match the unresolved placeholder so callers can detect it")
	}
}

func TestStampBuildArgRefs(t *testing.T) {
	state := &imageState{
		Images: map[string]imageEntry{
			"monofs-client-build": {
				AssetName: "monofs-client-build",
				ImageRef:  "registry.strata.local:5000/monofs-client:sha256-f010b1bac6082790",
			},
		},
	}

	// Build a propsNode with buildArgs containing a placeholder.
	propsNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "buildArgs"},
			{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "MONOFS_CLIENT_IMAGE"},
					{Kind: yaml.ScalarNode, Value: "${intent.images.outputs.monofs-client-build.imageRef}"},
					{Kind: yaml.ScalarNode, Value: "OTHER_ARG"},
					{Kind: yaml.ScalarNode, Value: "static-value"},
				},
			},
		},
	}

	modified := stampBuildArgRefs(propsNode, state)
	if !modified {
		t.Fatal("expected stampBuildArgRefs to report a modification")
	}

	buildArgsNode := findNode(propsNode, "buildArgs")
	got := buildArgsNode.Content[1].Value
	want := "registry.strata.local:5000/monofs-client:sha256-f010b1bac6082790"
	if got != want {
		t.Errorf("MONOFS_CLIENT_IMAGE: got %q, want %q", got, want)
	}
	if buildArgsNode.Content[3].Value != "static-value" {
		t.Error("static buildArg should be unchanged")
	}
}

func TestStampBuildArgRefsNoop(t *testing.T) {
	// Asset with no imageRef yet in state should leave placeholder intact.
	state := &imageState{Images: map[string]imageEntry{}}
	propsNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "buildArgs"},
			{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "MONOFS_CLIENT_IMAGE"},
					{Kind: yaml.ScalarNode, Value: "${intent.images.outputs.monofs-client-build.imageRef}"},
				},
			},
		},
	}

	modified := stampBuildArgRefs(propsNode, state)
	if modified {
		t.Error("stampBuildArgRefs should not modify when imageRef is missing from state")
	}
	buildArgsNode := findNode(propsNode, "buildArgs")
	if buildArgsNode.Content[1].Value != "${intent.images.outputs.monofs-client-build.imageRef}" {
		t.Error("placeholder should be preserved when imageRef missing")
	}
}

func TestDeduplicateStrings(t *testing.T) {
	in := []string{"a", "b", "a", "c", "b"}
	out := deduplicateStrings(in)
	if len(out) != 3 {
		t.Errorf("expected 3 unique, got %d: %v", len(out), out)
	}
}

func TestFindNode(t *testing.T) {
	yamlStr := `
spec:
  assets:
    - name: a
      type: ImageBuild
    - name: b
      type: Compute
`
	var doc yaml.Node
	yaml.Unmarshal([]byte(yamlStr), &doc)

	assets := findNode(doc.Content[0], "spec", "assets")
	if assets == nil || assets.Kind != yaml.SequenceNode {
		t.Fatal("findNode(spec.assets) failed")
	}
	if len(assets.Content) != 2 {
		t.Errorf("expected 2 assets, got %d", len(assets.Content))
	}

	missing := findNode(doc.Content[0], "spec", "nonexistent")
	if missing != nil {
		t.Error("findNode should return nil for missing path")
	}
}

func TestImageTagFormatting(t *testing.T) {
	tag := "sha256-abc12345"
	if len(tag) < 10 {
		t.Error("tag too short")
	}
	if tag[:7] != "sha256-" {
		t.Error("tag should start with sha256-")
	}
}

func TestImageStateJSONOutput(t *testing.T) {
	state := &imageState{
		Images: map[string]imageEntry{
			"test": {AssetName: "test", Tag: "sha256-abc12345", ImageRef: "reg/test:sha256-abc12345"},
		},
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) < 10 {
		t.Error("unexpectedly short output")
	}

	var loaded imageState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.Images["test"].Tag != "sha256-abc12345" {
		t.Errorf("roundtrip mismatch")
	}
}
