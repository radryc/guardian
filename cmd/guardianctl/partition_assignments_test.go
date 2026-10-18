package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestPartitionAssignmentsCommand(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	_, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{
			{LogicalPath: paths.PartitionConfig("assign-demo"), Content: []byte("apiVersion: guardian/v1alpha1\nkind: Partition\nmetadata:\n  name: assign-demo\n")},
			{LogicalPath: paths.IntentManifest("assign-demo", "web"), Content: []byte(`apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: web
spec:
  targetPusher: aws-111111111111
  target:
    account: "111111111111"
    region: us-east-1
  assets:
    - type: Network
      name: main
      properties:
        cidr: 10.42.0.0/16
`)},
		},
		Context: guardianapi.MutationContext{PrincipalID: "test"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	reg := registry.New(store)
	if err := reg.PutPusher(ctx, &registry.Pusher{Name: "aws-111111111111", Account: "111111111111", Region: "us-east-1", ObservedAPIURL: "http://127.0.0.1:19090"}); err != nil {
		t.Fatalf("pusher: %v", err)
	}
	if err := reg.PutRepo(ctx, &registry.Repo{
		Name: "repo-demo", Pusher: "aws-111111111111", Source: "https://github.com/example/repo-demo.git", Branch: "main",
		Bindings: []registry.RepoBinding{{Partition: "assign-demo", Path: "/partitions/assign-demo/payloads/aws", Selector: "AssignDemo"}},
	}); err != nil {
		t.Fatalf("repo: %v", err)
	}
	var buf bytes.Buffer
	cmd := partitionAssignmentsCommand(store, &output.Printer{Writer: &buf})
	if err := cmd.Flags.Parse([]string{"--partition", "assign-demo"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	text := buf.String()
	for _, want := range []string{"repo-demo", "/partitions/assign-demo/payloads/aws", "aws-111111111111", "web"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}
