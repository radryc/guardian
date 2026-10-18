package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func seedUIPartition(t *testing.T, store guardianapi.Store, name, pusher, account string) {
	t.Helper()
	ctx := context.Background()
	_, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{
			{LogicalPath: paths.PartitionConfig(name), Content: []byte(`apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: ` + name + `
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: manual
`)},
			{LogicalPath: paths.IntentManifest(name, "web"), Content: []byte(`apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: web
spec:
  targetPusher: ` + pusher + `
  target:
    account: "` + account + `"
    region: us-east-1
  assets:
    - type: Network
      name: main
      properties:
        cidr: 10.42.0.0/16
`)},
		},
		Context: guardianapi.MutationContext{PrincipalID: "test"},
	})
	if err != nil {
		t.Fatalf("seed partition %s: %v", name, err)
	}
}

func TestIndexIncludesFourDestinationsThemePickerAndSetPanels(t *testing.T) {
	srv, _ := newRegistryTestServer(t)
	rec := registryRequest(t, srv, http.MethodGet, "/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="themeSelect"`,
		`>Midnight<`,
		`>Graphite<`,
		`>Ocean<`,
		`>Forest<`,
		`>Daylight<`,
		`>Connections<`,
		`>Adopt<`,
		`>Build<`,
		`id="partitionTabs"`,
		`id="setCardsSection"`,
		`id="setPanel"`,
		`id="assignmentCard"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index HTML missing %q", want)
		}
	}
}

func TestPartitionDetailIncludesAssignments(t *testing.T) {
	srv, store := newRegistryTestServer(t)
	seedUIPartition(t, store, "assign-demo", "aws-111111111111", "111111111111")
	reg := registry.New(store)
	if err := reg.PutPusher(context.Background(), &registry.Pusher{
		Name: "aws-111111111111", Account: "111111111111", Region: "us-east-1",
		ObservedAPIURL: "http://127.0.0.1:19090",
	}); err != nil {
		t.Fatalf("PutPusher: %v", err)
	}
	if err := reg.PutRepo(context.Background(), &registry.Repo{
		Name: "repo-demo", Pusher: "aws-111111111111",
		Source: "https://github.com/example/repo-demo.git", Branch: "main",
		Bindings: []registry.RepoBinding{{Partition: "assign-demo", Path: "/partitions/assign-demo/payloads/aws", Selector: "AssignDemo"}},
	}); err != nil {
		t.Fatalf("PutRepo: %v", err)
	}

	rec := registryRequest(t, srv, http.MethodGet, "/api/partitions/assign-demo", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET detail = %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Assignments struct {
			Repos []struct {
				Name        string `json:"name"`
				MonoFSPath  string `json:"monofsPath"`
				PayloadPath string `json:"payloadPath"`
			} `json:"repos"`
			Pushers []struct {
				Name       string   `json:"name"`
				Registered bool     `json:"registered"`
				Intents    []string `json:"intents"`
			} `json:"pushers"`
			Warnings []string `json:"warnings"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode detail: %v\n%s", err, rec.Body.String())
	}
	if len(payload.Assignments.Repos) != 1 {
		t.Fatalf("repos = %+v", payload.Assignments.Repos)
	}
	repo := payload.Assignments.Repos[0]
	if repo.Name != "repo-demo" || repo.MonoFSPath != "/partitions/assign-demo" || repo.PayloadPath != "/partitions/assign-demo/payloads/aws" {
		t.Errorf("assignment repo = %+v", repo)
	}
	if len(payload.Assignments.Pushers) != 1 || !payload.Assignments.Pushers[0].Registered {
		t.Fatalf("pushers = %+v", payload.Assignments.Pushers)
	}
	if len(payload.Assignments.Pushers[0].Intents) != 1 || payload.Assignments.Pushers[0].Intents[0] != "web" {
		t.Errorf("intent mapping = %+v", payload.Assignments.Pushers[0].Intents)
	}
	if len(payload.Assignments.Warnings) != 0 {
		t.Errorf("warnings = %v", payload.Assignments.Warnings)
	}
}

func TestSetsAPICountsChildrenAndUngrouped(t *testing.T) {
	srv, store := newRegistryTestServer(t)
	seedUIPartition(t, store, "set-a", "aws-111111111111", "111111111111")
	seedUIPartition(t, store, "set-b", "aws-111111111111", "111111111111")

	rec := registryRequest(t, srv, http.MethodPost, "/api/sets", map[string]any{"name": "Department Test"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create parent: %d %s", rec.Code, rec.Body.String())
	}
	var parent struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &parent)

	rec = registryRequest(t, srv, http.MethodPost, "/api/sets", map[string]any{"name": "Child Test", "parentId": parent.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("create child: %d %s", rec.Code, rec.Body.String())
	}
	var child struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &child)

	rec = registryRequest(t, srv, http.MethodPost, "/api/sets/"+child.ID+"/partitions", map[string]any{"add": []string{"set-a"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("add partition: %d %s", rec.Code, rec.Body.String())
	}
	rec = registryRequest(t, srv, http.MethodPatch, "/api/sets/"+parent.ID, map[string]any{"name": "Department Test", "pinned": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("pin parent: %d %s", rec.Code, rec.Body.String())
	}

	rec = registryRequest(t, srv, http.MethodGet, "/api/sets", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list sets: %d %s", rec.Code, rec.Body.String())
	}
	var response SetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode sets: %v", err)
	}
	byID := map[string]SetView{}
	for _, set := range response.Sets {
		byID[set.ID] = set
	}
	if got := byID[child.ID].PartitionCount; got != 1 {
		t.Errorf("child partitionCount = %d, want 1", got)
	}
	if got := byID[parent.ID].ChildCount; got != 1 {
		t.Errorf("parent childCount = %d, want 1", got)
	}
	if !byID[parent.ID].Pinned {
		t.Errorf("parent should be pinned")
	}
	if response.Sets[0].ID != parent.ID {
		t.Errorf("pinned set should sort first, got %q", response.Sets[0].ID)
	}
	if response.Ungrouped != 1 {
		t.Errorf("ungrouped = %d, want 1", response.Ungrouped)
	}
}
