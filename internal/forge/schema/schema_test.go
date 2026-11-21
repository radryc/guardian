package schema

import (
	"encoding/json"
	"testing"
)

func TestDocuments(t *testing.T) {
	docs, err := Documents()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{PartitionFile, IntentFile, CatalogFile} {
		if len(docs[name]) == 0 {
			t.Fatalf("%s is empty", name)
		}
		var value any
		if err := json.Unmarshal(docs[name], &value); err != nil {
			t.Fatalf("%s is not JSON: %v", name, err)
		}
	}
}

func TestIntentSchemaHasAssetTypes(t *testing.T) {
	docs, err := Documents()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(docs[IntentFile], &doc); err != nil {
		t.Fatal(err)
	}
	defs := doc["$defs"].(map[string]any)
	asset := defs["asset"].(map[string]any)
	props := asset["properties"].(map[string]any)
	typeProp := props["type"].(map[string]any)
	values := typeProp["enum"].([]any)
	seen := map[string]bool{}
	for _, value := range values {
		seen[value.(string)] = true
	}
	for _, want := range []string{"Compute", "ImageBuild", "Database"} {
		if !seen[want] {
			t.Errorf("asset type enum missing %q", want)
		}
	}
}

func TestPartitionSchemaEnums(t *testing.T) {
	docs, err := Documents()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(docs[PartitionFile], &doc); err != nil {
		t.Fatal(err)
	}
	defs := doc["$defs"].(map[string]any)
	spec := defs["partitionSpec"].(map[string]any)
	props := spec["properties"].(map[string]any)
	policy := props["deletionPolicy"].(map[string]any)["enum"].([]any)
	if len(policy) != 2 || policy[0] != "orphan" || policy[1] != "destroy" {
		t.Fatalf("deletionPolicy enum = %v", policy)
	}
}
