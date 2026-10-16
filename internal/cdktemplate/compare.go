// Package cdktemplate provides stable comparison and hashing of
// CloudFormation templates produced by CDK synthesis.
package cdktemplate

import (
	"fmt"

	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"sort"
	"strings"
)

// Compare diffs the desired CloudFormation template against the live observed
// template at resource granularity. It ignores non-semantic fields (resource
// Metadata, DependsOn) so CDK path metadata does not show up as drift.
func Compare(desiredJSON, observedJSON string) ([]string, error) {
	desired, err := parseTemplate(desiredJSON)
	if err != nil {
		return nil, err
	}
	observed, err := parseTemplate(observedJSON)
	if err != nil {
		return nil, err
	}
	desiredResources := resourcesOf(desired)
	observedResources := resourcesOf(observed)
	changed := map[string]struct{}{}
	for id, desiredResource := range desiredResources {
		observedResource, ok := observedResources[id]
		if !ok {
			changed["added:"+id] = struct{}{}
			continue
		}
		if resourceSignature(desiredResource) != resourceSignature(observedResource) {
			changed["changed:"+id] = struct{}{}
		}
	}
	for id := range observedResources {
		if _, ok := desiredResources[id]; !ok {
			changed["removed:"+id] = struct{}{}
		}
	}
	out := make([]string, 0, len(changed))
	for key := range changed {
		out = append(out, key)
	}
	sortStrings(out)
	return out, nil
}

func parseTemplate(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}, nil
	}
	var template map[string]any
	if err := json.Unmarshal([]byte(raw), &template); err == nil {
		return template, nil
	}
	// CloudFormation returns YAML templates verbatim for stacks deployed from
	// YAML sources, so accept both encodings.
	if err := yaml.Unmarshal([]byte(raw), &template); err != nil {
		return nil, fmt.Errorf("template is neither JSON nor YAML: %w", err)
	}
	return template, nil
}

func resourcesOf(template map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	resources, _ := template["Resources"].(map[string]any)
	for id, raw := range resources {
		resource, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out[id] = resource
	}
	return out
}

func resourceSignature(resource map[string]any) string {
	projected := map[string]any{
		"Type":       resource["Type"],
		"Properties": resource["Properties"],
	}
	if condition, ok := resource["Condition"]; ok {
		projected["Condition"] = condition
	}
	if policy, ok := resource["DeletionPolicy"]; ok {
		projected["DeletionPolicy"] = policy
	}
	encoded, _ := json.Marshal(projected)
	return string(encoded)
}

// Hash returns a stable content hash for a template.
func Hash(raw string) string {
	template, err := parseTemplate(raw)
	if err != nil {
		sum := sha256.Sum256([]byte(raw))
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	encoded, _ := json.Marshal(template)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sortStrings(values []string) {
	sort.Strings(values)
}
