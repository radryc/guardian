package ui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radryc/guardian/internal/access"
	"github.com/radryc/guardian/internal/observability"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/pusher/probes"
	"github.com/radryc/guardian/internal/registry"
	setstore "github.com/radryc/guardian/internal/sets"
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

func authzJSONRequest(ctx context.Context, method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
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

func TestAuthorizeMutationObserveModeFailsClosedByDefault(t *testing.T) {
	srv := newAuthzTestServer(t, false)
	if srv.authorizeMutation(authzRequest(identityFor("viewer@example.com"), http.MethodDelete, "/api/partitions/aws-x")) {
		t.Fatal("observe mode without an enforcing policy must deny mutations by default")
	}
	if !srv.authorizeMutation(authzRequest(identityFor("viewer@example.com"), http.MethodGet, "/api/partitions/aws-x")) {
		t.Fatal("GET must remain unaffected by the mutation guard")
	}

	optIn := newAuthzTestServer(t, false)
	optIn.allowUnpolicedMutations = true
	if !optIn.authorizeMutation(authzRequest(identityFor("viewer@example.com"), http.MethodDelete, "/api/partitions/aws-x")) {
		t.Fatal("explicit opt-in must preserve permissive observe-mode mutations")
	}
}

func TestAuthorizeMutationWithoutPolicyFailsClosed(t *testing.T) {
	srv := &Server{}
	if srv.authorizeMutation(authzRequest(context.Background(), http.MethodPost, "/api/pushers")) {
		t.Fatal("missing access policy must deny mutations by default")
	}
	if !srv.authorizeMutation(authzRequest(context.Background(), http.MethodGet, "/api/partitions")) {
		t.Fatal("missing access policy must not block reads")
	}

	srv.allowUnpolicedMutations = true
	if !srv.authorizeMutation(authzRequest(context.Background(), http.MethodPost, "/api/pushers")) {
		t.Fatal("explicit opt-in must allow unpoliced mutations")
	}
}

func TestAuthorizeMutationSetMembershipScopedToPartitions(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	cases := []struct {
		name  string
		email string
		body  string
		want  bool
	}{
		{"maintainer adds own partition", "maintainer@example.com", `{"add":["aws-x"]}`, true},
		{"maintainer adds foreign partition", "maintainer@example.com", `{"add":["k8s-z"]}`, false},
		{"maintainer removes own partition", "maintainer@example.com", `{"remove":["aws-x"]}`, true},
		{"maintainer removes foreign partition", "maintainer@example.com", `{"remove":["k8s-z"]}`, false},
		{"maintainer mixed add is denied", "maintainer@example.com", `{"add":["aws-x","k8s-z"]}`, false},
		{"viewer cannot add visible partition", "viewer@example.com", `{"add":["aws-x"]}`, false},
		{"admin adds any partition", "admin@example.com", `{"add":["k8s-z"]}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := authzJSONRequest(identityFor(tc.email), http.MethodPost, "/api/sets/core-sre/partitions", tc.body)
			if got := srv.authorizeMutation(req); got != tc.want {
				t.Fatalf("authorizeMutation() = %v, want %v", got, tc.want)
			}
			if tc.want {
				// The handler must still be able to decode the body.
				raw, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("read restored body: %v", err)
				}
				if string(raw) != tc.body {
					t.Fatalf("restored body = %q, want %q", raw, tc.body)
				}
			}
		})
	}
}

func TestAuthorizeMutationSetCreateRequiresPartitionMaintainer(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	if !srv.authorizeMutation(authzRequest(identityFor("maintainer@example.com"), http.MethodPost, "/api/sets")) {
		t.Fatal("maintainer should create sets")
	}
	if srv.authorizeMutation(authzRequest(identityFor("viewer@example.com"), http.MethodPost, "/api/sets")) {
		t.Fatal("viewer must not create sets")
	}
	if !srv.authorizeMutation(authzRequest(identityFor("admin@example.com"), http.MethodPost, "/api/sets")) {
		t.Fatal("admin should create sets")
	}
	if srv.authorizeMutation(authzRequest(identityFor("maintainer@example.com"), http.MethodPatch, "/api/sets/core-sre")) {
		t.Fatal("maintainer must not rename sets")
	}
}

func TestAuthorizeMutationSetCreateObserveModeFailsClosedByDefault(t *testing.T) {
	srv := newAuthzTestServer(t, false)
	if srv.authorizeMutation(authzRequest(identityFor("viewer@example.com"), http.MethodPost, "/api/sets")) {
		t.Fatal("observe mode without an enforcing policy must deny set creation by default")
	}
	srv.allowUnpolicedMutations = true
	if !srv.authorizeMutation(authzRequest(identityFor("viewer@example.com"), http.MethodPost, "/api/sets")) {
		t.Fatal("explicit opt-in must preserve permissive set creation")
	}
}

func TestAuthorizeMutationSetMembershipObserveModeFailsClosedByDefault(t *testing.T) {
	srv := newAuthzTestServer(t, false)
	req := authzJSONRequest(identityFor("viewer@example.com"), http.MethodPost, "/api/sets/core-sre/partitions", `{"add":["k8s-z"]}`)
	if srv.authorizeMutation(req) {
		t.Fatal("observe mode without an enforcing policy must deny set membership by default")
	}
	srv.allowUnpolicedMutations = true
	req = authzJSONRequest(identityFor("viewer@example.com"), http.MethodPost, "/api/sets/core-sre/partitions", `{"add":["k8s-z"]}`)
	if !srv.authorizeMutation(req) {
		t.Fatal("explicit opt-in must preserve permissive set membership")
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

func TestReadFiltersHideInvisiblePartitions(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	ctx := identityFor("viewer@example.com")

	probeResults := srv.filterProbes(ctx, []probes.Result{
		{ProbeID: "visible", Partition: "aws-x"},
		{ProbeID: "hidden", Partition: "k8s-z"},
	})
	if len(probeResults) != 1 || probeResults[0].Partition != "aws-x" {
		t.Fatalf("probes = %+v, want only aws-x", probeResults)
	}

	identities := srv.filterIdentities(ctx, []observability.IdentityRecord{
		{Service: "visible", Partition: "aws-x"},
		{Service: "hidden", Partition: "k8s-z"},
	})
	if len(identities) != 1 || identities[0].Partition != "aws-x" {
		t.Fatalf("identities = %+v, want only aws-x", identities)
	}
}

func TestFilterReposTrimsInvisibleBindings(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	ctx := identityFor("viewer@example.com")

	repos := []registry.Repo{
		{Name: "visible", Partition: "aws-x", Path: "/partitions/aws-x"},
		{Name: "hidden", Partition: "k8s-z", Path: "/partitions/k8s-z"},
		{Name: "multi", Bindings: []registry.RepoBinding{
			{Partition: "aws-x", Path: "/partitions/aws-x"},
			{Partition: "k8s-z", Path: "/partitions/k8s-z"},
		}},
		{Name: "unbound"},
	}
	got := srv.filterRepos(ctx, repos)
	if len(got) != 2 {
		t.Fatalf("repos = %+v, want visible and multi only", got)
	}
	if got[0].Name != "visible" || got[1].Name != "multi" {
		t.Fatalf("repos = %v, want [visible multi]", []string{got[0].Name, got[1].Name})
	}
	if len(got[1].Bindings) != 1 || got[1].Bindings[0].Partition != "aws-x" {
		t.Fatalf("multi bindings = %+v, want only aws-x", got[1].Bindings)
	}
	if got[1].Partition != "aws-x" {
		t.Fatalf("multi legacy partition = %q, want aws-x", got[1].Partition)
	}

	admin := srv.filterRepos(identityFor("admin@example.com"), repos)
	if len(admin) != 4 {
		t.Fatalf("admin repos = %d, want all 4", len(admin))
	}

	observe := newAuthzTestServer(t, false)
	got = observe.filterRepos(identityFor("viewer@example.com"), repos)
	if len(got) != 4 {
		t.Fatalf("observe repos = %d, want all 4", len(got))
	}
}

func TestFilterPushersHidesExclusivePushersAndRedactsTokens(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	pa := &pusherAccess{
		restricted: true,
		known:      map[string]bool{"aws-visible": true, "aws-hidden": true},
		visible:    map[string]bool{"aws-visible": true},
	}
	pushers := []registry.Pusher{
		{Name: "aws-visible", Token: "visible-secret"},
		{Name: "aws-hidden", Token: "hidden-secret"},
		{Name: "k8s-main", Token: "global-secret"},
	}
	got := srv.filterPushers(pushers, pa)
	if len(got) != 2 {
		t.Fatalf("pushers = %+v, want aws-visible and k8s-main", got)
	}
	for _, pusher := range got {
		if pusher.Token != "" {
			t.Fatalf("pusher %s token = %q, want redacted", pusher.Name, pusher.Token)
		}
	}
	if got[0].Name != "aws-visible" || got[1].Name != "k8s-main" {
		t.Fatalf("pushers = %v, want [aws-visible k8s-main]", []string{got[0].Name, got[1].Name})
	}

	unrestricted := srv.filterPushers(pushers, &pusherAccess{})
	if len(unrestricted) != 3 || unrestricted[0].Token != "visible-secret" {
		t.Fatalf("unrestricted pushers = %+v, want all with tokens", unrestricted)
	}
}

func TestVisibleSetIDsKeepsAncestorsOfVisiblePartitions(t *testing.T) {
	all := []setstore.Set{
		{ID: "division-a", Partitions: []string{"k8s-z"}},
		{ID: "division-b", Partitions: []string{"aws-x"}},
		{ID: "parent"},
		{ID: "parent-child", ParentID: "parent", Partitions: []string{"aws-x"}},
		{ID: "empty"},
	}
	keep := visibleSetIDs(all, map[string]bool{"aws-x": true})
	if keep["division-a"] || keep["empty"] {
		t.Fatalf("keep = %v, want hidden sets dropped", keep)
	}
	if !keep["division-b"] || !keep["parent"] || !keep["parent-child"] {
		t.Fatalf("keep = %v, want visible set and its ancestors kept", keep)
	}
}

func TestFilterSetsForAccessHidesEmptySets(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	ctx := identityFor("viewer@example.com")
	all := []setstore.Set{
		{ID: "hidden", Partitions: []string{"k8s-z"}},
		{ID: "visible", Partitions: []string{"aws-x"}},
		{ID: "empty"},
	}
	got := srv.filterSetsForAccess(ctx, all, map[string]bool{"aws-x": true})
	if len(got) != 1 || got[0].ID != "visible" {
		t.Fatalf("sets = %+v, want only visible", got)
	}

	observe := newAuthzTestServer(t, false)
	got = observe.filterSetsForAccess(identityFor("viewer@example.com"), all, map[string]bool{"aws-x": true})
	if len(got) != len(all) {
		t.Fatalf("observe mode sets = %d, want all %d", len(got), len(all))
	}
}

func TestHandleReposFiltersInvisibleRepositories(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	for _, repo := range []registry.Repo{
		{Name: "strata-div-a", Partition: "k8s-z"},
		{Name: "strata-div-b", Partition: "aws-x"},
	} {
		raw, err := json.Marshal(repo)
		if err != nil {
			t.Fatalf("marshal repo: %v", err)
		}
		writeAuthzFixture(t, srv.store.(*memory.Store), paths.RepoRecord(repo.Name), string(raw))
	}
	req := authzRequest(identityFor("viewer@example.com"), http.MethodGet, "/api/repos")
	rec := httptest.NewRecorder()
	srv.handleRepos(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp repoListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Repos) != 1 || resp.Repos[0].Name != "strata-div-b" {
		t.Fatalf("repos = %+v, want only strata-div-b", resp.Repos)
	}
}

func TestHandleSetsHidesInvisibleSets(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	store := srv.store.(*memory.Store)
	sets := setstore.New(store)
	if _, err := sets.Create(context.Background(), &setstore.Set{Name: "Hidden", Partitions: []string{"k8s-z"}}); err != nil {
		t.Fatalf("create hidden set: %v", err)
	}
	if _, err := sets.Create(context.Background(), &setstore.Set{Name: "Visible", Partitions: []string{"aws-x"}}); err != nil {
		t.Fatalf("create visible set: %v", err)
	}

	req := authzRequest(identityFor("viewer@example.com"), http.MethodGet, "/api/sets")
	rec := httptest.NewRecorder()
	srv.handleSets(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp SetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Sets) != 1 || resp.Sets[0].Name != "Visible" {
		t.Fatalf("sets = %+v, want only Visible", resp.Sets)
	}
}

func TestHandlePushersFiltersAndRedacts(t *testing.T) {
	srv := newAuthzTestServer(t, true)
	store := srv.store.(*memory.Store)
	for _, repo := range []registry.Repo{
		{Name: "repo-visible", Pusher: "aws-visible", Partition: "aws-x"},
		{Name: "repo-hidden", Pusher: "aws-hidden", Partition: "k8s-z"},
	} {
		raw, err := json.Marshal(repo)
		if err != nil {
			t.Fatalf("marshal repo: %v", err)
		}
		writeAuthzFixture(t, store, paths.RepoRecord(repo.Name), string(raw))
	}
	for _, pusher := range []registry.Pusher{
		{Name: "aws-visible", ObservedAPIURL: "http://visible", Token: "visible-secret"},
		{Name: "aws-hidden", ObservedAPIURL: "http://hidden", Token: "hidden-secret"},
		{Name: "k8s-main", ObservedAPIURL: "http://global", Token: "global-secret"},
	} {
		raw, err := json.Marshal(pusher)
		if err != nil {
			t.Fatalf("marshal pusher: %v", err)
		}
		writeAuthzFixture(t, store, paths.PusherRecord(pusher.Name), string(raw))
	}

	req := authzRequest(identityFor("viewer@example.com"), http.MethodGet, "/api/pushers")
	rec := httptest.NewRecorder()
	srv.handlePushers(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp pusherListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Pushers) != 2 {
		t.Fatalf("pushers = %+v, want aws-visible and k8s-main", resp.Pushers)
	}
	for _, pusher := range resp.Pushers {
		if pusher.Name == "aws-hidden" {
			t.Fatal("hidden pusher leaked to viewer")
		}
		if pusher.Token != "" {
			t.Fatalf("pusher %s token = %q, want redacted", pusher.Name, pusher.Token)
		}
	}
}
