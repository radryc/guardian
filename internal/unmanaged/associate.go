package unmanaged

import (
	"fmt"
	"strings"

	"github.com/radryc/guardian/internal/observedgen"
	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/workspacepub"
)

// AssociateOptions describes a repo association for one stack.
type AssociateOptions struct {
	// RepoName is the Guardian repo record name.
	RepoName string
	// RepoURL is the existing repository URL the user pasted.
	RepoURL string
	// BaseBranch is the branch the MR should target (defaults to the repo
	// default branch when empty).
	BaseBranch string
	// Partition is the target partition (created by the snapshot when new).
	Partition string
	// Path overrides the payload dir (default /partitions/<partition>/payloads/aws).
	Path string

	Pusher    string
	Account   string
	Region    string
	StackName string
	// Template is the live CloudFormation template (JSON or YAML). When empty
	// the caller must fetch it from the pusher first.
	Template string
	Interval string
}

// Association is the prepared repo association: a repo registration, the
// observed snapshot bundle, and the MR branch to publish on.
type Association struct {
	Repo       registry.Repo `json:"repo"`
	Partition  string        `json:"partition"`
	Branch     string        `json:"branch"`
	BaseBranch string        `json:"baseBranch,omitempty"`
	// Files are repo-relative paths (guardian/partitions/<name>/...) that are
	// published through MonoFS (branch + merge request).
	Files map[string][]byte `json:"files"`
	// Publish is the MonoFS publish outcome (review branch, PR URL, status).
	Publish *workspacepub.PublishResult `json:"publish,omitempty"`
	// PublishError records a failed MonoFS publish. The repo registration is
	// kept so the operator can retry.
	PublishError string `json:"publishError,omitempty"`
}

// BuildAssociation prepares the observed snapshot bundle and repo record for a
// stack. The caller persists the repo record and publishes Files to the
// repository through MonoFS.
func BuildAssociation(opts AssociateOptions) (*Association, error) {
	if strings.TrimSpace(opts.RepoURL) == "" {
		return nil, fmt.Errorf("repoUrl is required")
	}
	if strings.TrimSpace(opts.StackName) == "" {
		return nil, fmt.Errorf("stackName is required")
	}
	if strings.TrimSpace(opts.Pusher) == "" {
		return nil, fmt.Errorf("pusher is required")
	}
	partition := strings.TrimSpace(opts.Partition)
	if partition == "" {
		partition = "aws-" + observedgen.Slug(opts.StackName)
	}
	repoName := strings.TrimSpace(opts.RepoName)
	if repoName == "" {
		repoName = observedgen.Slug(partition)
	}

	snapshot, err := observedgen.Snapshot(observedgen.SnapshotOptions{
		Partition: partition,
		StackName: opts.StackName,
		Account:   opts.Account,
		Region:    opts.Region,
		Pusher:    opts.Pusher,
		Template:  opts.Template,
		Interval:  opts.Interval,
	})
	if err != nil {
		return nil, err
	}

	prefix := "guardian/partitions/" + partition + "/"
	files := make(map[string][]byte, len(snapshot))
	for rel, content := range snapshot {
		files[prefix+rel] = content
	}

	payloadDir := strings.TrimSpace(opts.Path)
	if payloadDir == "" {
		payloadDir = "/partitions/" + partition + "/payloads/aws"
	}
	repo := registry.Repo{
		APIVersion: "guardian/v1alpha1",
		Kind:       "Repo",
		Name:       repoName,
		Pusher:     opts.Pusher,
		Source:     strings.TrimSpace(opts.RepoURL),
		Branch:     strings.TrimSpace(opts.BaseBranch),
		Account:    strings.TrimSpace(opts.Account),
		Region:     strings.TrimSpace(opts.Region),
		Bindings: []registry.RepoBinding{{
			Partition: partition,
			Path:      payloadDir,
		}},
	}

	branch := "guardian/adopt-" + branchSlug(partition) + "-" + branchSlug(opts.StackName)
	return &Association{
		Repo:       repo,
		Partition:  partition,
		Branch:     branch,
		BaseBranch: strings.TrimSpace(opts.BaseBranch),
		Files:      files,
	}, nil
}

func branchSlug(value string) string {
	slug := strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	prevDash := false
	for _, r := range slug {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "stack"
	}
	if len(out) > 48 {
		out = strings.Trim(out[:48], "-")
	}
	return out
}
