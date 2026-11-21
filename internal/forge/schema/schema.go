// Package schema generates JSON Schema documents for Guardian partition and
// intent manifests, plus the asset catalog used by editors.
package schema

import (
	"encoding/json"
	"fmt"

	"github.com/radryc/guardian/internal/domain/assets"
)

// Document names written by forge schema.
const (
	PartitionFile = "partition.schema.json"
	IntentFile    = "intent.schema.json"
	CatalogFile   = "catalog.json"
)

// Documents returns the generated schema set.
func Documents() (map[string][]byte, error) {
	catalog := assetTypes()
	out := map[string][]byte{}
	docs := map[string]any{
		PartitionFile: partitionSchema(catalog),
		IntentFile:    intentSchema(catalog),
		CatalogFile:   map[string]any{"assetTypes": assets.Catalog()},
	}
	for name, doc := range docs {
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", name, err)
		}
		out[name] = append(data, '\n')
	}
	return out, nil
}

func assetTypes() []string {
	templates := assets.Catalog()
	out := make([]string, 0, len(templates))
	for _, template := range templates {
		out = append(out, template.Type)
	}
	// assets.Catalog already sorts; keep a deterministic fallback.
	if len(out) == 0 {
		return []string{"Compute"}
	}
	return out
}

func partitionSchema(catalog []string) map[string]any {
	m := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://strata.dev/schemas/guardian/partition.json",
		"title":   "Guardian Partition",
		"type":    "object",
		"required": []string{
			"metadata", "spec",
		},
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": "guardian/v1alpha1"},
			"kind":       map[string]any{"type": "string", "const": "Partition"},
			"metadata":   ref("metadata"),
			"spec":       ref("partitionSpec"),
		},
		"$defs": map[string]any{
			"metadata": metadataSchema(),
			"partitionSpec": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"enabled":        map[string]any{"type": "boolean"},
					"deletionPolicy": enum("orphan", "destroy"),
					"reconciliation": map[string]any{
						"type":     "object",
						"required": []string{"mode"},
						"properties": map[string]any{
							"mode":          enum("auto", "manual", "readonly"),
							"interval":      map[string]any{"type": "string", "description": "Go duration or quartz cron expression"},
							"jitterPercent": map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
						},
					},
					"defaults": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"targetPusher":  map[string]any{"type": "string"},
							"target":        ref("target"),
							"observability": map[string]any{"type": "object"},
							"probes":        map[string]any{"type": "object"},
						},
					},
					"labels":     stringMap(),
					"sets":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"primarySet": map[string]any{"type": "string"},
					"quota":      map[string]any{"type": "object"},
				},
				"additionalProperties": false,
			},
			"target": targetSchema(),
		},
	}
	return m
}

func intentSchema(catalog []string) map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://strata.dev/schemas/guardian/intent.json",
		"title":   "Guardian Intent",
		"type":    "object",
		"required": []string{
			"metadata", "spec",
		},
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": "guardian/v1alpha1"},
			"kind":       map[string]any{"type": "string", "const": "Intent"},
			"metadata":   ref("metadata"),
			"spec":       ref("intentSpec"),
		},
		"$defs": map[string]any{
			"metadata": metadataSchema(),
			"target":   targetSchema(),
			"hint": map[string]any{
				"type":     "object",
				"required": []string{"path", "description"},
				"properties": map[string]any{
					"path":        map[string]any{"type": "string"},
					"title":       map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
				},
				"additionalProperties": false,
			},
			"intentSpec": map[string]any{
				"type":     "object",
				"required": []string{"targetPusher"},
				"properties": map[string]any{
					"intentType":     enum("standard", "observed"),
					"implementation": enum("native", "cdk"),
					"joins":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"targetPusher":   map[string]any{"type": "string"},
					"target":         ref("target"),
					"locked":         map[string]any{"type": "boolean"},
					"hints":          map[string]any{"type": "array", "items": ref("hint")},
					"assets":         map[string]any{"type": "array", "items": ref("asset")},
				},
				"additionalProperties": false,
			},
			"asset": map[string]any{
				"type":     "object",
				"required": []string{"type", "name"},
				"properties": map[string]any{
					"type":             enum(catalog...),
					"name":             map[string]any{"type": "string", "pattern": "^[a-z][a-z0-9-]*$"},
					"version":          map[string]any{"type": "string"},
					"dependsOn":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"flowMetricGroups": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"hints":            map[string]any{"type": "array", "items": ref("hint")},
					"payload":          stringMap(),
					"properties":       map[string]any{"type": "object"},
					"skip":             map[string]any{"type": "boolean"},
				},
				"additionalProperties": false,
			},
		},
	}
}

func metadataSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"name"},
		"properties": map[string]any{
			"name":   map[string]any{"type": "string", "pattern": "^[a-z][a-z0-9-]*$"},
			"labels": stringMap(),
		},
		"additionalProperties": false,
	}
}

func targetSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"cluster":   map[string]any{"type": "string"},
			"namespace": map[string]any{"type": "string"},
			"region":    map[string]any{"type": "string"},
			"account":   map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	}
}

func enum(values ...string) map[string]any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return map[string]any{"type": "string", "enum": items}
}

func stringMap() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
}

func ref(name string) map[string]any {
	return map[string]any{"$ref": "#/$defs/" + name}
}
