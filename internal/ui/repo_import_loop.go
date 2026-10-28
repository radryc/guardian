package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/radryc/monofs/pkg/repowatch"
	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/repoconfig"
	"github.com/radryc/guardian/internal/repoimport"
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
			if !hasPartitionBundle(ctx, source, binding.Partition) {
				continue
			}
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
				log.Printf("guardian repo-import: shard %d/%d %s imported %s (%d changed file(s))",
					shard, shards, repo.Name, binding.Partition, result.FilesWritten)
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
	s.repoImportMu.Lock()
	s.autoDeployPartitions(ctx, partitions)
	s.repoImportMu.Unlock()
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
