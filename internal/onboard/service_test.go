package onboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/observed"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/probes"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/internal/workspacepub"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func newFakePusher(t *testing.T, descriptors []any, probes []probes.Result) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/aws/onboarding":
			_ = json.NewEncoder(w).Encode(map[string]any{"descriptors": descriptors})
		case strings.HasPrefix(r.URL.Path, "/v1/aws/observed/stacks/") && strings.HasSuffix(r.URL.Path, "/template"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stackName": "CustomerAppStack",
				"template":  `{"Resources":{"Bucket":{"Type":"AWS::S3::Bucket"}}}`,
			})
		case r.URL.Path == "/v1/aws/probes":
			_ = json.NewEncoder(w).Encode(map[string]any{"probes": probes})
		default:
			http.NotFound(w, r)
		}
	}))
}

func seedPusher(t *testing.T, store guardianapi.Store, name, url string) {
	t.Helper()
	if err := registry.New(store).PutPusher(context.Background(), &registry.Pusher{
		Name:           name,
		Account:        "123456789012",
		Region:         "eu-west-1",
		ObservedAPIURL: url,
	}); err != nil {
		t.Fatalf("put pusher: %v", err)
	}
}

func readStore(t *testing.T, store guardianapi.Store, path string) string {
	t.Helper()
	raw, err := store.ReadFile(context.Background(), path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func TestServiceMaterializesObservedPartitionAndProbes(t *testing.T) {
	server := newFakePusher(t, []any{map[string]any{
		"stack":     "CustomerAppStack",
		"partition": "customer-prod-1",
		"intent":    "app",
	}}, []probes.Result{{Partition: "customer-prod-1", Intent: "app", Asset: "CustomerAppStack", Name: "health", Status: "up"}})
	defer server.Close()

	store := memory.New()
	seedPusher(t, store, "aws-123456789012", server.URL)
	service := &Service{
		Store:    store,
		Registry: registry.New(store),
		Resolver: observed.StaticResolver{"aws-123456789012": {Name: "aws-123456789012", Client: observed.NewClient(server.URL, "", time.Second)}},
		Now:      func() time.Time { return time.Unix(1700000000, 0) },
	}
	results, err := service.Run(context.Background(), Options{Pusher: "aws-123456789012"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].Error != "" {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Probes != 1 {
		t.Fatalf("probes = %d, want 1", results[0].Probes)
	}
	config := readStore(t, store, paths.PartitionConfig("customer-prod-1"))
	if strings.Contains(config, "guardian.io/origin") {
		t.Fatalf("onboarded partition should be observe-only, got %s", config)
	}
	intent := readStore(t, store, paths.IntentManifest("customer-prod-1", "app"))
	if !strings.Contains(intent, "intentType: observed") {
		t.Fatalf("intent = %s", intent)
	}
	if !strings.Contains(readStore(t, store, paths.ProbeSnapshot("aws-123456789012", "customer-prod-1")), "health") {
		t.Fatalf("probe snapshot missing")
	}
	if !strings.Contains(readStore(t, store, paths.OnboardingRecord("aws-123456789012", "CustomerAppStack")), "customer-prod-1") {
		t.Fatalf("onboarding audit record missing")
	}
	if !strings.Contains(readStore(t, store, paths.DashboardRecord("customer-prod-1")), "guardian_probe_up") {
		t.Fatalf("partition dashboard missing")
	}
}

func TestServicePreservesExistingPartitionConfig(t *testing.T) {
	server := newFakePusher(t, []any{map[string]any{
		"stack":     "CustomerAppStack",
		"partition": "customer-prod-1",
		"intent":    "app",
	}}, nil)
	defer server.Close()

	store := memory.New()
	seedPusher(t, store, "aws-123456789012", server.URL)
	existing := []byte("apiVersion: guardian/v1alpha1\nkind: Partition\nmetadata:\n  name: customer-prod-1\nspec:\n  deletionPolicy: orphan\n  reconciliation:\n    mode: readonly\n    interval: 5m\n")
	if _, err := store.UpsertFiles(context.Background(), guardianapi.MutationBatch{Writes: []guardianapi.PathWrite{{
		LogicalPath: paths.PartitionConfig("customer-prod-1"), Content: existing,
	}}}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	service := &Service{
		Store:    store,
		Registry: registry.New(store),
		Resolver: observed.StaticResolver{"aws-123456789012": {Name: "aws-123456789012", Client: observed.NewClient(server.URL, "", time.Second)}},
	}
	if _, err := service.Run(context.Background(), Options{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := readStore(t, store, paths.PartitionConfig("customer-prod-1")); got != string(existing) {
		t.Fatalf("config was overwritten:\n%s", got)
	}
}

type fakePublisher struct {
	req *workspacepub.PublishRequest
}

func (f *fakePublisher) Publish(_ context.Context, req workspacepub.PublishRequest) (*workspacepub.PublishResult, error) {
	f.req = &req
	return &workspacepub.PublishResult{Repository: workspacepub.RepositoryResult{Status: "published"}}, nil
}

func TestServicePublishesRepoWhenRequested(t *testing.T) {
	server := newFakePusher(t, []any{map[string]any{
		"stack":     "CustomerAppStack",
		"partition": "customer-prod-1",
		"intent":    "app",
		"publish":   map[string]any{"mode": "repo", "repo": "guardian-onboarding"},
	}}, nil)
	defer server.Close()

	store := memory.New()
	seedPusher(t, store, "aws-123456789012", server.URL)
	reg := registry.New(store)
	if err := reg.PutRepo(context.Background(), &registry.Repo{
		Name:   "guardian-onboarding",
		Source: "https://github.com/example/guardian-onboarding.git",
		Branch: "main",
	}); err != nil {
		t.Fatalf("put repo: %v", err)
	}
	publisher := &fakePublisher{}
	service := &Service{
		Store:     store,
		Registry:  reg,
		Resolver:  observed.StaticResolver{"aws-123456789012": {Name: "aws-123456789012", Client: observed.NewClient(server.URL, "", time.Second)}},
		Publisher: publisher,
	}
	if _, err := service.Run(context.Background(), Options{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if publisher.req == nil {
		t.Fatalf("publisher was not called")
	}
	if !strings.Contains(publisher.req.Repository.Operations[0].Path, "guardian/partitions/customer-prod-1/") {
		t.Fatalf("operation path = %q", publisher.req.Repository.Operations[0].Path)
	}
	if publisher.req.Repository.RepoURL == "" {
		t.Fatalf("repo URL was not forwarded")
	}
}

func TestServiceFiltersStack(t *testing.T) {
	server := newFakePusher(t, []any{
		map[string]any{"stack": "A", "partition": "p", "intent": "a"},
		map[string]any{"stack": "B", "partition": "p", "intent": "b"},
	}, nil)
	defer server.Close()

	store := memory.New()
	seedPusher(t, store, "aws-123456789012", server.URL)
	service := &Service{
		Store:    store,
		Registry: registry.New(store),
		Resolver: observed.StaticResolver{"aws-123456789012": {Name: "aws-123456789012", Client: observed.NewClient(server.URL, "", time.Second)}},
	}
	results, err := service.Run(context.Background(), Options{Stack: "A"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(results) != 1 || results[0].Stack != "A" {
		t.Fatalf("results = %+v", results)
	}
}
