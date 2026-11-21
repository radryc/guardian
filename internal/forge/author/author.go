// Package author writes Guardian partition artifacts with comment-preserving
// YAML edits. Every write is validated before it is kept; on failure the
// original file is restored.
package author

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/radryc/guardian/internal/bundle"
	"github.com/radryc/guardian/internal/compiler/validator"
	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	partitiondomain "github.com/radryc/guardian/internal/domain/partition"
	targetdomain "github.com/radryc/guardian/internal/domain/target"
	"gopkg.in/yaml.v3"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// PartitionOptions controls partition scaffolding.
type PartitionOptions struct {
	Root           string
	Name           string
	PartitionsDir  string
	DeletionPolicy string
	Interval       string
	Force          bool
}

// CreatePartition writes partitions/<name>/config.yaml.
func CreatePartition(opts PartitionOptions) (string, error) {
	if !namePattern.MatchString(opts.Name) {
		return "", fmt.Errorf("invalid partition name %q", opts.Name)
	}
	root := opts.Root
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	partitionsDir := opts.PartitionsDir
	if strings.TrimSpace(partitionsDir) == "" {
		partitionsDir = filepath.Join(root, "partitions")
	}
	dir := filepath.Join(partitionsDir, opts.Name)
	path := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(path); err == nil && !opts.Force {
		return "", fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}
	policy := opts.DeletionPolicy
	if policy == "" {
		policy = "orphan"
	}
	interval := opts.Interval
	if interval == "" {
		interval = "30s"
	}
	part := partitiondomain.Partition{
		APIVersion: "guardian/v1alpha1",
		Kind:       "Partition",
		Metadata:   partitiondomain.Metadata{Name: opts.Name},
		Spec: partitiondomain.Spec{
			DeletionPolicy: policy,
			Reconciliation: partitiondomain.ReconciliationSpec{Mode: "auto", Interval: interval},
		},
	}
	if err := validator.ValidatePartition(&part); err != nil {
		return "", err
	}
	data, err := yaml.Marshal(part)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// IntentOptions controls intent scaffolding.
type IntentOptions struct {
	PartitionDir string
	Name         string
	Pusher       string
	Cluster      string
	Namespace    string
	Force        bool
}

// CreateIntent writes intents/<name>.yaml under a partition.
func CreateIntent(opts IntentOptions) (string, error) {
	if !namePattern.MatchString(opts.Name) {
		return "", fmt.Errorf("invalid intent name %q", opts.Name)
	}
	if strings.TrimSpace(opts.Pusher) == "" {
		return "", fmt.Errorf("--pusher is required")
	}
	dir := filepath.Join(opts.PartitionDir, "intents")
	path := filepath.Join(dir, opts.Name+".yaml")
	if _, err := os.Stat(path); err == nil && !opts.Force {
		return "", fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}
	intent := intentdomain.Intent{
		APIVersion: "guardian/v1alpha1",
		Kind:       "Intent",
		Metadata:   intentdomain.Metadata{Name: opts.Name},
		Spec: intentdomain.IntentSpec{
			IntentType:   intentdomain.TypeStandard,
			TargetPusher: opts.Pusher,
			Target:       targetFor(opts.Cluster, opts.Namespace),
			Assets:       []assetdomain.Spec{},
		},
	}
	if err := validator.ValidateIntent(&intent, nil, nil); err != nil {
		return "", err
	}
	data, err := yaml.Marshal(intent)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func targetFor(cluster, namespace string) targetdomain.Placement {
	return targetdomain.Placement{Cluster: cluster, Namespace: namespace}
}

// AddAsset appends an asset to an intent, preserving existing formatting.
func AddAsset(partitionDir, intentName string, asset assetdomain.Spec) (string, error) {
	path := filepath.Join(partitionDir, "intents", intentName+".yaml")
	doc, err := loadDocument(path)
	if err != nil {
		return "", err
	}
	root, err := rootMapping(doc)
	if err != nil {
		return "", err
	}
	spec, err := mappingChild(root, "spec", true)
	if err != nil {
		return "", err
	}
	assets, err := mappingChildSequence(spec, "assets")
	if err != nil {
		return "", err
	}
	if assets.Style == yaml.FlowStyle {
		assets.Style = 0
	}
	if sequenceContainsName(assets, asset.Name) {
		return "", fmt.Errorf("asset %q already exists in %s", asset.Name, path)
	}
	node, err := nodeFromAny(assetMap(asset))
	if err != nil {
		return "", err
	}
	assets.Content = append(assets.Content, node)
	return path, saveIntent(path, doc)
}

// ConnectAssets appends to to from's dependsOn list.
func ConnectAssets(partitionDir, intentName, from, to string) (string, error) {
	if from == to {
		return "", fmt.Errorf("an asset cannot depend on itself")
	}
	path := filepath.Join(partitionDir, "intents", intentName+".yaml")
	doc, err := loadDocument(path)
	if err != nil {
		return "", err
	}
	assets, err := assetsSequence(doc)
	if err != nil {
		return "", err
	}
	fromNode := findAssetNode(assets, from)
	if fromNode == nil {
		return "", fmt.Errorf("asset %q not found", from)
	}
	if findAssetNode(assets, to) == nil {
		return "", fmt.Errorf("asset %q not found", to)
	}
	dependsOn, err := mappingChildSequence(fromNode, "dependsOn")
	if err != nil {
		return "", err
	}
	if dependsOn.Style == yaml.FlowStyle {
		dependsOn.Style = 0
	}
	for _, item := range dependsOn.Content {
		if item.Value == to {
			return path, nil
		}
	}
	dependsOn.Content = append(dependsOn.Content, scalarNode(to))
	if hasDependencyCycle(assets, from, to) {
		return "", fmt.Errorf("adding %s -> %s creates a dependency cycle", from, to)
	}
	return path, saveIntent(path, doc)
}

// AddSecretRef sets properties.env[env] = {secret_ref: ref} on a Compute asset.
func AddSecretRef(partitionDir, intentName, assetName, env, ref string) (string, error) {
	if strings.TrimSpace(env) == "" || strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("--env and --ref are required")
	}
	path := filepath.Join(partitionDir, "intents", intentName+".yaml")
	doc, err := loadDocument(path)
	if err != nil {
		return "", err
	}
	assets, err := assetsSequence(doc)
	if err != nil {
		return "", err
	}
	asset := findAssetNode(assets, assetName)
	if asset == nil {
		return "", fmt.Errorf("asset %q not found", assetName)
	}
	properties, err := mappingChild(asset, "properties", true)
	if err != nil {
		return "", err
	}
	envMap, err := mappingChild(properties, "env", true)
	if err != nil {
		return "", err
	}
	valueNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			scalarNode("secret_ref"), scalarNode(ref),
		},
	}
	setMapping(envMap, env, valueNode)
	return path, saveIntent(path, doc)
}

// AddProbe appends a Probe asset targeting an existing asset.
func AddProbe(partitionDir, intentName string, spec assetdomain.Spec) (string, error) {
	return AddAsset(partitionDir, intentName, spec)
}

func assetMap(asset assetdomain.Spec) map[string]any {
	out := map[string]any{
		"type": asset.Type,
		"name": asset.Name,
	}
	if len(asset.DependsOn) > 0 {
		out["dependsOn"] = asset.DependsOn
	}
	if len(asset.Properties) > 0 {
		out["properties"] = asset.Properties
	}
	if len(asset.Payload) > 0 {
		out["payload"] = asset.Payload
	}
	return out
}

func loadDocument(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("%s is not a YAML document", path)
	}
	return &doc, nil
}

func saveIntent(path string, doc *yaml.Node) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if _, _, err := bundle.NormalizeIntentManifest(data, path, nil); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	// Defense in depth: restore if the on-disk bundle no longer loads.
	partitionDir := filepath.Dir(filepath.Dir(path))
	if _, err := bundle.Load(partitionDir, bundle.Options{ResolveIntentRefs: bundle.ResolveImageRefs}); err != nil {
		_ = os.WriteFile(path, original, 0o644)
		return err
	}
	return nil
}

func rootMapping(doc *yaml.Node) (*yaml.Node, error) {
	if doc == nil || len(doc.Content) == 0 {
		return nil, fmt.Errorf("empty YAML document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("YAML document root is not a mapping")
	}
	return root, nil
}

func mappingChildSequence(mapping *yaml.Node, key string) (*yaml.Node, error) {
	if mapping.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected a mapping for %q", key)
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			value := mapping.Content[i+1]
			if value.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("%q is not a sequence", key)
			}
			return value, nil
		}
	}
	value := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
	return value, nil
}

func mappingChild(mapping *yaml.Node, key string, create bool) (*yaml.Node, error) {
	if mapping.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected a mapping for %q", key)
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1], nil
		}
	}
	if !create {
		return nil, fmt.Errorf("key %q not found", key)
	}
	value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
	return value, nil
}

func assetsSequence(doc *yaml.Node) (*yaml.Node, error) {
	root, err := rootMapping(doc)
	if err != nil {
		return nil, err
	}
	spec, err := mappingChild(root, "spec", false)
	if err != nil {
		return nil, err
	}
	assets, err := mappingChild(spec, "assets", false)
	if err != nil {
		return nil, err
	}
	if assets.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("spec.assets is not a sequence")
	}
	return assets, nil
}

func findAssetNode(assets *yaml.Node, name string) *yaml.Node {
	for _, item := range assets.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(item.Content); i += 2 {
			if item.Content[i].Value == "name" && item.Content[i+1].Value == name {
				return item
			}
		}
	}
	return nil
}

func sequenceContainsName(seq *yaml.Node, name string) bool {
	return findAssetNode(seq, name) != nil
}

func setMapping(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func nodeFromAny(value any) (*yaml.Node, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, errors.New("failed to build YAML node")
	}
	return doc.Content[0], nil
}

func hasDependencyCycle(assets *yaml.Node, from, to string) bool {
	graph := map[string][]string{}
	for _, item := range assets.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		name := ""
		var deps []string
		for i := 0; i+1 < len(item.Content); i += 2 {
			switch item.Content[i].Value {
			case "name":
				name = item.Content[i+1].Value
			case "dependsOn":
				for _, dep := range item.Content[i+1].Content {
					deps = append(deps, dep.Value)
				}
			}
		}
		if name != "" {
			graph[name] = deps
		}
	}
	// The new edge is from -> to; a cycle exists if to already reaches from.
	seen := map[string]bool{}
	var reaches func(node string) bool
	reaches = func(node string) bool {
		if node == from {
			return true
		}
		if seen[node] {
			return false
		}
		seen[node] = true
		for _, dep := range graph[node] {
			if reaches(dep) {
				return true
			}
		}
		return false
	}
	return reaches(to)
}
