package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/repoconfig"
	"github.com/radryc/guardian/internal/repoimport"
	"github.com/radryc/monofs/pkg/repowatch"
)

// RunRepoImportLoop starts one importer per shard. Each shard acquires a
// Kubernetes Lease when lease is enabled, so several guardiand replicas can
// share the work and survive a worker failure.
func (s *Server) RunRepoImportLoop(ctx context.Context, interval time.Duration, shards int, lease bool, leaseName string) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if shards <= 0 {
		shards = 1
	}
	if strings.TrimSpace(leaseName) == "" {
		leaseName = "guardian-repo-import"
	}
	log.Printf("guardian repo-import loop starting (interval=%s, shards=%d, lease=%t)", interval, shards, lease)
	for shard := 0; shard < shards; shard++ {
		shard := shard
		go s.runRepoImportShard(ctx, shard, shards, interval, lease, leaseName)
	}
}

func (s *Server) runRepoImportShard(ctx context.Context, shard, shards int, interval time.Duration, lease bool, leaseName string) {
	if lease {
		name := leaseName
		if shards > 1 {
			name = fmt.Sprintf("%s-%d", leaseName, shard)
		}
		cfg := repowatch.DefaultLeaseConfig(name)
		cfg.OnError = func(err error) {
			log.Printf("guardian repo-import shard %d lease acquisition failed: %v", shard, err)
		}
		if cfg.Enabled() {
			elector, err := repowatch.NewElector(cfg)
			if err == nil {
				log.Printf("guardian repo-import shard %d waiting for lease %s", shard, cfg.Name)
				_ = elector.Run(ctx, func(leaderCtx context.Context) {
					log.Printf("guardian repo-import shard %d acquired lease %s", shard, cfg.Name)
					s.repoImportLoop(leaderCtx, shard, shards, interval)
				}, nil)
				return
			}
			log.Printf("guardian repo-import shard %d lease unavailable: %v", shard, err)
		} else {
			log.Printf("guardian repo-import shard %d lease requested but Kubernetes API is not configured", shard)
		}
	}
	s.repoImportLoop(ctx, shard, shards, interval)
}

func (s *Server) repoImportLoop(ctx context.Context, shard, shards int, interval time.Duration) {
	s.syncRepoBundlesShard(ctx, shard, shards)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncRepoBundlesShard(ctx, shard, shards)
		}
	}
}

func (s *Server) syncRepoBundles(ctx context.Context) {
	s.syncRepoBundlesShard(ctx, 0, 1)
}

func (s *Server) syncRepoBundlesShard(ctx context.Context, shard, shards int) {
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
	seen := make(map[string]struct{})
	for _, repo := range repos {
		if ctx.Err() != nil {
			return
		}
		if shards > 1 && repowatch.ShardFor(repo.Name, shards) != shard {
			continue
		}
		root := repoStoreSourceRoot(repo)
		if root == "" {
			continue
		}
		seen[root] = struct{}{}
		s.importRepoBundles(ctx, root, repo.Name, repo.Source, repo.BindingList(), changed)
	}

	// Repo-authored partitions do not need a registry entry: discovering every
	// ingested repository that carries guardian/partitions makes
	// `monofs-admin ingest` (or a git push with MonoFS auto-refresh) the only
	// setup step.
	s.discoverRepoBundles(ctx, seen, changed, shard, shards)

	if len(changed) == 0 {
		return
	}
	partitions := make([]string, 0, len(changed))
	for partition := range changed {
		partitions = append(partitions, partition)
	}
	sort.Strings(partitions)
	s.repoImportMu.Lock()
	s.autoDeployPartitions(ctx, partitions)
	s.repoImportMu.Unlock()
}

// importRepoBundles imports partition bundles under root. bindings limits the
// import to registered partition names; an empty slice imports every directory
// under guardian/partitions (used for auto-discovered repositories).
func (s *Server) importRepoBundles(ctx context.Context, root, repoName, source string, bindings []registry.RepoBinding, changed map[string]struct{}) {
	sourceStore := repoimport.NewStoreSource(s.store, root)
	partitions := make([]string, 0)
	if len(bindings) > 0 {
		for _, binding := range bindings {
			partitions = append(partitions, binding.Partition)
		}
	} else {
		entries, err := sourceStore.ListDir(ctx, repoimport.PartitionsSubdir)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) && ctx.Err() == nil {
				log.Printf("guardian repo-import: %s: list %s: %v", repoName, repoimport.PartitionsSubdir, err)
			}
			return
		}
		for _, entry := range entries {
			if entry.IsDir {
				partitions = append(partitions, entry.Name)
			}
		}
	}

	for _, partition := range partitions {
		if ctx.Err() != nil {
			return
		}
		if !hasPartitionBundle(ctx, sourceStore, partition) {
			continue
		}
		result, err := repoimport.Import(ctx, sourceStore, s.store, repoimport.Options{
			RepoName:  repoName,
			Partition: partition,
			Principal: s.principalID,
			Source:    source,
			Prune:     true,
		})
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("guardian repo-import: %s/%s: %v", repoName, partition, err)
			}
			continue
		}
		if result.Changed {
			for _, warning := range result.Warnings {
				log.Printf("guardian repo-import: %s: %s", repoName, warning)
			}
			changed[partition] = struct{}{}
			log.Printf("guardian repo-import: %s imported %s (%d changed file(s))",
				repoName, partition, result.FilesWritten)
		}
	}
}

// discoveryHosts are MonoFS namespaces whose children are repositories rather
// than single repositories themselves.
var discoveryHosts = map[string]bool{
	"github.com":    true,
	"gitlab.com":    true,
	"bitbucket.org": true,
	"codeberg.org":  true,
}

func (s *Server) discoverRepoBundles(ctx context.Context, seen, changed map[string]struct{}, shard, shards int) {
	entries, err := s.store.ListDir(ctx, "/.repos")
	if err != nil {
		return
	}
	for _, top := range entries {
		if !top.IsDir {
			continue
		}
		topPath := path.Join("/.repos", top.Name)
		if !discoveryHosts[top.Name] {
			s.importDiscoveredRepo(ctx, topPath, seen, changed, shard, shards)
			continue
		}
		owners, err := s.store.ListDir(ctx, topPath)
		if err != nil {
			continue
		}
		for _, owner := range owners {
			if !owner.IsDir {
				continue
			}
			ownerPath := path.Join(topPath, owner.Name)
			repos, err := s.store.ListDir(ctx, ownerPath)
			if err != nil {
				continue
			}
			for _, repo := range repos {
				if repo.IsDir {
					s.importDiscoveredRepo(ctx, path.Join(ownerPath, repo.Name), seen, changed, shard, shards)
				}
			}
		}
	}
}

func (s *Server) importDiscoveredRepo(ctx context.Context, root string, seen, changed map[string]struct{}, shard, shards int) {
	if ctx.Err() != nil {
		return
	}
	if _, ok := seen[root]; ok {
		return
	}
	name := strings.TrimPrefix(root, "/.repos/")
	if shards > 1 && repowatch.ShardFor(name, shards) != shard {
		return
	}
	seen[root] = struct{}{}
	source := repoimport.NewStoreSource(s.store, root)
	if entries, err := source.ListDir(ctx, repoimport.PartitionsSubdir); err != nil || len(entries) == 0 {
		return
	}
	s.importRepoBundles(ctx, root, name, root, nil, changed)
}

func hasPartitionBundle(ctx context.Context, source repoimport.Source, partition string) bool {
	entries, err := source.ListDir(ctx, repoimport.PartitionsSubdir+"/"+partition)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir && (entry.Name == "partition.yaml" || entry.Name == "config.yaml") {
			return true
		}
	}
	return false
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
