package registry

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/store/memory"
)

func TestPusherCRUD(t *testing.T) {
	ctx := context.Background()
	store := New(memory.New())

	if _, err := store.GetPusher(ctx, "aws-1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("GetPusher() error = %v, want not exist", err)
	}
	pusher := &Pusher{
		Name:           "aws-1",
		Account:        "123456789012",
		Region:         "eu-west-1",
		ObservedAPIURL: "http://guardian-awsread:19090/",
	}
	if err := store.PutPusher(ctx, pusher); err != nil {
		t.Fatalf("PutPusher() error = %v", err)
	}
	if pusher.ObservedAPIURL != "http://guardian-awsread:19090" {
		t.Fatalf("URL not normalized: %q", pusher.ObservedAPIURL)
	}
	loaded, err := store.GetPusher(ctx, "aws-1")
	if err != nil {
		t.Fatalf("GetPusher() error = %v", err)
	}
	if loaded.Account != "123456789012" || loaded.CreatedAt.IsZero() {
		t.Fatalf("loaded = %+v", loaded)
	}
	list, err := store.ListPushers(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListPushers() = %v, %v", list, err)
	}
	if err := store.DeletePusher(ctx, "aws-1"); err != nil {
		t.Fatalf("DeletePusher() error = %v", err)
	}
	if _, err := store.GetPusher(ctx, "aws-1"); err == nil {
		t.Fatalf("GetPusher() after delete should fail")
	}
}

func TestPusherValidation(t *testing.T) {
	ctx := context.Background()
	store := New(memory.New())
	if err := store.PutPusher(ctx, &Pusher{Name: "Bad Name", ObservedAPIURL: "http://x"}); err == nil {
		t.Fatalf("expected invalid name error")
	}
	if err := store.PutPusher(ctx, &Pusher{Name: "aws-1", ObservedAPIURL: "ftp://x"}); err == nil {
		t.Fatalf("expected invalid URL error")
	}
	if err := store.PutPusher(ctx, &Pusher{Name: "aws-1", ObservedAPIURL: "http://x", Token: "a", TokenEnv: "B"}); err == nil {
		t.Fatalf("expected token/tokenEnv conflict error")
	}
}

func TestRepoBindings(t *testing.T) {
	ctx := context.Background()
	store := New(memory.New())
	if err := store.PutPusher(ctx, &Pusher{Name: "aws-1", ObservedAPIURL: "http://x"}); err != nil {
		t.Fatalf("PutPusher() error = %v", err)
	}

	legacy := &Repo{Name: "strata", Pusher: "aws-1", Partition: "legacy"}
	if err := store.PutRepo(ctx, legacy); err != nil {
		t.Fatalf("PutRepo(legacy) error = %v", err)
	}
	loaded, err := store.GetRepo(ctx, "strata")
	if err != nil {
		t.Fatalf("GetRepo() error = %v", err)
	}
	if bindings := loaded.BindingList(); len(bindings) != 1 || bindings[0].Partition != "legacy" {
		t.Fatalf("legacy bindings = %+v", bindings)
	}
	if loaded.ObservedPartitionPath() != "/partitions/legacy/payloads/aws" {
		t.Fatalf("ObservedPartitionPath() = %q", loaded.ObservedPartitionPath())
	}

	multi := &Repo{
		Name:   "strata",
		Pusher: "aws-1",
		Bindings: []RepoBinding{
			{Partition: "prod", Selector: "Prod-*", Path: "/partitions/prod/payloads/aws"},
			{Partition: "staging", Selector: "Stg-*"},
			{Partition: "prod", Selector: "duplicate"},
		},
	}
	if err := store.PutRepo(ctx, multi); err != nil {
		t.Fatalf("PutRepo(multi) error = %v", err)
	}
	loaded, err = store.GetRepo(ctx, "strata")
	if err != nil {
		t.Fatalf("GetRepo() error = %v", err)
	}
	if len(loaded.Bindings) != 2 {
		t.Fatalf("bindings = %+v", loaded.Bindings)
	}
	if loaded.Partition != "prod" {
		t.Fatalf("legacy partition = %q, want prod", loaded.Partition)
	}
	if loaded.Bindings[0].PayloadDir() != "/partitions/prod/payloads/aws" {
		t.Fatalf("payload dir = %q", loaded.Bindings[0].PayloadDir())
	}
	if loaded.Bindings[1].PayloadDir() != "/partitions/staging/payloads/aws" {
		t.Fatalf("payload dir = %q", loaded.Bindings[1].PayloadDir())
	}
	if names := loaded.PartitionNames(); len(names) != 2 || names[0] != "prod" || names[1] != "staging" {
		t.Fatalf("PartitionNames() = %v", names)
	}

	badSelector := &Repo{Name: "strata", Pusher: "aws-1", Bindings: []RepoBinding{{Partition: "prod", Selector: "[bad"}}}
	if err := store.PutRepo(ctx, badSelector); err == nil {
		t.Fatalf("expected invalid selector error")
	}
	// A repo can be registered before it has a pusher or partition bindings,
	// then bound later from Connections or `guardianctl repo assign`.
	unbound := &Repo{Name: "unbound", Source: "/.repos/github.com/example/unbound", SourceType: "monofs"}
	if err := store.PutRepo(ctx, unbound); err != nil {
		t.Fatalf("PutRepo(unbound) error = %v", err)
	}
	loaded, err = store.GetRepo(ctx, "unbound")
	if err != nil {
		t.Fatalf("GetRepo(unbound) error = %v", err)
	}
	if loaded.Pusher != "" || len(loaded.BindingList()) != 0 {
		t.Fatalf("unbound repo = %+v", loaded)
	}
	if badSource := (&Repo{Name: "bad-source", SourceType: "monofs", Source: "relative/path"}); badSource.Validate() == nil {
		t.Fatalf("expected invalid monofs source error")
	}
}

func TestRepoRequiresRegisteredPusher(t *testing.T) {
	ctx := context.Background()
	store := New(memory.New())
	repo := &Repo{Name: "strata", Pusher: "aws-1", Partition: "strata-aws", Source: "https://example.com/repo.git"}
	if err := store.PutRepo(ctx, repo); err == nil {
		t.Fatalf("expected unknown pusher error")
	}
	var unknown *UnknownPusherError
	if err := store.PutRepo(ctx, repo); !errors.As(err, &unknown) {
		t.Fatalf("error = %v, want UnknownPusherError", err)
	}
	if err := store.PutPusher(ctx, &Pusher{Name: "aws-1", ObservedAPIURL: "http://x"}); err != nil {
		t.Fatalf("PutPusher() error = %v", err)
	}
	if err := store.PutRepo(ctx, repo); err != nil {
		t.Fatalf("PutRepo() error = %v", err)
	}
	repo.AssemblySubdir = ""
	if err := store.PutRepo(ctx, repo); err != nil {
		t.Fatalf("PutRepo() update error = %v", err)
	}
	loaded, err := store.GetRepo(ctx, "strata")
	if err != nil {
		t.Fatalf("GetRepo() error = %v", err)
	}
	if loaded.AssemblySubdir != "cdk.out" {
		t.Fatalf("AssemblySubdir = %q, want cdk.out", loaded.AssemblySubdir)
	}
	if loaded.ObservedPartitionPath() != "/partitions/strata-aws/payloads/aws" {
		t.Fatalf("ObservedPartitionPath() = %q", loaded.ObservedPartitionPath())
	}
	if err := store.RecordSync(ctx, "strata", "abc123", nil); err != nil {
		t.Fatalf("RecordSync() error = %v", err)
	}
	loaded, _ = store.GetRepo(ctx, "strata")
	if loaded.LastSyncCommit != "abc123" || loaded.LastSyncedAt.IsZero() {
		t.Fatalf("sync not recorded: %+v", loaded)
	}
}
