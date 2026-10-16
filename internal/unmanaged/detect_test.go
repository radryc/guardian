package unmanaged

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/awsobserve"
	"github.com/rydzu/ainfra/guardian/internal/observed"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/internal/workspacepub"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type fakeLister struct {
	stacks   []awsobserve.StackState
	err      error
	lastAcct string
	lastReg  string
}

func (f *fakeLister) ListStacks(_ context.Context, account, region string) ([]awsobserve.StackState, error) {
	f.lastAcct = account
	f.lastReg = region
	if f.err != nil {
		return nil, f.err
	}
	return f.stacks, nil
}

func seedPartition(t *testing.T, store guardianapi.Store, partition string, files map[string]string) {
	t.Helper()
	writes := make([]guardianapi.PathWrite, 0, len(files))
	for path, content := range files {
		writes = append(writes, guardianapi.PathWrite{LogicalPath: path, Content: []byte(content)})
	}
	if _, err := store.UpsertFiles(context.Background(), guardianapi.MutationBatch{Writes: writes}); err != nil {
		t.Fatalf("seed partition: %v", err)
	}
}

const observedIntentYAML = `apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: aws-pusher-stack
spec:
  intentType: observed
  targetPusher: aws-123456789012
  target:
    account: "123456789012"
    region: eu-west-1
  assets:
    - type: CDKStack
      name: aws-pusher-stack
      payload:
        aws: /partitions/strata-aws/payloads/aws/aws-pusher-stack.yaml
`

const payloadManifestYAML = `sourceType: cdk-ts
prebuiltAssemblyDir: /partitions/strata-aws/payloads/aws/cdk.out
stackName: AwsPusherStack
stackID: AwsPusherStack
packageManager: none
`

const generatedIntentYAML = `apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  implementation: cdk
  targetPusher: aws-123456789012
  target:
    account: "123456789012"
    region: eu-west-1
  assets:
    - type: Compute
      name: api
      properties:
        image: nginx
`

func TestDetectClassifiesStacks(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	seedPartition(t, store, "strata-aws", map[string]string{
		"/partitions/strata-aws/config.yaml":                        "apiVersion: guardian/v1alpha1\nkind: Partition\nmetadata:\n  name: strata-aws\n",
		"/partitions/strata-aws/intents/aws-pusher-stack.yaml":      observedIntentYAML,
		"/partitions/strata-aws/payloads/aws/aws-pusher-stack.yaml": payloadManifestYAML,
	})
	// prod manages a stack but has no repo binding -> missing-repo.
	prodIntent := strings.ReplaceAll(observedIntentYAML,
		"/partitions/strata-aws/payloads/aws/aws-pusher-stack.yaml", "/partitions/prod/payloads/aws/pusher.yaml")
	prodIntent = strings.ReplaceAll(prodIntent, "name: aws-pusher-stack", "name: pusher")
	seedPartition(t, store, "prod", map[string]string{
		"/partitions/prod/config.yaml":              "apiVersion: guardian/v1alpha1\nkind: Partition\nmetadata:\n  name: prod\n",
		"/partitions/prod/intents/pusher.yaml":      prodIntent,
		"/partitions/prod/payloads/aws/pusher.yaml": strings.ReplaceAll(payloadManifestYAML, "AwsPusherStack", "ProdStack"),
	})
	// gen manages a generated stack and is repo-bound -> fully managed.
	seedPartition(t, store, "gen", map[string]string{
		"/partitions/gen/config.yaml":      "apiVersion: guardian/v1alpha1\nkind: Partition\nmetadata:\n  name: gen\n",
		"/partitions/gen/intents/api.yaml": generatedIntentYAML,
	})
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: "/.registry/repos/strata-aws.json",
			Content:     []byte(`{"name":"strata-aws","pusher":"aws-123456789012","bindings":[{"partition":"strata-aws"},{"partition":"gen"}]}`),
		}},
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	lister := &fakeLister{stacks: []awsobserve.StackState{
		{StackName: "AwsPusherStack", Region: "eu-west-1", StackStatus: "CREATE_COMPLETE", Account: "123456789012"},
		{StackName: "ProdStack", Region: "eu-west-1", StackStatus: "UPDATE_COMPLETE"},
		{StackName: "guardian-gen-api", Region: "eu-west-1", StackStatus: "UPDATE_COMPLETE"},
		{StackName: "ForeignStack", Region: "eu-west-1", StackStatus: "CREATE_COMPLETE", DriftStatus: "DRIFTED"},
		{StackName: "ForeignStack", Region: "us-east-1", StackStatus: "CREATE_COMPLETE"},
	}}

	result, err := Detect(ctx, store, []Pusher{{
		Name:    "aws-123456789012",
		Account: "123456789012",
		Region:  "eu-west-1",
		Client:  lister,
	}})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	got := map[string]Stack{}
	for _, stack := range result.Stacks {
		got[stack.Region+"|"+stack.StackName] = stack
	}
	if _, ok := got["eu-west-1|AwsPusherStack"]; ok {
		t.Error("fully managed stack must not be listed")
	}
	if _, ok := got["eu-west-1|guardian-gen-api"]; ok {
		t.Error("repo-bound generated stack must not be listed")
	}
	prod, ok := got["eu-west-1|ProdStack"]
	if !ok {
		t.Fatal("ProdStack must be listed as missing-repo")
	}
	if prod.State != StateMissingRepo || prod.Partition != "prod" {
		t.Errorf("ProdStack = %+v", prod)
	}
	foreign, ok := got["eu-west-1|ForeignStack"]
	if !ok || foreign.State != StateUnmanaged {
		t.Errorf("ForeignStack = %+v", foreign)
	}
	if foreign.DriftStatus != "DRIFTED" {
		t.Errorf("ForeignStack drift = %q", foreign.DriftStatus)
	}
	other, ok := got["us-east-1|ForeignStack"]
	if !ok || other.State != StateUnmanaged {
		t.Errorf("us-east-1 ForeignStack = %+v", other)
	}
	if lister.lastAcct != "123456789012" || lister.lastReg != "eu-west-1" {
		t.Errorf("lister called with %s/%s", lister.lastAcct, lister.lastReg)
	}
}

func TestDetectWarnsOnListerFailure(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	result, err := Detect(ctx, store, []Pusher{{
		Name:   "aws-1",
		Client: &fakeLister{err: context.DeadlineExceeded},
	}})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "aws-1") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestBuildAssociation(t *testing.T) {
	association, err := BuildAssociation(AssociateOptions{
		RepoURL:   "https://github.com/example/ops.git",
		Partition: "strata-aws",
		Pusher:    "aws-123456789012",
		Account:   "123456789012",
		Region:    "eu-west-1",
		StackName: "AwsPusherStack",
		Template:  `{"Resources":{"Q":{"Type":"AWS::SQS::Queue"}}}`,
	})
	if err != nil {
		t.Fatalf("BuildAssociation: %v", err)
	}
	if association.Branch != "guardian/adopt-strata-aws-awspusherstack" {
		t.Fatalf("branch = %q", association.Branch)
	}
	if association.Repo.Source != "https://github.com/example/ops.git" {
		t.Fatalf("repo source = %q", association.Repo.Source)
	}
	if len(association.Repo.Bindings) != 1 || association.Repo.Bindings[0].Partition != "strata-aws" {
		t.Fatalf("repo bindings = %+v", association.Repo.Bindings)
	}
	for _, path := range []string{
		"guardian/partitions/strata-aws/config.yaml",
		"guardian/partitions/strata-aws/intents/aws-pusher-stack.yaml",
		"guardian/partitions/strata-aws/payloads/aws/aws-pusher-stack.yaml",
		"guardian/partitions/strata-aws/payloads/aws/cdk.out/manifest.json",
		"guardian/partitions/strata-aws/payloads/aws/cdk.out/AwsPusherStack.template.json",
	} {
		if _, ok := association.Files[path]; !ok {
			t.Errorf("association missing %s", path)
		}
	}
}

func TestBuildAssociationDefaults(t *testing.T) {
	association, err := BuildAssociation(AssociateOptions{
		RepoURL:   "https://github.com/example/ops.git",
		Pusher:    "aws-1",
		Account:   "1",
		Region:    "eu-west-1",
		StackName: "My Stack",
		Template:  `{"Resources":{}}`,
	})
	if err != nil {
		t.Fatalf("BuildAssociation: %v", err)
	}
	if association.Partition != "aws-my-stack" {
		t.Fatalf("partition = %q", association.Partition)
	}
	if association.Repo.Name != "aws-my-stack" {
		t.Fatalf("repo name = %q", association.Repo.Name)
	}
	if !strings.HasPrefix(association.Branch, "guardian/adopt-aws-my-stack-") {
		t.Fatalf("branch = %q", association.Branch)
	}
	if _, ok := association.Files["guardian/partitions/aws-my-stack/config.yaml"]; !ok {
		t.Fatalf("files not prefixed: %v", len(association.Files))
	}
}

func TestBuildAssociationValidation(t *testing.T) {
	if _, err := BuildAssociation(AssociateOptions{}); err == nil {
		t.Fatal("expected repoUrl error")
	}
	if _, err := BuildAssociation(AssociateOptions{RepoURL: "x", StackName: "s", Pusher: "p"}); err == nil {
		t.Fatal("expected template error")
	}
}

func TestServiceDetectAndAssociate(t *testing.T) {
	ctx := context.Background()
	service, registryStore := newUnmanagedTestService(t, []awsobserve.StackState{
		{StackName: "ForeignStack", Region: "eu-west-1", StackStatus: "CREATE_COMPLETE"},
	})

	result, err := service.Detect(ctx)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(result.Stacks) != 1 || result.Stacks[0].StackName != "ForeignStack" {
		t.Fatalf("stacks = %+v", result.Stacks)
	}

	association, err := service.Associate(ctx, AssociateOptions{
		RepoURL:   "https://github.com/example/ops.git",
		Pusher:    "aws-123456789012",
		Account:   "123456789012",
		Region:    "eu-west-1",
		StackName: "ForeignStack",
	})
	if err != nil {
		t.Fatalf("Associate: %v", err)
	}
	if association.Repo.Name == "" {
		t.Fatal("association repo not registered")
	}
	repos, err := registryStore.ListRepos(ctx)
	if err != nil {
		t.Fatalf("list repos: %v", err)
	}
	if len(repos) != 1 || repos[0].Source != "https://github.com/example/ops.git" {
		t.Fatalf("repos = %+v", repos)
	}
	if _, ok := association.Files["guardian/partitions/aws-foreign-stack/payloads/aws/cdk.out/ForeignStack.template.json"]; !ok {
		t.Fatalf("association files = %v", len(association.Files))
	}
	if association.Publish != nil || association.PublishError != "" {
		t.Fatalf("association without publisher must not publish: %+v", association.Publish)
	}
}

func TestServiceAssociatePublishesThroughMonoFS(t *testing.T) {
	ctx := context.Background()
	publisher := &fakePublisher{result: &workspacepub.PublishResult{
		BundleID: "bundle-1",
		Repository: workspacepub.RepositoryResult{
			Status:             "published",
			Branch:             "main",
			TargetBranch:       "monofs/guardian-adopt-aws-foreign-stack-foreignstack/aws-foreign-stack",
			PullRequestURL:     "https://github.com/example/ops/pull/3",
			PullRequestCreated: true,
		},
	}}
	service, _ := newUnmanagedTestServiceWithPublisher(t, []awsobserve.StackState{
		{StackName: "ForeignStack", Region: "eu-west-1", StackStatus: "CREATE_COMPLETE"},
	}, publisher)

	association, err := service.Associate(ctx, AssociateOptions{
		RepoURL:   "https://github.com/example/ops.git",
		Pusher:    "aws-123456789012",
		Account:   "123456789012",
		Region:    "eu-west-1",
		StackName: "ForeignStack",
	})
	if err != nil {
		t.Fatalf("Associate: %v", err)
	}
	if publisher.calls != 1 {
		t.Fatalf("publish calls = %d", publisher.calls)
	}
	req := publisher.req
	if req.WorkspaceID != "guardian/adopt-aws-foreign-stack-foreignstack" {
		t.Fatalf("workspace id = %q", req.WorkspaceID)
	}
	if req.BranchStrategy != workspacepub.StrategyPerRepoBranch {
		t.Fatalf("strategy = %q", req.BranchStrategy)
	}
	if !strings.Contains(req.CommitMessage, "ForeignStack") || !strings.Contains(req.CommitMessage, "aws-foreign-stack") {
		t.Fatalf("commit message = %q", req.CommitMessage)
	}
	if req.AuthorName != "Guardian" {
		t.Fatalf("author = %q", req.AuthorName)
	}
	repo := req.Repository
	if repo.RepoURL != "https://github.com/example/ops.git" {
		t.Fatalf("repo url = %q", repo.RepoURL)
	}
	if repo.DisplayPath != "github.com/example/ops" {
		t.Fatalf("display path = %q", repo.DisplayPath)
	}
	if repo.StorageID != association.Repo.Name {
		t.Fatalf("storage id = %q, want %q", repo.StorageID, association.Repo.Name)
	}
	if len(repo.Operations) != len(association.Files) {
		t.Fatalf("operations = %d, files = %d", len(repo.Operations), len(association.Files))
	}
	for i := 1; i < len(repo.Operations); i++ {
		if repo.Operations[i-1].Path > repo.Operations[i].Path {
			t.Fatalf("operations not sorted: %q > %q", repo.Operations[i-1].Path, repo.Operations[i].Path)
		}
	}
	if repo.Operations[0].Kind != workspacepub.OperationUpsert {
		t.Fatalf("operation kind = %q", repo.Operations[0].Kind)
	}

	if association.Publish == nil {
		t.Fatal("association missing publish result")
	}
	if association.Publish.Repository.PullRequestURL != "https://github.com/example/ops/pull/3" {
		t.Fatalf("pr url = %q", association.Publish.Repository.PullRequestURL)
	}
	if association.Branch != "monofs/guardian-adopt-aws-foreign-stack-foreignstack/aws-foreign-stack" {
		t.Fatalf("association branch = %q", association.Branch)
	}
}

func TestServiceAssociateRecordsPublishFailure(t *testing.T) {
	ctx := context.Background()
	publisher := &fakePublisher{err: errors.New("push rejected: non-fast-forward")}
	service, _ := newUnmanagedTestServiceWithPublisher(t, []awsobserve.StackState{
		{StackName: "ForeignStack", Region: "eu-west-1", StackStatus: "CREATE_COMPLETE"},
	}, publisher)

	association, err := service.Associate(ctx, AssociateOptions{
		RepoURL:   "https://github.com/example/ops.git",
		Pusher:    "aws-123456789012",
		Account:   "123456789012",
		Region:    "eu-west-1",
		StackName: "ForeignStack",
	})
	if err != nil {
		t.Fatalf("Associate must not fail on publish errors: %v", err)
	}
	if association.PublishError == "" || !strings.Contains(association.PublishError, "non-fast-forward") {
		t.Fatalf("publish error = %q", association.PublishError)
	}
	if association.Publish != nil {
		t.Fatal("failed publish must not set a result")
	}
}

type fakePublisher struct {
	req    workspacepub.PublishRequest
	result *workspacepub.PublishResult
	err    error
	calls  int
}

func (f *fakePublisher) Publish(_ context.Context, req workspacepub.PublishRequest) (*workspacepub.PublishResult, error) {
	f.calls++
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// newUnmanagedTestService builds a service backed by a fake observed API.
func newUnmanagedTestService(t *testing.T, stacks []awsobserve.StackState) (*Service, *registry.Store) {
	t.Helper()
	return newUnmanagedTestServiceWithPublisher(t, stacks, nil)
}

func newUnmanagedTestServiceWithPublisher(t *testing.T, stacks []awsobserve.StackState, publisher Publisher) (*Service, *registry.Store) {
	t.Helper()
	store := memory.New()
	registryStore := registry.New(store)
	server := newObservedServer(t, stacks)
	t.Cleanup(server.Close)
	if err := registryStore.PutPusher(context.Background(), &registry.Pusher{
		APIVersion:     "guardian/v1alpha1",
		Kind:           "Pusher",
		Name:           "aws-123456789012",
		Account:        "123456789012",
		Region:         "eu-west-1",
		ObservedAPIURL: server.URL,
	}); err != nil {
		t.Fatalf("put pusher: %v", err)
	}
	return &Service{
		Store:     store,
		Registry:  registryStore,
		Resolver:  &observed.RegistryResolver{Registry: registryStore},
		Publisher: publisher,
	}, registryStore
}

// newObservedServer serves the pusher read API surface used by the service.
func newObservedServer(t *testing.T, stacks []awsobserve.StackState) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/aws/observed/stacks":
			_ = json.NewEncoder(w).Encode(map[string]any{"stacks": stacks})
		case strings.HasSuffix(r.URL.Path, "/template"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stackName": "ForeignStack",
				"template":  `{"Resources":{"Q":{"Type":"AWS::SQS::Queue"}}}`,
			})
		default:
			http.NotFound(w, r)
		}
	}))
}
