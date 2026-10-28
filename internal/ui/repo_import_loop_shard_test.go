package ui

import (
	"context"
	"testing"

	"github.com/radryc/monofs/pkg/repowatch"
	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/store/memory"
)

func TestSyncRepoBundlesShardFiltersRepos(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	registryStore := registry.New(store)
	if err := registryStore.PutPusher(ctx, &registry.Pusher{
		APIVersion: "guardian/v1alpha1",
		Kind:       "Pusher",
		Name:       "k8s-main",
		Provider:   "kubernetes",
	}); err != nil {
		t.Fatalf("put pusher: %v", err)
	}

	repos := []string{"demo-a", "demo-b"}
	for _, name := range repos {
		if err := registryStore.PutRepo(ctx, &registry.Repo{
			APIVersion: "guardian/v1alpha1",
			Kind:       "Repo",
			Name:       name,
			Source:     "https://github.com/acme/" + name + ".git",
			Pusher:     "k8s-main",
			Bindings:   []registry.RepoBinding{{Partition: name}},
		}); err != nil {
			t.Fatalf("put repo %s: %v", name, err)
		}
		root := "/.repos/github.com/acme/" + name + "/guardian/partitions/" + name
		writeTestFiles(t, ctx, store, map[string]string{
			root + "/config.yaml": `apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: ` + name + `
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: manual
`,
		})
	}

	shard := 0
	shards := 2
	srv := &Server{store: store, principalID: "test-ui"}
	srv.syncRepoBundlesShard(ctx, shard, shards)

	for _, name := range repos {
		_, err := store.ReadFile(ctx, "/partitions/"+name+"/config.yaml")
		want := repowatch.ShardFor(name, shards) == shard
		if want && err != nil {
			t.Fatalf("expected shard %d to import %s: %v", shard, name, err)
		}
		if !want && err == nil {
			t.Fatalf("shard %d imported %s from another shard", shard, name)
		}
	}

	other := 1 - shard
	srv.syncRepoBundlesShard(ctx, other, shards)
	for _, name := range repos {
		if _, err := store.ReadFile(ctx, "/partitions/"+name+"/config.yaml"); err != nil {
			t.Fatalf("repo %s was not imported by the union of both shards: %v", name, err)
		}
	}
}
