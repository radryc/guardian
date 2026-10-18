package main

import (
	"context"
	"io"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
)

func TestRepoCLIRegistersSourceThenBindsLater(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	printer := &output.Printer{Writer: io.Discard}
	run := func(cmd *command.Command, args []string) error {
		if err := cmd.Flags.Parse(args); err != nil {
			t.Fatalf("parse: %v", err)
		}
		return cmd.Run(ctx, nil)
	}

	add := repoAddCommand(store, printer)
	if err := run(add, []string{"--name", "source-only", "--source", "/.repos/github.com/example/source", "--source-type", "monofs"}); err != nil {
		t.Fatalf("repo add: %v", err)
	}
	reg := registry.New(store)
	repo, err := reg.GetRepo(ctx, "source-only")
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	if repo.SourceType != "monofs" || repo.Pusher != "" || len(repo.BindingList()) != 0 {
		t.Fatalf("repo after add = %+v", repo)
	}

	if err := reg.PutPusher(ctx, &registry.Pusher{Name: "aws-1", Account: "111111111111", Region: "us-east-1", ObservedAPIURL: "http://x"}); err != nil {
		t.Fatalf("put pusher: %v", err)
	}
	assign := repoAssignCommand(store, printer)
	if err := run(assign, []string{"--name", "source-only", "--pusher", "aws-1", "--partition", "prod"}); err != nil {
		t.Fatalf("repo assign: %v", err)
	}
	repo, err = reg.GetRepo(ctx, "source-only")
	if err != nil {
		t.Fatalf("get bound repo: %v", err)
	}
	if repo.Pusher != "aws-1" || len(repo.PartitionNames()) != 1 || repo.PartitionNames()[0] != "prod" {
		t.Fatalf("repo after bind = %+v", repo)
	}
}
