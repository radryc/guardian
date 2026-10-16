package workspacepub

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	pb "github.com/radryc/monofs/api/proto"
	"google.golang.org/grpc"
)

type fakeUploadStream struct {
	grpc.ClientStream
	chunks      []*pb.WorkspaceBundleChunk
	workspaceID string
}

func (f *fakeUploadStream) Send(chunk *pb.WorkspaceBundleChunk) error {
	f.chunks = append(f.chunks, chunk)
	if chunk.GetWorkspaceId() != "" {
		f.workspaceID = chunk.GetWorkspaceId()
	}
	return nil
}

func (f *fakeUploadStream) CloseAndRecv() (*pb.UploadWorkspaceBundleResponse, error) {
	return &pb.UploadWorkspaceBundleResponse{BundleId: "bundle-1", WorkspaceId: f.workspaceID}, nil
}

type fakePublishStream struct {
	grpc.ClientStream
	events []*pb.WorkspaceSyncEvent
	index  int
}

func (f *fakePublishStream) Recv() (*pb.WorkspaceSyncEvent, error) {
	if f.index >= len(f.events) {
		return nil, io.EOF
	}
	event := f.events[f.index]
	f.index++
	return event, nil
}

type fakeRouter struct {
	upload     *fakeUploadStream
	publish    *fakePublishStream
	uploadErr  error
	publishErr error
	lastReq    *pb.PublishWorkspaceRequest
}

func (f *fakeRouter) UploadWorkspaceBundle(context.Context, ...grpc.CallOption) (grpc.ClientStreamingClient[pb.WorkspaceBundleChunk, pb.UploadWorkspaceBundleResponse], error) {
	if f.uploadErr != nil {
		return nil, f.uploadErr
	}
	if f.upload == nil {
		f.upload = &fakeUploadStream{}
	}
	return f.upload, nil
}

func (f *fakeRouter) PublishWorkspace(_ context.Context, in *pb.PublishWorkspaceRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[pb.WorkspaceSyncEvent], error) {
	f.lastReq = in
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	if f.publish == nil {
		f.publish = &fakePublishStream{}
	}
	return f.publish, nil
}

func publishedEvents() []*pb.WorkspaceSyncEvent {
	return []*pb.WorkspaceSyncEvent{
		{EventType: pb.WorkspaceSyncEventType_WORKSPACE_SYNC_EVENT_JOB_ACCEPTED, Message: "job accepted"},
		{
			EventType: pb.WorkspaceSyncEventType_WORKSPACE_SYNC_EVENT_REPOSITORY_COMPLETED,
			Message:   "repository published",
			Repository: &pb.WorkspaceSyncRepositoryResult{
				StorageId:          "strata-aws",
				DisplayPath:        "github.com/example/strata",
				RepoUrl:            "https://github.com/example/strata.git",
				Branch:             "main",
				TargetBranch:       "monofs/guardian-adopt-strata-aws-awspusherstack/strata-aws",
				PushedCommit:       "abc123",
				Status:             pb.WorkspaceSyncRepositoryStatus_WORKSPACE_SYNC_REPOSITORY_STATUS_PUBLISHED,
				PullRequestUrl:     "https://github.com/example/strata/pull/7",
				PullRequestCreated: true,
			},
		},
		{EventType: pb.WorkspaceSyncEventType_WORKSPACE_SYNC_EVENT_JOB_COMPLETED, Message: "job completed"},
	}
}

func TestPublishUploadsBundleAndStartsReviewBranch(t *testing.T) {
	router := &fakeRouter{publish: &fakePublishStream{events: publishedEvents()}}
	publisher := New(router)

	large := strings.Repeat("x", chunkSize+17) // force more than one chunk
	result, err := publisher.Publish(context.Background(), PublishRequest{
		WorkspaceID:   "guardian/adopt-strata-aws-awspusherstack",
		CommitMessage: "Guardian adoption: AwsPusherStack (strata-aws)",
		AuthorName:    "Guardian",
		AuthorEmail:   "guardian@local",
		Repository: Repository{
			StorageID:   "strata-aws",
			DisplayPath: "github.com/example/strata",
			RepoURL:     "https://github.com/example/strata.git",
			Operations: []Operation{
				{Kind: OperationUpsert, Path: "guardian/partitions/strata-aws/config.yaml", Content: []byte("kind: Partition\n")},
				{Kind: OperationUpsert, Path: "guardian/partitions/strata-aws/payloads/big.txt", Content: []byte(large)},
			},
		},
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if router.upload == nil || len(router.upload.chunks) < 2 {
		t.Fatalf("expected multiple upload chunks, got %d", len(router.upload.chunks))
	}
	for i, chunk := range router.upload.chunks {
		wantLast := i == len(router.upload.chunks)-1
		if chunk.GetIsLast() != wantLast {
			t.Errorf("chunk %d is_last = %v, want %v", i, chunk.GetIsLast(), wantLast)
		}
		if chunk.GetWorkspaceId() != "guardian/adopt-strata-aws-awspusherstack" {
			t.Errorf("chunk %d workspace = %q", i, chunk.GetWorkspaceId())
		}
	}

	var bundle bundleJSON
	assembled := []byte{}
	for _, chunk := range router.upload.chunks {
		assembled = append(assembled, chunk.GetData()...)
	}
	if err := json.Unmarshal(assembled, &bundle); err != nil {
		t.Fatalf("decode uploaded bundle: %v", err)
	}
	if bundle.WorkspaceID != "guardian/adopt-strata-aws-awspusherstack" {
		t.Fatalf("bundle workspace = %q", bundle.WorkspaceID)
	}
	if len(bundle.Repositories) != 1 {
		t.Fatalf("bundle repositories = %d", len(bundle.Repositories))
	}
	repo := bundle.Repositories[0]
	if repo.StorageID != "strata-aws" || repo.DisplayPath != "github.com/example/strata" || repo.RepoURL == "" {
		t.Fatalf("bundle repository = %+v", repo)
	}
	if len(repo.Operations) != 2 {
		t.Fatalf("bundle operations = %d", len(repo.Operations))
	}
	if string(repo.Operations[1].Content) != large {
		t.Fatal("large operation content did not round-trip")
	}

	if router.lastReq == nil {
		t.Fatal("publish request was not sent")
	}
	if router.lastReq.GetBundleId() != "bundle-1" {
		t.Fatalf("bundle id = %q", router.lastReq.GetBundleId())
	}
	if router.lastReq.GetRequestedBranchStrategy() != StrategyPerRepoBranch {
		t.Fatalf("strategy = %q", router.lastReq.GetRequestedBranchStrategy())
	}
	if router.lastReq.GetLogicalCommitMessage() != "Guardian adoption: AwsPusherStack (strata-aws)" {
		t.Fatalf("commit message = %q", router.lastReq.GetLogicalCommitMessage())
	}
	if router.lastReq.GetAuthorName() != "Guardian" {
		t.Fatalf("author = %q", router.lastReq.GetAuthorName())
	}

	if result.Repository.Status != "published" {
		t.Fatalf("status = %q", result.Repository.Status)
	}
	if result.Repository.Branch != "main" {
		t.Fatalf("base branch = %q", result.Repository.Branch)
	}
	if result.Repository.TargetBranch != "monofs/guardian-adopt-strata-aws-awspusherstack/strata-aws" {
		t.Fatalf("target branch = %q", result.Repository.TargetBranch)
	}
	if result.Repository.PullRequestURL != "https://github.com/example/strata/pull/7" {
		t.Fatalf("pr url = %q", result.Repository.PullRequestURL)
	}
	if !result.Repository.PullRequestCreated {
		t.Fatal("expected pull request created")
	}
	if len(result.Events) != 3 {
		t.Fatalf("events = %v", result.Events)
	}
}

func TestPublishDefaultsStrategyAndStorageID(t *testing.T) {
	router := &fakeRouter{publish: &fakePublishStream{events: publishedEvents()}}
	publisher := New(router)
	if _, err := publisher.Publish(context.Background(), PublishRequest{
		WorkspaceID: "ws-1",
		Repository: Repository{
			RepoURL:    "https://github.com/example/strata.git",
			Operations: []Operation{{Path: "file.txt", Content: []byte("x")}},
		},
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if router.lastReq.GetRequestedBranchStrategy() != StrategyPerRepoBranch {
		t.Fatalf("default strategy = %q", router.lastReq.GetRequestedBranchStrategy())
	}
	var bundle bundleJSON
	assembled := []byte{}
	for _, chunk := range router.upload.chunks {
		assembled = append(assembled, chunk.GetData()...)
	}
	if err := json.Unmarshal(assembled, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if bundle.Repositories[0].StorageID != "ws-1" {
		t.Fatalf("storage id = %q", bundle.Repositories[0].StorageID)
	}
	if bundle.Repositories[0].Operations[0].Kind != OperationUpsert {
		t.Fatalf("operation kind = %q", bundle.Repositories[0].Operations[0].Kind)
	}
}

func TestPublishValidation(t *testing.T) {
	publisher := New(&fakeRouter{})
	cases := []PublishRequest{
		{Repository: Repository{RepoURL: "x", Operations: []Operation{{Path: "a"}}}},
		{WorkspaceID: "ws", Repository: Repository{Operations: []Operation{{Path: "a"}}}},
		{WorkspaceID: "ws", Repository: Repository{RepoURL: "x"}},
	}
	for i, req := range cases {
		if _, err := publisher.Publish(context.Background(), req); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
	if _, err := (*Publisher)(nil).Publish(context.Background(), PublishRequest{}); err == nil {
		t.Error("nil publisher must fail")
	}
}

func TestPublishRequiresRepositoryResult(t *testing.T) {
	router := &fakeRouter{publish: &fakePublishStream{events: []*pb.WorkspaceSyncEvent{
		{Message: "job accepted"},
		{Message: "job completed"},
	}}}
	publisher := New(router)
	_, err := publisher.Publish(context.Background(), PublishRequest{
		WorkspaceID: "ws",
		Repository: Repository{
			RepoURL:    "https://github.com/example/strata.git",
			Operations: []Operation{{Path: "a", Content: []byte("x")}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "no repository result") {
		t.Fatalf("expected missing result error, got %v", err)
	}
}
