package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type imageState struct {
	Images map[string]imageEntry `json:"images"`
}

type imageEntry struct {
	AssetName     string            `json:"assetName"`
	IntentFile    string            `json:"intentFile"`
	LocalTag      string            `json:"localTag"`
	Repository    string            `json:"repository"`
	Registry      string            `json:"registry,omitempty"`
	Dockerfile    string            `json:"dockerfile"`
	Context       string            `json:"context"`
	BuildArgs     map[string]string `json:"buildArgs,omitempty"`
	Target        string            `json:"target,omitempty"`
	Platform      string            `json:"platform,omitempty"`
	ImageRef      string            `json:"imageRef,omitempty"`
	Tag           string            `json:"tag,omitempty"`
	SourceImage   string            `json:"sourceImage,omitempty"`
	BuildContexts map[string]string `json:"buildContexts,omitempty"`
}

type imageBuildAsset struct {
	AssetName         string
	IntentFile        string
	Repository        string
	Registry          string
	BuildContext      string
	BuildContexts     map[string]string
	Dockerfile        string
	BuildArgs         map[string]string
	Target            string
	Platform          string
	SourceImage       string
	ImageFromUpstream string
}

func loadImageBuildAssets(dir string) ([]imageBuildAsset, error) {
	intentsDir := filepath.Join(dir, "intents")
	entries, err := os.ReadDir(intentsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read intents directory: %w", err)
	}

	var assets []imageBuildAsset
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		intentPath := filepath.Join(intentsDir, entry.Name())
		content, err := os.ReadFile(intentPath)
		if err != nil {
			return nil, fmt.Errorf("read intent file %s: %w", intentPath, err)
		}

		var doc yaml.Node
		if err := yaml.Unmarshal(content, &doc); err != nil {
			return nil, fmt.Errorf("parse intent file %s: %w", intentPath, err)
		}
		if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
			continue
		}

		assetsNode := findNode(doc.Content[0], "spec", "assets")
		if assetsNode == nil || assetsNode.Kind != yaml.SequenceNode {
			continue
		}

		for _, assetNode := range assetsNode.Content {
			typeNode := findNode(assetNode, "type")
			if typeNode == nil || typeNode.Value != "ImageBuild" {
				continue
			}
			propsNode := findNode(assetNode, "properties")
			if propsNode == nil || propsNode.Kind != yaml.MappingNode {
				continue
			}

			buildContext := getStringNode(propsNode, "buildContext")
			sourceImage := getStringNode(propsNode, "sourceImage")
			imageFromUpstream := getStringNode(propsNode, "imageFromUpstream")
			if buildContext == "" && sourceImage == "" && imageFromUpstream == "" {
				continue
			}

			nameNode := findNode(assetNode, "name")
			assetName := ""
			if nameNode != nil {
				assetName = nameNode.Value
			}

			buildArgsNode := findNode(propsNode, "buildArgs")
			var buildArgs map[string]string
			if buildArgsNode != nil && buildArgsNode.Kind == yaml.MappingNode {
				buildArgs = map[string]string{}
				for j := 0; j < len(buildArgsNode.Content); j += 2 {
					key := buildArgsNode.Content[j].Value
					val := buildArgsNode.Content[j+1].Value
					buildArgs[key] = val
				}
			}

			assets = append(assets, imageBuildAsset{
				AssetName:         assetName,
				IntentFile:        intentPath,
				Repository:        getStringNode(propsNode, "repository"),
				Registry:          getStringNode(propsNode, "registry"),
				BuildContext:      buildContext,
				BuildContexts:     getMapNode(propsNode, "buildContexts"),
				Dockerfile:        getStringNode(propsNode, "dockerfile"),
				Target:            getStringNode(propsNode, "target"),
				Platform:          getStringNode(propsNode, "platform"),
				SourceImage:       sourceImage,
				ImageFromUpstream: imageFromUpstream,
				BuildArgs:         buildArgs,
			})
		}
	}
	return assets, nil
}

func loadImageState(dir string) (*imageState, error) {
	statePath := filepath.Join(dir, ".state", "images.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, err
	}
	var state imageState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode image state: %w", err)
	}
	return &state, nil
}

func saveImageState(dir string, state *imageState) error {
	stateDir := filepath.Join(dir, ".state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, "images.json"), data, 0644)
}

func expandBuildContext(path string) string {
	if strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		if home != "" {
			path = strings.Replace(path, "~", home, 1)
		}
	}
	if strings.Contains(path, "$HOME") {
		home, _ := os.UserHomeDir()
		if home != "" {
			path = strings.ReplaceAll(path, "$HOME", home)
		}
	}
	return filepath.Clean(path)
}

func resolveDockerfile(dockerfile, buildContext, partitionDir string) string {
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if filepath.IsAbs(dockerfile) {
		return dockerfile
	}
	return filepath.Join(buildContext, dockerfile)
}

func tagFromSourceImage(sourceImage string) string {
	if idx := strings.LastIndex(sourceImage, ":"); idx >= 0 {
		return sourceImage[idx+1:]
	}
	return "latest"
}

func gitVersion(dir string) (tag, commit, buildTime string) {
	buildTime = time.Now().UTC().Format(time.RFC3339)
	if cmd := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD"); cmd != nil {
		out, err := cmd.Output()
		if err == nil {
			commit = strings.TrimSpace(string(out))
		}
	}
	if cmd := exec.Command("git", "-C", dir, "tag", "--points-at", "HEAD"); cmd != nil {
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			tag = strings.TrimSpace(strings.Split(string(out), "\n")[0])
		}
	}
	return
}

func findNode(parent *yaml.Node, path ...string) *yaml.Node {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return nil
	}
	remaining := path
	current := parent
	for len(remaining) > 0 {
		found := false
		for i := 0; i < len(current.Content); i += 2 {
			if current.Content[i].Value == remaining[0] {
				current = current.Content[i+1]
				remaining = remaining[1:]
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return current
}

func getStringNode(parent *yaml.Node, key string) string {
	for i := 0; i < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1].Value
		}
	}
	return ""
}

func setStringNode(parent *yaml.Node, key, value string) {
	for i := 0; i < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1].Value = value
			return
		}
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value},
	)
}

func removeNode(parent *yaml.Node, key string) {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
			return
		}
	}
}

func getMapNode(parent *yaml.Node, key string) map[string]string {
	for i := 0; i < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			node := parent.Content[i+1]
			if node.Kind != yaml.MappingNode {
				return nil
			}
			m := make(map[string]string, len(node.Content)/2)
			for j := 0; j < len(node.Content); j += 2 {
				m[node.Content[j].Value] = node.Content[j+1].Value
			}
			return m
		}
	}
	return nil
}

func imageCommands() []bootstrapReg {
	return []bootstrapReg{
		{Group: "image", Name: "build", Cmd: imageBuildCommand()},
		{Group: "image", Name: "push", Cmd: imagePushCommand()},
		{Group: "image", Name: "stamp", Cmd: imageStampCommand()},
		{Group: "image", Name: "release", Cmd: imageReleaseCommand()},
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func setBoolNode(parent *yaml.Node, key string, value bool) {
	for i := 0; i < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1].Value = fmt.Sprintf("%t", value)
			parent.Content[i+1].Tag = "!!bool"
			return
		}
	}
	valStr := fmt.Sprintf("%t", value)
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: valStr, Tag: "!!bool"},
	)
}

func deduplicateStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
