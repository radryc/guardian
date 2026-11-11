package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/radryc/guardian/internal/access"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
	coreauthz "github.com/radryc/monofs/pkg/authz"
)

const authzTestACL = `
version: 1
defaults:
  partitions: deny
users:
  admin@example.com:
    partitions: ["*"]
    role: admin
  maintainer@example.com:
    partitions: [aws-x]
    role: maintainer
  viewer@example.com:
    partitions: [aws-x]
`

func writeAuthzFixture(t *testing.T, store *memory.Store, logicalPath, content string) {
	t.Helper()
	if _, err := store.UpsertFiles(context.Background(), guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: []byte(content)}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "fixture"},
	}); err != nil {
		t.Fatalf("write %s: %v", logicalPath, err)
	}
}

func newAuthzTestServer(t *testing.T, enforce bool) *Server {
	t.Helper()
	store := memory.New()
	writeAuthzFixture(t, store, paths.PartitionConfig("aws-x"), `
metadata:
  name: aws-x
  labels:
    team: payments
spec:
  labels:
    team: payments
`)
	writeAuthzFixture(t, store, paths.PartitionConfig("k8s-z"), `
metadata:
  name: k8s-z
  labels:
    team: infra
spec:
  labels:
    team: infra
`)
	writeAuthzFixture(t, store, paths.AccessPolicyPath("dev"), authzTestACL)
	svc := access.NewService(access.Options{
		Store:    store,
		RawPath:  paths.AccessPolicyPath("dev"),
		Enforce:  enforce,
		Interval: time.Minute,
		Logf:     func(string, ...any) {},
	})
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh access policy: %v", err)
	}
	return &Server{access: svc, store: store}
}

func authzRequest(ctx context.Context, method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	return req.WithContext(ctx)
}

func identityFor(email string) context.Context {
	return coreauthz.ContextWithIdentity(context.Background(), coreauthz.Identity{Subject: "sub-" + email, Email: email})
}

func TestAuthorizeMutationPartitionRole(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	if srv.authorizeMutation(authzRequest(identityFor("maintainer@example.com"), http.MethodPut, "/api/partitions/aws-x/bundle")) != true {
		t.Fatal("maintainer should be allowed to modify aws-x")
	}
	if srv.authorizeMutation(authzRequest(identityFor("maintainer@example.com"), http.MethodPut, "/api/partitions/k8s-z/bundle")) != false {
		t.Fatal("maintainer must not modify k8s-z")
	}
	if srv.authorizeMutation(authzRequest(identityFor("viewer@example.com"), http.MethodDelete, "/api/partitions/aws-x")) != false {
		t.Fatal("viewer must not delete aws-x")
	}
	if srv.authorizeMutation(authzRequest(identityFor("admin@example.com"), http.MethodDelete, "/api/partitions/k8s-z")) != true {
		t.Fatal("admin should modify any partition")
	}
}

func TestAuthorizeMutationRegistryRequiresAdmin(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	if srv.authorizeMutation(authzRequest(identityFor("maintainer@example.com"), http.MethodPost, "/api/pushers")) {
		t.Fatal("maintainer must not register pushers")
	}
	if !srv.authorizeMutation(authzRequest(identityFor("admin@example.com"), http.MethodPost, "/api/pushers")) {
		t.Fatal("admin should register pushers")
	}
	if !srv.authorizeMutation(authzRequest(identityFor("viewer@example.com"), http.MethodGet, "/api/partitions/aws-x")) {
		t.Fatal("GET must not be blocked by the mutation guard")
	}
}

func TestAuthorizeMutationObserveModeIsPermissive(t *testing.T) {
	srv := newAuthzTestServer(t, false)
	if !srv.authorizeMutation(authzRequest(identityFor("viewer@example.com"), http.MethodDelete, "/api/partitions/aws-x")) {
		t.Fatal("observe mode must not enforce mutations")
	}
}

func TestM2MPartitionsReturnsAllForInternalToken(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	srv.accessPolicyToken = "internal-token"

	req := httptest.NewRequest(http.MethodGet, "/api/m2m/partitions", nil)
	req.Header.Set("X-Guardian-Internal-Token", "internal-token")
	rec := httptest.NewRecorder()
	srv.handleM2MPartitions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var partitions []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &partitions); err != nil {
		t.Fatalf("decode partitions: %v", err)
	}
	seen := map[string]bool{}
	for _, partition := range partitions {
		seen[partition.Name] = true
	}
	// The internal token is trusted for machine-to-machine reads, so the
	// end-user access policy must not filter the feed down to nothing.
	if !seen["aws-x"] || !seen["k8s-z"] {
		t.Fatalf("m2m partitions = %v, want aws-x and k8s-z", seen)
	}
}

func TestAuthorizeRepoNotifyUsesInternalToken(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	srv.accessPolicyToken = "internal-token"
	req := authzRequest(context.Background(), http.MethodPost, "/api/repos/example/notify")
	if srv.authorizeMutation(req) {
		t.Fatal("notify without the internal token must be rejected")
	}
	req = authzRequest(context.Background(), http.MethodPost, "/api/repos/example/notify")
	req.Header.Set("X-Guardian-Internal-Token", "internal-token")
	if !srv.authorizeMutation(req) {
		t.Fatal("notify with the internal token should be allowed")
	}
}
