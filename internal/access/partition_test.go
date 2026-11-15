package access

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/store/memory"
	coreauthz "github.com/radryc/monofs/pkg/authz"
	"github.com/radryc/monofs/pkg/authz/policy"
)

const validPartitionAccess = `version: 1
users:
  radryc@example.com:
    partitions: [memebank]
    logs: [memebank]
    traces: [memebank]
    monofs:
      - github.com/*/memebank
    role: admin
groups:
  meme-team:
    partitions: [memebank]
    role: viewer
`

func TestValidatePartitionPolicyAcceptsScopedGrants(t *testing.T) {
	file, err := ValidatePartitionPolicy([]byte(validPartitionAccess), "memebank", "github.com/radryc/memebank")
	if err != nil {
		t.Fatalf("ValidatePartitionPolicy: %v", err)
	}
	if file == nil || len(file.Users) != 1 || len(file.Groups) != 1 {
		t.Fatalf("unexpected parsed file: %+v", file)
	}
}

func TestValidatePartitionPolicyRejectsOutOfScopeGrants(t *testing.T) {
	cases := map[string]string{
		"other partition": `version: 1
users:
  a@example.com:
    partitions: [other]
`,
		"wildcard partition": `version: 1
users:
  a@example.com:
    partitions: ["*"]
`,
		"other logs": `version: 1
users:
  a@example.com:
    partitions: [memebank]
    logs: [other]
`,
		"other traces": `version: 1
users:
  a@example.com:
    traces: [other]
`,
		"other repo": `version: 1
users:
  a@example.com:
    partitions: [memebank]
    monofs: [github.com/*/other]
`,
		"subtree monofs": `version: 1
users:
  a@example.com:
    partitions: [memebank]
    monofs: [github.com/radryc/memebank/**]
`,
		"wildcard monofs": `version: 1
users:
  a@example.com:
    partitions: [memebank]
    monofs: ["*"]
`,
		"imports": `version: 1
imports:
  - ../base.yaml
users:
  a@example.com:
    partitions: [memebank]
`,
		"defaults": `version: 1
defaults:
  partitions: allow
users:
  a@example.com:
    partitions: [memebank]
`,
		"selectors": `version: 1
selectors:
  all: { labels: { role: demo } }
users:
  a@example.com:
    partitions: [all]
`,
		"clients": `version: 1
clients:
  guardiand:
    partitions: ["*"]
    role: admin
`,
		"deny": `version: 1
users:
  a@example.com:
    partitions: [memebank]
    deny: [partitions]
`,
		"role without partition": `version: 1
users:
  a@example.com:
    logs: [memebank]
    role: admin
`,
		"empty subject": `version: 1
users:
  "  ":
    partitions: [memebank]
`,
	}
	for name, yaml := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidatePartitionPolicy([]byte(yaml), "memebank", "github.com/radryc/memebank"); err == nil {
				t.Fatalf("expected validation error for %s", name)
			}
		})
	}
}

func TestValidatePartitionPolicyRejectsMonoFSWithoutRepo(t *testing.T) {
	yaml := `version: 1
users:
  a@example.com:
    partitions: [memebank]
    monofs: [github.com/*/memebank]
`
	if _, err := ValidatePartitionPolicy([]byte(yaml), "memebank", ""); err == nil {
		t.Fatal("expected monofs rejection without a repository display path")
	}
}

func TestCompileWithPartitionsAddsScopedGrants(t *testing.T) {
	catalog := &policy.Catalog{Partitions: []policy.Partition{
		{Name: "memebank"},
		{Name: "other"},
	}}
	base, err := policy.ParseFile([]byte(`version: 1
defaults:
  partitions: deny
  logs: deny
  traces: deny
  monofs: deny
users:
  base@example.com:
    partitions: [other]
`))
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	part, err := ValidatePartitionPolicy([]byte(validPartitionAccess), "memebank", "github.com/radryc/memebank")
	if err != nil {
		t.Fatalf("parse partition access: %v", err)
	}

	compiled, _, err := CompileWithPartitions(base, catalog, []PartitionPolicy{{
		Partition:   "memebank",
		DisplayPath: "github.com/radryc/memebank",
		File:        part,
		Digest:      "sha256:test",
	}}, time.Now())
	if err != nil {
		t.Fatalf("CompileWithPartitions: %v", err)
	}

	id := coreauthz.Identity{Subject: "sub-radryc", Email: "radryc@example.com"}
	if !compiled.CanSeePartition(id, "memebank") {
		t.Fatal("repo-owned grant must see memebank")
	}
	if compiled.CanSeePartition(id, "other") {
		t.Fatal("repo-owned grant must not see other partitions")
	}
	if got := compiled.Role(id, "memebank"); got != coreauthz.RoleAdmin {
		t.Fatalf("role on memebank = %q, want admin", got)
	}
	if got := compiled.Role(id, "other"); got != "" {
		t.Fatalf("role on other = %q, want none", got)
	}
	if !compiled.CanReadPath(id, "github.com/radryc/memebank") {
		t.Fatal("repo-owned grant must read the owning repo")
	}
	if compiled.CanReadPath(id, "github.com/radryc/other") {
		t.Fatal("repo-owned grant must not read another repo")
	}
}

func TestLoadPartitionPolicies(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	for _, name := range []string{"memebank", "other"} {
		writeStoreFile(t, store, paths.PartitionConfig(name), "metadata:\n  name: "+name+"\n")
	}
	writeStoreFile(t, store, paths.AccessPartitionPolicy("memebank"), validPartitionAccess)
	writeStoreFile(t, store, paths.AccessPartitionMeta("memebank"), `{"partition":"memebank","displayPath":"github.com/radryc/memebank"}`)
	writeStoreFile(t, store, paths.AccessPartitionPolicy("other"), `version: 1
users:
  a@example.com:
    partitions: [memebank]
`)
	writeStoreFile(t, store, paths.AccessPartitionMeta("other"), `{"partition":"other","displayPath":"github.com/radryc/other"}`)

	catalog, err := LoadCatalog(ctx, store)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	policies, problems := LoadPartitionPolicies(ctx, store, catalog)
	if len(policies) != 1 || policies[0].Partition != "memebank" {
		t.Fatalf("policies = %+v, want memebank only", policies)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "outside partition") {
		t.Fatalf("problems = %v, want an out-of-scope error", problems)
	}
}

func TestLoadPartitionPoliciesMissingDirectory(t *testing.T) {
	store := memory.New()
	policies, problems := LoadPartitionPolicies(context.Background(), store, nil)
	if len(policies) != 0 || len(problems) != 0 {
		t.Fatalf("missing directory = %+v / %v, want empty", policies, problems)
	}
}

func TestServiceMergesImportedPartitionPolicy(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	writeStoreFile(t, store, paths.PartitionConfig("memebank"), "metadata:\n  name: memebank\n")
	writeStoreFile(t, store, paths.PartitionConfig("other"), "metadata:\n  name: other\n")
	writeStoreFile(t, store, paths.AccessPolicyPath("dev"), `version: 1
defaults:
  partitions: deny
  logs: deny
  traces: deny
  monofs: deny
`)
	writeStoreFile(t, store, paths.AccessPartitionPolicy("memebank"), validPartitionAccess)
	writeStoreFile(t, store, paths.AccessPartitionMeta("memebank"), `{"partition":"memebank","displayPath":"github.com/radryc/memebank"}`)

	svc := NewService(Options{
		Store:    store,
		RawPath:  paths.AccessPolicyPath("dev"),
		Enforce:  true,
		Interval: time.Minute,
		Logf:     func(string, ...any) {},
	})
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	idCtx := ctxFor("radryc@example.com")
	if !svc.CanSee(idCtx, "memebank") {
		t.Fatal("service must see memebank from the repo-owned grant")
	}
	if svc.CanSee(idCtx, "other") {
		t.Fatal("service must not see other")
	}
	if !svc.CanModify(idCtx, "memebank") {
		t.Fatal("repo-owned admin grant must allow modify")
	}
	if svc.CanModify(idCtx, "other") {
		t.Fatal("repo-owned grant must not allow modify on other")
	}

	before := svc.Version()
	writeStoreFile(t, store, paths.AccessPartitionPolicy("memebank"), strings.Replace(validPartitionAccess, "role: admin", "role: viewer", 1))
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("Refresh after change: %v", err)
	}
	if svc.Version() == before {
		t.Fatal("partition ACL change must change the compiled version")
	}
	if svc.CanModify(idCtx, "memebank") {
		t.Fatal("viewer grant must not allow modify")
	}
}

func TestServiceSkipsInvalidPartitionPolicy(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	writeStoreFile(t, store, paths.PartitionConfig("memebank"), "metadata:\n  name: memebank\n")
	writeStoreFile(t, store, paths.AccessPolicyPath("dev"), `version: 1
defaults:
  partitions: deny
users:
  base@example.com:
    partitions: [memebank]
`)
	writeStoreFile(t, store, paths.AccessPartitionPolicy("memebank"), `version: 1
users:
  a@example.com:
    partitions: [other]
`)
	writeStoreFile(t, store, paths.AccessPartitionMeta("memebank"), `{"partition":"memebank","displayPath":"github.com/radryc/memebank"}`)

	var logs []string
	svc := NewService(Options{
		Store:   store,
		RawPath: paths.AccessPolicyPath("dev"),
		Enforce: true,
		Logf: func(format string, args ...any) {
			logs = append(logs, format)
		},
	})
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !svc.CanSee(ctxFor("base@example.com"), "memebank") {
		t.Fatal("base policy must still load when a partition ACL is invalid")
	}
	if svc.CanSee(ctxFor("a@example.com"), "memebank") {
		t.Fatal("invalid partition ACL must not grant access")
	}
	if len(logs) == 0 {
		t.Fatal("expected a logged validation problem")
	}
}

func TestPartitionPolicyMetaRoundTrip(t *testing.T) {
	meta := PartitionPolicyMeta{Partition: "memebank", Repo: "memebank", Source: "https://github.com/radryc/memebank", DisplayPath: "github.com/radryc/memebank"}
	if meta.Partition != "memebank" || meta.DisplayPath == "" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}
