package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/radryc/guardian/internal/registry"
)

// partitionRepoManaged reports whether a partition is fed by a repository
// binding. Repo-managed partitions must change through a reviewed PR, not the
// live control-plane UI.
func (s *Server) partitionRepoManaged(ctx context.Context, partition string) (bool, string, error) {
	if s == nil || s.store == nil {
		return false, "", nil
	}
	repos, err := registry.New(s.store).ListRepos(ctx)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "", nil
		}
		return false, "", err
	}
	for _, repo := range repos {
		for _, binding := range repo.Bindings {
			if binding.Partition == partition {
				return true, repo.Name, nil
			}
		}
		if repo.Partition == partition {
			return true, repo.Name, nil
		}
	}
	return false, "", nil
}

func allowRepoManagedWrites() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GUARDIAN_ALLOW_REPO_MANAGED_WRITES")), "true")
}

func repoManagedWriteError(partition, repo string) error {
	return fmt.Errorf("partition %q is managed by repository %q; submit the change through a PR/MR (Forge) instead of the live UI, or set GUARDIAN_ALLOW_REPO_MANAGED_WRITES=true for an emergency override", partition, repo)
}
