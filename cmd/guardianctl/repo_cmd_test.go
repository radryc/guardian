package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/store/memory"
)

func TestRepoSeedRegistersBindings(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	printer := &output.Printer{Writer: io.Discard}
	dir := t.TempDir()
	path := filepath.Join(dir, "repos.yaml")
	content := `apiVersion: guardian/v1alpha1
kind: RepoRegistry
repos:
  - name: doctor
    sourceType: git
    source: https://github.com/radryc/doctor.git
    bindings:
      - partition: doctor
        path: /partitions/doctor
  - name: lb
    sourceType: git
    source: https://github.com/radryc/lb.git
    bindings:
      - partition: lb-agent
        path: /partitions/lb-agent
      - partition: lb-edge
        path: /partitions/lb-edge
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := repoSeedCommand(store, printer)
	if err := cmd.Flags.Parse([]string{"--file", path}); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("repo seed: %v", err)
	}
	reg := registry.New(store)
	doctor, err := reg.GetRepo(ctx, "doctor")
	if err != nil {
		t.Fatalf("get doctor repo: %v", err)
	}
	if doctor.SourceType != "git" || len(doctor.PartitionNames()) != 1 || doctor.PartitionNames()[0] != "doctor" {
		t.Fatalf("doctor repo = %+v", doctor)
	}
	lb, err := reg.GetRepo(ctx, "lb")
	if err != nil {
		t.Fatalf("get lb repo: %v", err)
	}
	if names := lb.PartitionNames(); len(names) != 2 || names[0] != "lb-agent" || names[1] != "lb-edge" {
		t.Fatalf("lb bindings = %v", names)
	}
}

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

	native := repoAssignCommand(store, printer)
	if err := run(native, []string{"--name", "native", "--partition", "platform"}); err != nil {
		t.Fatalf("native bind: %v", err)
	}
	nativeRepo, err := reg.GetRepo(ctx, "native")
	if err != nil {
		t.Fatalf("get native repo: %v", err)
	}
	if nativeRepo.SourceType != "native" || len(nativeRepo.PartitionNames()) != 1 || nativeRepo.PartitionNames()[0] != "platform" {
		t.Fatalf("native repo = %+v", nativeRepo)
	}
}
