package ui

import (
	"context"
	"errors"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/internal/repoconfig"
	"github.com/rydzu/ainfra/guardian/internal/repoimport"
)

// RunRepoImportLoop periodically imports repo-authored partition bundles from
// MonoFS into the canonical partition namespace. It is level-triggered: the
// importer compares content and only writes changed files, so an unchanged
// repository does not wake the partition watcher or queue APPLY tasks.
func (s *Server) RunRepoImportLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	log.Printf("guardian repo-import loop started (interval=%s)", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncRepoBundles(ctx)
		}
	}
}

func (s *Server) syncRepoBundles(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	repos, err := registry.New(s.store).ListRepos(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("guardian repo-import: list repos: %v", err)
		}
		return
	}

	changed := make(map[string]struct{})
	for _, repo := range repos {
		if ctx.Err() != nil {
			return
		}
		root := repoStoreSourceRoot(repo)
		if root == "" {
			continue
		}
		source := repoimport.NewStoreSource(s.store, root)
		entries, err := source.ListDir(ctx, repoimport.PartitionsSubdir)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) && ctx.Err() == nil {
				log.Printf("guardian repo-import: %s: list %s: %v", repo.Name, repoimport.PartitionsSubdir, err)
			}
			continue
		}
		if len(entries) == 0 {
			continue
		}
		for _, binding := range repo.BindingList() {
			result, err := repoimport.Import(ctx, source, s.store, repoimport.Options{
				RepoName:  repo.Name,
				Partition: binding.Partition,
				Principal: s.principalID,
			})
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("guardian repo-import: %s/%s: %v", repo.Name, binding.Partition, err)
				}
				continue
			}
			if result.Changed {
				for _, warning := range result.Warnings {
					log.Printf("guardian repo-import: %s: %s", repo.Name, warning)
				}
				changed[binding.Partition] = struct{}{}
				log.Printf("guardian repo-import: %s imported %s (%d changed file(s))",
					repo.Name, binding.Partition, result.FilesWritten)
			}
		}
	}

	if len(changed) == 0 {
		return
	}
	partitions := make([]string, 0, len(changed))
	for partition := range changed {
		partitions = append(partitions, partition)
	}
	sort.Strings(partitions)
	s.autoDeployPartitions(ctx, partitions)
}

// repoStoreSourceRoot maps a registry repo source to the MonoFS logical root.
// URL sources are projected through MonoFS's /.repos/<display-path> mapping;
// local/native sources have no repo-authored bundle to import.
func repoStoreSourceRoot(repo registry.Repo) string {
	source := strings.TrimSpace(repo.Source)
	if strings.HasPrefix(source, "/.repos/") {
		return strings.TrimRight(source, "/")
	}
	if source == "" || strings.HasPrefix(source, "/") {
		return ""
	}
	display := repoconfig.DisplayPathFromSource(source)
	if display == "" {
		return ""
	}
	return "/.repos/" + display
}
