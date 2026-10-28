package access

import (
	"context"
	"testing"
	"time"

	coreauthz "github.com/radryc/monofs/pkg/authz"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
)

const testPartitionAWS = `
metadata:
  name: aws-x
  labels:
    team: payments
    aws.account: "123456789012"
    aws.region: us-east-1
spec:
  labels:
    team: payments
`

const testPartitionK8s = `
metadata:
  name: k8s-z
  labels:
    team: infra
`

const testRawPolicy = `
version: 1
defaults:
  partitions: deny
  logs: deny
  traces: deny
  monofs: deny
selectors:
  payments: { labels: { team: payments } }
users:
  foo@example.com:
    partitions: [aws-x]
    logs: [payments]
  admin@example.com:
    partitions: ["*"]
    role: admin
groups:
  sre:
    partitions: ["*"]
    role: maintainer
`

func writeStoreFile(t *testing.T, store *memory.Store, logicalPath, content string) {
	t.Helper()
	if _, err := store.UpsertFiles(context.Background(), guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: []byte(content)}},
		Context: guardianapi.MutationContext{
			PrincipalID: "test",
			Reason:      "fixture",
		},
	}); err != nil {
		t.Fatalf("write %s: %v", logicalPath, err)
	}
}

// newTestService builds a service over an in-memory store with two partitions.
// The raw policy path is always configured; withPolicy controls whether the
// file exists, so withPolicy=false exercises "enabled but never loaded".
func newTestService(t *testing.T, enforce, withPolicy bool) *Service {
	t.Helper()
	store := memory.New()
	writeStoreFile(t, store, paths.PartitionConfig("aws-x"), testPartitionAWS)
	writeStoreFile(t, store, paths.PartitionConfig("k8s-z"), testPartitionK8s)
	rawPath := paths.AccessPolicyPath("dev")
	if withPolicy {
		writeStoreFile(t, store, rawPath, testRawPolicy)
	}
	svc := NewService(Options{
		Store:    store,
		RawPath:  rawPath,
		Enforce:  enforce,
		Interval: time.Minute,
		Logf:     func(string, ...any) {},
	})
	if withPolicy {
		if err := svc.Refresh(context.Background()); err != nil {
			t.Fatalf("refresh: %v", err)
		}
		if !svc.Ready() {
			t.Fatal("service should be ready after refresh")
		}
	} else if err := svc.Refresh(context.Background()); err == nil {
		t.Fatal("refresh without a published policy should fail")
	}
	return svc
}

func ctxFor(email string, groups ...string) context.Context {
	return coreauthz.ContextWithIdentity(context.Background(), coreauthz.Identity{
		Subject: "sub-" + email,
		Email:   email,
		Groups:  groups,
	})
}

func TestCanSeePartitions(t *testing.T) {
	svc := newTestService(t, true, true)

	if !svc.CanSee(ctxFor("foo@example.com"), "aws-x") {
		t.Fatal("foo should see aws-x")
	}
	if svc.CanSee(ctxFor("foo@example.com"), "k8s-z") {
		t.Fatal("foo must not see k8s-z")
	}
	if !svc.CanSee(ctxFor("admin@example.com"), "k8s-z") {
		t.Fatal("admin should see k8s-z")
	}
	if !svc.CanSee(ctxFor("sre@example.com", "sre"), "aws-x") {
		t.Fatal("sre group should see aws-x")
	}
	if svc.CanSee(coreauthz.ContextWithIdentity(context.Background(), coreauthz.Identity{}), "aws-x") {
		t.Fatal("anonymous must not see partitions under enforcement")
	}
}

func TestObserveModeAllows(t *testing.T) {
	svc := newTestService(t, false, true)
	if !svc.CanSee(ctxFor("foo@example.com"), "k8s-z") {
		t.Fatal("observe mode must not filter")
	}
	if !svc.CanModify(ctxFor("foo@example.com"), "k8s-z") {
		t.Fatal("observe mode must not gate modify")
	}
}

func TestFailClosedWhenNotReady(t *testing.T) {
	svc := newTestService(t, true, false)
	if svc.Ready() {
		t.Fatal("service must not be ready without a policy")
	}
	if svc.CanSee(ctxFor("foo@example.com"), "aws-x") {
		t.Fatal("enforce without a policy must fail closed")
	}
	if svc.CanModify(ctxFor("foo@example.com"), "aws-x") {
		t.Fatal("enforce without a policy must deny modify")
	}
}

func TestDisabledIsPermissive(t *testing.T) {
	svc := NewService(Options{
		Store:    memory.New(),
		RawPath:  "",
		Enforce:  true,
		Interval: time.Minute,
		Logf:     func(string, ...any) {},
	})
	if svc.Enabled() {
		t.Fatal("service without RawPath must be disabled")
	}
	if !svc.CanSee(ctxFor("foo@example.com"), "anything") {
		t.Fatal("disabled service must allow everything")
	}
	if !svc.CanSee(coreauthz.ContextWithIdentity(context.Background(), coreauthz.Identity{}), "anything") {
		t.Fatal("disabled service must allow anonymous reads")
	}
}

func TestCanModifyRoles(t *testing.T) {
	svc := newTestService(t, true, true)
	if svc.CanModify(ctxFor("foo@example.com"), "aws-x") {
		t.Fatal("foo has no role and must not modify")
	}
	if !svc.CanModify(ctxFor("sre@example.com", "sre"), "aws-x") {
		t.Fatal("sre maintainer must modify")
	}
	if !svc.CanModify(ctxFor("admin@example.com"), "k8s-z") {
		t.Fatal("admin must modify")
	}
}

func TestExplainAndPolicyJSON(t *testing.T) {
	svc := newTestService(t, true, true)
	explanation := svc.Explain(context.Background(), "foo@example.com", nil)
	if explanation.Admin {
		t.Fatal("foo is not admin")
	}
	if len(explanation.Matches) != 1 {
		t.Fatalf("expected 1 matching grant, got %d", len(explanation.Matches))
	}
	if got := explanation.Matches[0].Partitions; len(got) != 1 || got[0] != "aws-x" {
		t.Fatalf("unexpected partition grants: %v", got)
	}
	if !containsString(explanation.Matches[0].Logs, "aws-x") {
		t.Fatalf("selector should resolve logs to aws-x, got %v", explanation.Matches[0].Logs)
	}

	data, err := svc.PolicyJSON()
	if err != nil || len(data) == 0 {
		t.Fatalf("PolicyJSON: %v", err)
	}
	version := svc.Version()
	if version == "" {
		t.Fatal("expected a policy version")
	}
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
