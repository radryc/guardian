// Package workspacepub publishes repository changes through MonoFS: it uploads
// a workspace bundle to the router and starts a publish job that pushes a
// review branch, where MonoFS opens the MR/PR. Guardian's repo association
// (adoption) flow uses this to push the observed snapshot into a repository.
package workspacepub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	pb "github.com/radryc/monofs/api/proto"
	"google.golang.org/grpc"
)

// chunkSize is the bundle upload chunk size (matching the MonoFS client).
const chunkSize = 1 << 20

// RouterClient is the MonoFS router surface the publisher needs.
type RouterClient interface {
	UploadWorkspaceBundle(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[pb.WorkspaceBundleChunk, pb.UploadWorkspaceBundleResponse], error)
	PublishWorkspace(ctx context.Context, in *pb.PublishWorkspaceRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.WorkspaceSyncEvent], error)
}

// Publisher publishes repository changes through the MonoFS router.
type Publisher struct {
	router RouterClient
}

// New builds a publisher around a MonoFS router client.
func New(router RouterClient) *Publisher {
	return &Publisher{router: router}
}

// Branch strategies understood by MonoFS.
const (
	// StrategyDirect pushes straight to the base branch.
	StrategyDirect = "direct"
	// StrategyWorkspaceBranch pushes to monofs/<workspace>/<job>.
	StrategyWorkspaceBranch = "workspace_branch"
	// StrategyPerRepoBranch pushes to monofs/<workspace>/<repository>; the
	// branch is deterministic per repository so re-runs update the same
	// review branch instead of opening duplicates.
	StrategyPerRepoBranch = "per_repo_branch"
)

// PublishRequest describes one repository publish.
type PublishRequest struct {
	WorkspaceID    string
	BranchStrategy string
	CommitMessage  string
	AuthorName     string
	AuthorEmail    string
	Repository     Repository
}

// Repository is the bundle repository entry.
type Repository struct {
	StorageID   string
	DisplayPath string
	RepoURL     string
	// Branch is the base branch. Empty means the remote default branch (HEAD).
	Branch string
	// BaseCommit is the expected remote head. Empty publishes on top of the
	// current remote head; the push guards against non-fast-forward races.
	BaseCommit string
	Operations []Operation
}

// Operation kinds accepted by MonoFS workspace bundles.
const (
	OperationUpsert = "upsert"
	OperationDelete = "delete"
)

// Operation is one file operation in the bundle.
type Operation struct {
	Kind    string
	Path    string
	Content []byte
	Mode    int64
}

// PublishResult is the outcome for the published repository.
type PublishResult struct {
	BundleID   string           `json:"bundleId"`
	Repository RepositoryResult `json:"repository"`
	Events     []string         `json:"events,omitempty"`
}

// RepositoryResult mirrors the MonoFS workspace repository result.
type RepositoryResult struct {
	StorageID          string `json:"storageId"`
	DisplayPath        string `json:"displayPath"`
	RepoURL            string `json:"repoUrl"`
	Branch             string `json:"branch"` // base branch (resolved from HEAD when unknown)
	TargetBranch       string `json:"targetBranch"`
	PushedCommit       string `json:"pushedCommit"`
	Status             string `json:"status"` // published | unchanged | conflict | failed
	Message            string `json:"message"`
	PullRequestURL     string `json:"pullRequestUrl"`
	PullRequestCreated bool   `json:"pullRequestCreated"`
}

// Publish uploads the bundle and runs the publish job to completion.
func (p *Publisher) Publish(ctx context.Context, req PublishRequest) (*PublishResult, error) {
	if p == nil || p.router == nil {
		return nil, fmt.Errorf("workspace publisher is not configured")
	}
	if strings.TrimSpace(req.WorkspaceID) == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	if strings.TrimSpace(req.Repository.RepoURL) == "" {
		return nil, fmt.Errorf("repository repo_url is required")
	}
	if len(req.Repository.Operations) == 0 {
		return nil, fmt.Errorf("repository must include at least one operation")
	}

	bundle := bundleJSON{
		WorkspaceID: req.WorkspaceID,
		Repositories: []repositoryJSON{{
			StorageID:   firstNonEmpty(req.Repository.StorageID, req.WorkspaceID),
			DisplayPath: req.Repository.DisplayPath,
			RepoURL:     req.Repository.RepoURL,
			Branch:      req.Repository.Branch,
			BaseCommit:  req.Repository.BaseCommit,
			Operations:  make([]operationJSON, 0, len(req.Repository.Operations)),
		}},
	}
	for _, op := range req.Repository.Operations {
		kind := strings.TrimSpace(op.Kind)
		if kind == "" {
			kind = OperationUpsert
		}
		bundle.Repositories[0].Operations = append(bundle.Repositories[0].Operations, operationJSON{
			Kind:    kind,
			Path:    op.Path,
			Content: op.Content,
			Mode:    op.Mode,
		})
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		return nil, fmt.Errorf("encode workspace bundle: %w", err)
	}

	bundleID, err := p.upload(ctx, req.WorkspaceID, raw)
	if err != nil {
		return nil, err
	}

	strategy := strings.TrimSpace(req.BranchStrategy)
	if strategy == "" {
		strategy = StrategyPerRepoBranch
	}
	stream, err := p.router.PublishWorkspace(ctx, &pb.PublishWorkspaceRequest{
		WorkspaceId:             req.WorkspaceID,
		BundleId:                bundleID,
		LogicalCommitMessage:    req.CommitMessage,
		AuthorName:              req.AuthorName,
		AuthorEmail:             req.AuthorEmail,
		RequestedBranchStrategy: strategy,
	})
	if err != nil {
		return nil, fmt.Errorf("start workspace publish: %w", err)
	}

	result := &PublishResult{BundleID: bundleID}
	for {
		event, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return nil, fmt.Errorf("workspace publish stream: %w", recvErr)
		}
		if message := strings.TrimSpace(event.GetMessage()); message != "" {
			result.Events = append(result.Events, message)
		}
		if repo := event.GetRepository(); repo != nil {
			result.Repository = repositoryResult(repo)
		}
	}
	if result.Repository.Status == "" {
		return nil, fmt.Errorf("workspace publish produced no repository result")
	}
	return result, nil
}

func (p *Publisher) upload(ctx context.Context, workspaceID string, bundle []byte) (string, error) {
	stream, err := p.router.UploadWorkspaceBundle(ctx)
	if err != nil {
		return "", fmt.Errorf("open workspace bundle upload: %w", err)
	}
	for offset := 0; offset < len(bundle); offset += chunkSize {
		end := offset + chunkSize
		if end > len(bundle) {
			end = len(bundle)
		}
		chunk := &pb.WorkspaceBundleChunk{
			WorkspaceId: workspaceID,
			Data:        bundle[offset:end],
			IsLast:      end == len(bundle),
		}
		if err := stream.Send(chunk); err != nil {
			return "", fmt.Errorf("upload workspace bundle chunk: %w", err)
		}
	}
	response, err := stream.CloseAndRecv()
	if err != nil {
		return "", fmt.Errorf("finish workspace bundle upload: %w", err)
	}
	return response.GetBundleId(), nil
}

func repositoryResult(repo *pb.WorkspaceSyncRepositoryResult) RepositoryResult {
	return RepositoryResult{
		StorageID:          repo.GetStorageId(),
		DisplayPath:        repo.GetDisplayPath(),
		RepoURL:            repo.GetRepoUrl(),
		Branch:             repo.GetBranch(),
		TargetBranch:       repo.GetTargetBranch(),
		PushedCommit:       repo.GetPushedCommit(),
		Status:             workspaceStatusLabel(repo.GetStatus()),
		Message:            repo.GetMessage(),
		PullRequestURL:     repo.GetPullRequestUrl(),
		PullRequestCreated: repo.GetPullRequestCreated(),
	}
}

func workspaceStatusLabel(status pb.WorkspaceSyncRepositoryStatus) string {
	name := strings.TrimPrefix(status.String(), "WORKSPACE_SYNC_REPOSITORY_STATUS_")
	return strings.ToLower(name)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// Bundle JSON mirrors monofs/internal/workspacebundle (internal package).
type bundleJSON struct {
	WorkspaceID  string           `json:"workspace_id"`
	Repositories []repositoryJSON `json:"repositories"`
}

type repositoryJSON struct {
	StorageID   string          `json:"storage_id"`
	DisplayPath string          `json:"display_path"`
	RepoURL     string          `json:"repo_url"`
	Branch      string          `json:"branch"`
	BaseCommit  string          `json:"base_commit"`
	Operations  []operationJSON `json:"operations"`
}

type operationJSON struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Mode    int64  `json:"mode,omitempty"`
	Content []byte `json:"content,omitempty"`
}
