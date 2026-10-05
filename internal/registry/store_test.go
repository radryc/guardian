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
