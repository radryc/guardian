package unmanaged

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/observed"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/internal/repoconfig"
	"github.com/rydzu/ainfra/guardian/internal/workspacepub"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

// Publisher publishes the association bundle through MonoFS (review branch +
// MR/PR). *workspacepub.Publisher satisfies it.
type Publisher interface {
	Publish(ctx context.Context, req workspacepub.PublishRequest) (*workspacepub.PublishResult, error)
}

// Service is the store-backed unmanaged-stacks implementation used by the UI
// and API: detection reads the pusher registry plus the observed read API, and
// association registers the repo record, publishes the snapshot through MonoFS
// and returns the review branch / PR URL.
type Service struct {
	Store     guardianapi.Store
	Registry  *registry.Store
	Resolver  observed.EndpointResolver
	Publisher Publisher
}

// Detect lists stacks that need a repository across every registered pusher.
func (s *Service) Detect(ctx context.Context) (*Result, error) {
	pushers, err := s.Registry.ListPushers(ctx)
	if err != nil {
		return nil, err
	}
	extraWarnings := make([]string, 0)
	list := make([]Pusher, 0, len(pushers))
	for _, pusher := range pushers {
		// Unmanaged-stack discovery only applies to providers that expose the
		// AWS observed read API. Kubernetes/Docker/local pushers are normal
		// Guardian targets and must not produce "observed API URL" warnings.
		if !supportsObservedAPI(pusher) {
			continue
		}
		endpoint, ok := s.resolve(pusher.Name)
		if !ok {
			extraWarnings = append(extraWarnings, fmt.Sprintf("pusher %s has no observed API URL", pusher.Name))
			continue
		}
		list = append(list, Pusher{
			Name:    pusher.Name,
			Account: pusher.Account,
			Region:  pusher.Region,
			Client:  endpoint.Client,
		})
	}
	result, err := Detect(ctx, s.Store, list)
	if err != nil {
		return nil, err
	}
	result.Warnings = append(extraWarnings, result.Warnings...)
	return result, nil
}

// supportsObservedAPI reports whether a pusher can serve the AWS observed read
// API. Legacy records may omit Provider, so fall back to the account/name shape
// used by AWS pushers.
func supportsObservedAPI(pusher registry.Pusher) bool {
	provider := strings.ToLower(strings.TrimSpace(pusher.Provider))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(pusher.Labels["kind"]))
	}
	switch provider {
	case "kubernetes", "k8s", "docker", "local":
		return false
	case "aws":
		return true
	}
	if strings.TrimSpace(pusher.ObservedAPIURL) != "" {
		return true
	}
	if strings.TrimSpace(pusher.Account) != "" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(pusher.Name)), "aws-")
}

// Associate registers the repo record, publishes the observed snapshot through
// MonoFS (review branch + MR/PR) and returns the association. Publish failures
// are recorded on the association instead of failing the registration.
func (s *Service) Associate(ctx context.Context, opts AssociateOptions) (*Association, error) {
	// The snapshot intent needs account and region; default them from the
	// pusher registration so callers only have to pass --stack/--repo.
	if strings.TrimSpace(opts.Account) == "" || strings.TrimSpace(opts.Region) == "" {
		if s.Registry != nil {
			if record, err := s.Registry.GetPusher(ctx, opts.Pusher); err == nil && record != nil {
				if strings.TrimSpace(opts.Account) == "" {
					opts.Account = record.Account
				}
				if strings.TrimSpace(opts.Region) == "" {
					opts.Region = record.Region
				}
			}
		}
	}
	if opts.Template == "" {
		if endpoint, ok := s.resolve(opts.Pusher); ok {
			template, err := endpoint.Client.Template(ctx, opts.Account, opts.Region, opts.StackName)
			if err != nil {
				return nil, fmt.Errorf("fetch live template: %w", err)
			}
			opts.Template = template
		} else {
			return nil, fmt.Errorf("pusher %s has no observed API URL; pass the live template explicitly", opts.Pusher)
		}
	}
	association, err := BuildAssociation(opts)
	if err != nil {
		return nil, err
	}
	if err := s.Registry.PutRepo(ctx, &association.Repo); err != nil {
		return nil, fmt.Errorf("register repo: %w", err)
	}
	s.publish(ctx, association, opts.StackName)
	return association, nil
}

// publish pushes the association bundle through MonoFS. It is best-effort: the
// repo registration stays valid when the publish fails, and the error is
// surfaced on the association for the UI to retry.
func (s *Service) publish(ctx context.Context, association *Association, stackName string) {
	if s.Publisher == nil {
		return
	}
	displayPath := repoconfig.DisplayPathFromSource(association.Repo.Source)
	if displayPath == "" {
		association.PublishError = fmt.Sprintf("cannot derive a MonoFS display path from %q", association.Repo.Source)
		return
	}
	paths := make([]string, 0, len(association.Files))
	for path := range association.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	operations := make([]workspacepub.Operation, 0, len(paths))
	for _, path := range paths {
		operations = append(operations, workspacepub.Operation{
			Kind:    workspacepub.OperationUpsert,
			Path:    path,
			Content: association.Files[path],
		})
	}

	commitMessage := fmt.Sprintf("Guardian adoption: %s (%s)", stackName, association.Partition)
	result, err := s.Publisher.Publish(ctx, workspacepub.PublishRequest{
		WorkspaceID:    association.Branch,
		BranchStrategy: workspacepub.StrategyPerRepoBranch,
		CommitMessage:  commitMessage,
		AuthorName:     "Guardian",
		AuthorEmail:    "guardian@local",
		Repository: workspacepub.Repository{
			StorageID:   association.Repo.Name,
			DisplayPath: displayPath,
			RepoURL:     association.Repo.Source,
			Branch:      association.BaseBranch,
			Operations:  operations,
		},
	})
	if err != nil {
		association.PublishError = err.Error()
		return
	}
	association.Publish = result
	if result.Repository.TargetBranch != "" {
		association.Branch = result.Repository.TargetBranch
	}
}

func (s *Service) resolve(pusher string) (observed.Endpoint, bool) {
	if s.Resolver == nil || pusher == "" {
		return observed.Endpoint{}, false
	}
	endpoint, ok := s.Resolver.Resolve(pusher)
	if !ok || endpoint.Client == nil {
		return observed.Endpoint{}, false
	}
	return endpoint, true
}
