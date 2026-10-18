package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestSetsAPI(t *testing.T) {
	srv, store := newRegistryTestServer(t)
	ctx := context.Background()
	_, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{
			{LogicalPath: paths.PartitionConfig("demo-deploy"), Content: []byte("apiVersion: guardian/v1alpha1\nkind: Partition\nmetadata:\n  name: demo-deploy\n")},
		},
		Context: guardianapi.MutationContext{PrincipalID: "test"},
	})
	if err != nil {
		t.Fatalf("seed partition: %v", err)
	}

	rec := registryRequest(t, srv, http.MethodPost, "/api/sets", map[string]any{
		"name": "Department X — AWS Production", "description": "prod",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create parent: %d %s", rec.Code, rec.Body.String())
	}
	var parent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &parent)
	parentID, _ := parent["id"].(string)
	if parentID == "" {
		t.Fatalf("parent id missing: %s", rec.Body.String())
	}

	rec = registryRequest(t, srv, http.MethodPost, "/api/sets", map[string]any{"name": "Payments", "parentId": parentID})
	if rec.Code != http.StatusOK {
		t.Fatalf("create child: %d %s", rec.Code, rec.Body.String())
	}
	var child map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &child)
	childID, _ := child["id"].(string)

	rec = registryRequest(t, srv, http.MethodPost, "/api/sets/"+childID+"/partitions", map[string]any{
		"add": []string{"demo-deploy"}, "primary": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add partition: %d %s", rec.Code, rec.Body.String())
	}
	rec = registryRequest(t, srv, http.MethodGet, "/api/sets", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Payments") || !strings.Contains(rec.Body.String(), "demo-deploy") {
		t.Fatalf("list sets: %d %s", rec.Code, rec.Body.String())
	}

	// Cycle and non-empty delete are rejected.
	rec = registryRequest(t, srv, http.MethodPost, "/api/sets/"+parentID+"/move", map[string]any{"parentId": childID})
	if rec.Code != http.StatusConflict {
		t.Fatalf("cycle move: %d %s", rec.Code, rec.Body.String())
	}
	rec = registryRequest(t, srv, http.MethodDelete, "/api/sets/"+childID, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete non-empty: %d %s", rec.Code, rec.Body.String())
	}

	// Remove the member and delete both sets.
	rec = registryRequest(t, srv, http.MethodPost, "/api/sets/"+childID+"/partitions", map[string]any{"remove": []string{"demo-deploy"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("remove partition: %d %s", rec.Code, rec.Body.String())
	}
	if rec = registryRequest(t, srv, http.MethodDelete, "/api/sets/"+childID, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete child: %d %s", rec.Code, rec.Body.String())
	}
	if rec = registryRequest(t, srv, http.MethodDelete, "/api/sets/"+parentID, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete parent: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSetsAPIPartitionMustExist(t *testing.T) {
	srv, _ := newRegistryTestServer(t)
	rec := registryRequest(t, srv, http.MethodPost, "/api/sets", map[string]any{"name": "Payments"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var set map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &set)
	id, _ := set["id"].(string)
	rec = registryRequest(t, srv, http.MethodPost, "/api/sets/"+id+"/partitions", map[string]any{"add": []string{"missing"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing partition: %d %s", rec.Code, rec.Body.String())
	}
}
