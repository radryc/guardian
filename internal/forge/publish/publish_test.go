package publish

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/workspacepub"
)

type fakePublisher struct {
	request workspacepub.PublishRequest
	calls   int
	result  *workspacepub.PublishResult
	err     error
}

func (f *fakePublisher) Publish(_ context.Context, req workspacepub.PublishRequest) (*workspacepub.PublishResult, error) {
	f.calls++
	f.request = req
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &workspacepub.PublishResult{
		BundleID:    "bundle-1",
		JobID:       "job-1",
		JobState:    "WORKSPACE_SYNC_STATE_COMPLETED",
		WorkspaceID: req.WorkspaceID,
		Repository: workspacepub.RepositoryResult{
			StorageID:      req.Repository.StorageID,
			DisplayPath:    req.Repository.DisplayPath,
			TargetBranch:   "monofs/forge/repo-1",
			PushedCommit:   "abc123",
			Status:         "published",
			PullRequestURL: "https://github.com/org/repo/pull/7",
			Message:        "published",
		},
	}, nil
}

func validPartition(t *testing.T) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "partitions", "demo")
	write(t, filepath.Join(dir, "config.yaml"), `metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`)
	write(t, filepath.Join(dir, "intents", "api.yaml"), `apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  intentType: standard
  targetPusher: k8s-main
  target:
    cluster: k8s-main
    namespace: demo
  assets:
    - type: Compute
      name: api
      properties:
        image: ghcr.io/example/api:1
`)
	write(t, filepath.Join(dir, "alerts", "health.yaml"), `version: 1
groups:
  - name: health
    partition: demo
    rules:
      - alert: DemoDown
        expr: up == 0
        annotations:
          summary: demo is down
`)
	return root, dir
}

func TestSubmitPublishesAndRecordsProposal(t *testing.T) {
	root, dir := validPartition(t)
	fake := &fakePublisher{}

	result, err := Submit(context.Background(), Request{
		Root:             root,
		PartitionDir:     dir,
		Partition:        "demo",
		WorkspaceID:      "forge",
		RepoURL:          "https://github.com/org/repo.git",
		BaseBranch:       "main",
		CommitMessage:    "demo: add api",
		PullRequestTitle: "demo: add api",
		PullRequestBody:  "## Plan\n- create Compute api\n",
		AuthorName:       "Forge",
		AuthorEmail:      "forge@test",
	}, fake)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("publish calls = %d", fake.calls)
	}
	if fake.request.WorkspaceID != "forge" {
		t.Errorf("workspace = %q", fake.request.WorkspaceID)
	}
	if fake.request.BranchStrategy != workspacepub.StrategyPerRepoBranch {
		t.Errorf("strategy = %q", fake.request.BranchStrategy)
	}
	if fake.request.PullRequestTitle != "demo: add api" {
		t.Errorf("title = %q", fake.request.PullRequestTitle)
	}
	if fake.request.PullRequestBody != "## Plan\n- create Compute api\n" {
		t.Errorf("body = %q", fake.request.PullRequestBody)
	}
	if fake.request.Repository.DisplayPath != "github.com/org/repo" {
		t.Errorf("display path = %q", fake.request.Repository.DisplayPath)
	}
	if len(fake.request.Repository.Operations) != 3 {
		t.Errorf("operations = %d", len(fake.request.Repository.Operations))
	}
	if !result.Success || result.JobID != "job-1" || result.PullRequestURL == "" {
		t.Fatalf("result = %+v", result)
	}
	if result.StorageID == "" || result.StorageID != fake.request.Repository.StorageID {
		t.Fatalf("storage id = %q", result.StorageID)
	}

	proposals, err := LoadProposals(root)
	if err != nil {
		t.Fatalf("LoadProposals: %v", err)
	}
	if len(proposals) != 1 || proposals[0].Partition != "demo" || proposals[0].JobID != "job-1" {
		t.Fatalf("proposals = %+v", proposals)
	}
}

func TestSubmitDryRunDoesNotPublish(t *testing.T) {
	root, dir := validPartition(t)
	fake := &fakePublisher{}
	result, err := Submit(context.Background(), Request{
		Root:         root,
		PartitionDir: dir,
		Partition:    "demo",
		WorkspaceID:  "forge",
		RepoURL:      "https://github.com/org/repo.git",
		DryRun:       true,
	}, fake)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if fake.calls != 0 {
		t.Fatal("dry run must not publish")
	}
	if len(result.Operations) == 0 {
		t.Fatal("dry run must list operations")
	}
	if _, err := LoadProposals(root); err == nil {
		t.Fatal("dry run must not record a proposal")
	}
}

func TestSubmitRejectsInvalidPartition(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "partitions", "demo")
	write(t, filepath.Join(dir, "config.yaml"), "metadata: [\n")

	_, err := Submit(context.Background(), Request{
		Root:         root,
		PartitionDir: dir,
		Partition:    "demo",
		WorkspaceID:  "forge",
		RepoURL:      "https://github.com/org/repo.git",
	}, &fakePublisher{})
	if err == nil || !strings.Contains(err.Error(), "does not validate") {
		t.Fatalf("Submit error = %v", err)
	}
}

func TestSubmitRequiresRepositoryURL(t *testing.T) {
	root, dir := validPartition(t)
	_, err := Submit(context.Background(), Request{
		Root:         root,
		PartitionDir: dir,
		Partition:    "demo",
		WorkspaceID:  "forge",
	}, &fakePublisher{})
	if err == nil || !strings.Contains(err.Error(), "repository url") {
		t.Fatalf("Submit error = %v", err)
	}
}
