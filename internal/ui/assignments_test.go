package ui

import (
	"context"
	"testing"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/domain/intent"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
)

func TestBuildPartitionAssignments(t *testing.T) {
	store := memory.New()
	reg := registry.New(store)
	if err := reg.PutPusher(context.Background(), &registry.Pusher{
		Name: "aws-755370915520", Account: "755370915520", Region: "us-east-1",
		ObservedAPIURL: "http://127.0.0.1:19092",
	}); err != nil {
		t.Fatalf("PutPusher: %v", err)
	}
	if err := reg.PutRepo(context.Background(), &registry.Repo{
		Name: "strata-demo-observe", Pusher: "aws-755370915520",
		Source: "https://github.com/example/strata-demo-observe.git",
		Branch: "main", LastSyncCommit: "f87af8f", LastSyncedAt: time.Now(),
		Bindings: []registry.RepoBinding{{Partition: "demo-observe", Path: "/partitions/demo-observe/payloads/aws", Selector: "StrataDemoObserve"}},
	}); err != nil {
		t.Fatalf("PutRepo: %v", err)
	}
	srv := &Server{store: store}
	intents := []IntentDocument{{
		Name: "strata-demo-observe",
		Manifest: intent.Intent{
			Metadata: intent.Metadata{Name: "strata-demo-observe"},
			Spec:     intent.IntentSpec{TargetPusher: "aws-755370915520", Target: targetdomain.Placement{Account: "755370915520", Region: "us-east-1"}},
		},
	}}
	got := srv.buildPartitionAssignments(context.Background(), "demo-observe", intents)
	if len(got.Repos) != 1 {
		t.Fatalf("repos = %+v, want 1", got.Repos)
	}
	repo := got.Repos[0]
	if repo.PayloadPath != "/partitions/demo-observe/payloads/aws" {
		t.Errorf("payload path = %q", repo.PayloadPath)
	}
	if repo.MonoFSPath != "/partitions/demo-observe" {
		t.Errorf("monofs path = %q", repo.MonoFSPath)
	}
	if repo.PartitionPath != "guardian/partitions/demo-observe" {
		t.Errorf("partition path = %q", repo.PartitionPath)
	}
	if repo.SourcePath != "https://github.com/example/strata-demo-observe.git" {
		t.Errorf("source path = %q", repo.SourcePath)
	}
	if len(got.Pushers) != 1 || !got.Pushers[0].Registered {
		t.Fatalf("pushers = %+v, want one registered", got.Pushers)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", got.Warnings)
	}
}

func TestBuildPartitionAssignmentsFallsBackToNative(t *testing.T) {
	store := memory.New()
	if err := registry.New(store).PutPusher(context.Background(), &registry.Pusher{
		Name: "k8s-main", Region: "local", ObservedAPIURL: "http://127.0.0.1:19090",
	}); err != nil {
		t.Fatalf("PutPusher: %v", err)
	}
	srv := &Server{store: store}
	intents := []IntentDocument{{
		Name: "images",
		Manifest: intent.Intent{
			Metadata: intent.Metadata{Name: "images"},
			Spec: intent.IntentSpec{
				TargetPusher: "k8s-main",
				Assets: []intent.AssetSpec{{
					Type:       "ImageBuild",
					Name:       "doctor-build",
					Properties: map[string]any{"buildContext": "~/strata/doctor"},
				}},
			},
		},
	}}
	got := srv.buildPartitionAssignments(context.Background(), "orphan", intents)
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", got.Warnings)
	}
	if len(got.Repos) != 1 {
		t.Fatalf("repos = %+v, want native fallback", got.Repos)
	}
	repo := got.Repos[0]
	if repo.Name != registry.NativeRepoName || repo.SourceType != "native" {
		t.Fatalf("repo = %+v, want native", repo)
	}
	if repo.PartitionPath != "/partitions/orphan" || repo.SourcePath != "~/strata/doctor" {
		t.Fatalf("native paths = %q / %q", repo.PartitionPath, repo.SourcePath)
	}
	if len(got.Pushers) != 1 || got.Pushers[0].Name != "k8s-main" {
		t.Fatalf("pushers = %+v, want k8s-main target", got.Pushers)
	}
}
