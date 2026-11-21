package publish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/bundle"
	"github.com/radryc/guardian/internal/workspacepub"
)

// Publisher is the subset of internal/workspacepub the submitter needs.
type Publisher interface {
	Publish(ctx context.Context, req workspacepub.PublishRequest) (*workspacepub.PublishResult, error)
}

// Request describes one reviewed proposal.
type Request struct {
	Root         string
	PartitionDir string
	Partition    string

	IncludeSecrets bool
	Changed        bool
	BaseRef        string

	WorkspaceID string
	StorageID   string
	DisplayPath string
	RepoURL     string
	BaseBranch  string
	BaseCommit  string

	CommitMessage    string
	PullRequestTitle string
	PullRequestBody  string
	AuthorName       string
	AuthorEmail      string

	DryRun bool
}

// OperationSummary is a printable view of one operation.
type OperationSummary struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// Result is the machine-readable outcome of a submission.
type Result struct {
	Success            bool               `json:"success"`
	Partition          string             `json:"partition"`
	DryRun             bool               `json:"dryRun"`
	Operations         []OperationSummary `json:"operations"`
	WorkspaceID        string             `json:"workspaceId,omitempty"`
	StorageID          string             `json:"storageId,omitempty"`
	DisplayPath        string             `json:"displayPath,omitempty"`
	TargetBranch       string             `json:"targetBranch,omitempty"`
	PushedCommit       string             `json:"pushedCommit,omitempty"`
	PullRequestURL     string             `json:"pullRequestUrl,omitempty"`
	PullRequestCreated bool               `json:"pullRequestCreated"`
	JobID              string             `json:"jobId,omitempty"`
	JobState           string             `json:"jobState,omitempty"`
	Status             string             `json:"status,omitempty"`
	Message            string             `json:"message,omitempty"`
	SubmittedAt        time.Time          `json:"submittedAt"`
	Warning            string             `json:"warning,omitempty"`
}

// Submit validates the partition, builds repository operations, and publishes
// them through MonoFS as a review branch. It never writes Guardian state.
func Submit(ctx context.Context, req Request, publisher Publisher) (Result, error) {
	result := Result{
		Partition:   req.Partition,
		DryRun:      req.DryRun,
		WorkspaceID: req.WorkspaceID,
		StorageID:   req.StorageID,
		DisplayPath: req.DisplayPath,
		SubmittedAt: time.Now().UTC(),
	}
	if strings.TrimSpace(req.PartitionDir) == "" {
		return result, fmt.Errorf("partition directory is required")
	}
	if strings.TrimSpace(req.WorkspaceID) == "" {
		return result, fmt.Errorf("workspace id is required")
	}
	if strings.TrimSpace(req.RepoURL) == "" {
		return result, fmt.Errorf("repository url is required")
	}
	if strings.TrimSpace(req.StorageID) == "" {
		req.StorageID = StorageIDForURL(req.RepoURL)
		result.StorageID = req.StorageID
	}
	if strings.TrimSpace(req.DisplayPath) == "" {
		req.DisplayPath = DisplayPathFromURL(req.RepoURL)
		result.DisplayPath = req.DisplayPath
	}

	if _, err := bundle.Load(req.PartitionDir, bundle.Options{
		IncludeSecrets:    req.IncludeSecrets,
		ResolveIntentRefs: bundle.ResolveImageRefs,
	}); err != nil {
		return result, fmt.Errorf("partition does not validate: %w", err)
	}

	ops, err := BuildOperations(ctx, OperationOptions{
		Changed:        req.Changed,
		BaseRef:        req.BaseRef,
		PartitionDir:   req.PartitionDir,
		Root:           req.Root,
		IncludeSecrets: req.IncludeSecrets,
	})
	if err != nil {
		return result, err
	}
	result.Operations = summarizeOperations(ops)

	if req.DryRun {
		return result, nil
	}
	if len(ops) == 0 {
		return result, fmt.Errorf("no changes to submit under %s", req.PartitionDir)
	}
	if publisher == nil {
		return result, fmt.Errorf("workspace publisher is not configured")
	}

	message := strings.TrimSpace(req.CommitMessage)
	if message == "" {
		message = fmt.Sprintf("%s: submit %d change(s)", req.Partition, len(ops))
	}
	title := strings.TrimSpace(req.PullRequestTitle)
	if title == "" {
		title = message
	}

	published, err := publisher.Publish(ctx, workspacepub.PublishRequest{
		WorkspaceID:      req.WorkspaceID,
		BranchStrategy:   workspacepub.StrategyPerRepoBranch,
		CommitMessage:    message,
		AuthorName:       req.AuthorName,
		AuthorEmail:      req.AuthorEmail,
		PullRequestTitle: title,
		PullRequestBody:  req.PullRequestBody,
		Repository: workspacepub.Repository{
			StorageID:   req.StorageID,
			DisplayPath: req.DisplayPath,
			RepoURL:     req.RepoURL,
			Branch:      req.BaseBranch,
			BaseCommit:  req.BaseCommit,
			Operations:  ops,
		},
	})
	if err != nil {
		return result, err
	}

	result.Success = true
	result.JobID = published.JobID
	result.JobState = published.JobState
	result.TargetBranch = published.Repository.TargetBranch
	result.PushedCommit = published.Repository.PushedCommit
	result.PullRequestURL = published.Repository.PullRequestURL
	result.PullRequestCreated = published.Repository.PullRequestCreated
	result.Status = published.Repository.Status
	result.Message = published.Repository.Message

	proposalRoot := req.Root
	if root, err := GitRoot(ctx, req.Root); err == nil {
		proposalRoot = root
	}
	if err := RecordProposal(proposalRoot, Proposal{
		Partition:          req.Partition,
		WorkspaceID:        req.WorkspaceID,
		StorageID:          req.StorageID,
		DisplayPath:        req.DisplayPath,
		RepoURL:            req.RepoURL,
		TargetBranch:       result.TargetBranch,
		PushedCommit:       result.PushedCommit,
		PullRequestURL:     result.PullRequestURL,
		PullRequestCreated: result.PullRequestCreated,
		JobID:              result.JobID,
		JobState:           result.JobState,
		Status:             result.Status,
		Message:            result.Message,
		SubmittedAt:        result.SubmittedAt,
	}); err != nil {
		result.Warning = "submitted, but the local proposal record could not be written: " + err.Error()
	}
	return result, nil
}

func summarizeOperations(ops []workspacepub.Operation) []OperationSummary {
	out := make([]OperationSummary, 0, len(ops))
	for _, op := range ops {
		out = append(out, OperationSummary{Kind: op.Kind, Path: op.Path})
	}
	return out
}

// DisplayPathFromURL renders a repository URL as MonoFS's display path,
// host/owner/repo, without a trailing .git.
func DisplayPathFromURL(repoURL string) string {
	raw := strings.TrimSpace(repoURL)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(raw, "git@"), ":", 2)
		if len(parts) == 2 {
			return trimGitSuffix(parts[0] + "/" + parts[1])
		}
		return trimGitSuffix(raw)
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return trimGitSuffix(u.Host + "/" + strings.TrimPrefix(u.Path, "/"))
	}
	return trimGitSuffix(strings.TrimPrefix(raw, "/"))
}

func trimGitSuffix(value string) string {
	value = strings.Trim(value, "/")
	value = strings.TrimSuffix(value, ".git")
	return value
}

// StorageIDForURL derives a stable, branch-safe identifier for a repository.
// MonoFS uses StorageID as a label; the fetcher clones from RepoURL, so this
// does not need to match an ingested repository id.
func StorageIDForURL(repoURL string) string {
	display := DisplayPathFromURL(repoURL)
	base := path.Base(display)
	if base == "." || base == "/" || base == "" {
		base = "repo"
	}
	var safe strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			safe.WriteRune(r)
		default:
			safe.WriteByte('-')
		}
	}
	name := strings.Trim(safe.String(), "-")
	if name == "" {
		name = "repo"
	}
	sum := sha256.Sum256([]byte(display))
	return fmt.Sprintf("%s-%s", name, hex.EncodeToString(sum[:4]))
}

// GitAuthor returns the configured git identity for a directory.
func GitAuthor(ctx context.Context, dir string) (name, email string) {
	if out, err := exec.CommandContext(ctx, "git", "-C", dir, "config", "user.name").Output(); err == nil {
		name = strings.TrimSpace(string(out))
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", dir, "config", "user.email").Output(); err == nil {
		email = strings.TrimSpace(string(out))
	}
	return name, email
}
