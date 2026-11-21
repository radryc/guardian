package access

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/monofs/pkg/authz/policy"
)

func TestReadRoutingDirAndCompile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "diva-shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	defaults := `defaults:
  attributes:
    environment: prod
`
	partition := `name: diva-shared
attributes:
  role: tenant
shards:
  - name: aws
    signal: logs
    platform: aws
    account: "111111111111"
    region: us-east-1
    placements:
      - id: omni
        backend: omni
        source: logs.default
        log_groups: ["/strata/diva-shared"]
        partition_binding: {kind: log_group}
`
	if err := os.WriteFile(filepath.Join(dir, "defaults.yaml"), []byte(defaults), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "diva-shared", "partition.yaml"), []byte(partition), 0o600); err != nil {
		t.Fatal(err)
	}

	local := ReadRoutingDir(dir)
	if len(local.Problems) != 0 {
		t.Fatalf("problems = %v", local.Problems)
	}
	if len(local.Policies) != 1 {
		t.Fatalf("policies = %d", len(local.Policies))
	}
	if local.Policies[0].Attributes["environment"] != "prod" {
		t.Fatalf("defaults not applied: %#v", local.Policies[0].Attributes)
	}

	accessFile, err := policy.ParseFile([]byte(`
version: 1
defaults: {partitions: deny, sql: deny}
users:
  usera@example.com:
    partitions: [diva-shared]
    sql: [diva-shared]
`))
	if err != nil {
		t.Fatal(err)
	}
	compiled, issues, err := CompileWithRouting(accessFile, nil, nil, local.Policies, time.Now())
	if err != nil {
		t.Fatalf("compile: %v (%v)", err, issues)
	}
	if len(compiled.Partitions()) != 1 || compiled.Partitions()[0].Name != "diva-shared" {
		t.Fatalf("bundle partitions = %#v", compiled.Partitions())
	}
}

func TestLoadRoutingPoliciesFromStore(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	dir := "access/envs/dev/routing"
	writes := []struct {
		path string
		data string
	}{
		{"/" + dir + "/diva-shared/partition.yaml", `name: diva-shared
shards:
  - name: aws
    signal: logs
    platform: aws
    placements:
      - id: omni
        backend: omni
`},
		{"/" + dir + "/divb-shared/partition.yaml", `name: divb-shared
shards:
  - name: aws
    signal: logs
    platform: aws
    placements:
      - id: insights
        backend: cloudwatch_logs_insights
`},
	}
	for _, w := range writes {
		writeStoreFile(t, store, w.path, w.data)
	}
	policies, problems := LoadRoutingPolicies(ctx, store, dir)
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	if len(policies) != 2 {
		t.Fatalf("policies = %d", len(policies))
	}
	digest := RoutingPoliciesDigest(policies)
	if digest == "" {
		t.Fatal("empty digest")
	}
	if digest != RoutingPoliciesDigest([]policy.PartitionPolicy{policies[1], policies[0]}) {
		t.Fatal("digest is not order-independent")
	}
}
