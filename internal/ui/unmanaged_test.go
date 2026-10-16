package ui

import (
	"context"
	"errors"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/internal/unmanaged"
)

type fakeUnmanagedService struct {
	result      *unmanaged.Result
	association *unmanaged.Association
	lastOpts    unmanaged.AssociateOptions
	detectErr   error
	assocErr    error
}

func (f *fakeUnmanagedService) Detect(context.Context) (*unmanaged.Result, error) {
	if f.detectErr != nil {
		return nil, f.detectErr
	}
	return f.result, nil
}

func (f *fakeUnmanagedService) Associate(_ context.Context, opts unmanaged.AssociateOptions) (*unmanaged.Association, error) {
	f.lastOpts = opts
	if f.assocErr != nil {
		return nil, f.assocErr
	}
	return f.association, nil
}

func newUnmanagedTestServer(t *testing.T, service UnmanagedService) *Server {
	t.Helper()
	srv, err := New(Options{Store: memory.New(), PrincipalID: "test-ui", Unmanaged: service})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return srv
}

func TestHandleUnmanagedListsStacks(t *testing.T) {
	service := &fakeUnmanagedService{result: &unmanaged.Result{
		Stacks: []unmanaged.Stack{{
			State:     unmanaged.StateUnmanaged,
			Pusher:    "aws-123456789012",
			Region:    "eu-west-1",
			StackName: "ForeignStack",
		}},
	}}
	srv := newUnmanagedTestServer(t, service)
	rec := registryRequest(t, srv, "GET", "/api/aws/unmanaged", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"ForeignStack"`, `"unmanaged"`} {
		if !contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
}

func TestHandleUnmanagedWithoutService(t *testing.T) {
	srv := newUnmanagedTestServer(t, nil)
	rec := registryRequest(t, srv, "GET", "/api/aws/unmanaged", nil)
	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestHandleUnmanagedDetectError(t *testing.T) {
	service := &fakeUnmanagedService{detectErr: errors.New("boom")}
	srv := newUnmanagedTestServer(t, service)
	rec := registryRequest(t, srv, "GET", "/api/aws/unmanaged", nil)
	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandleUnmanagedAssociate(t *testing.T) {
	service := &fakeUnmanagedService{association: &unmanaged.Association{
		Partition: "aws-foreign-stack",
		Branch:    "guardian/adopt-aws-foreign-stack-foreignstack",
		Files:     map[string][]byte{"guardian/partitions/aws-foreign-stack/config.yaml": []byte("kind: Partition")},
	}}
	srv := newUnmanagedTestServer(t, service)
	rec := registryRequest(t, srv, "POST", "/api/aws/unmanaged/ForeignStack/associate", map[string]any{
		"repoUrl":   "https://github.com/example/ops.git",
		"pusher":    "aws-123456789012",
		"account":   "123456789012",
		"region":    "eu-west-1",
		"partition": "aws-foreign-stack",
	})
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if service.lastOpts.StackName != "ForeignStack" {
		t.Fatalf("stack name = %q", service.lastOpts.StackName)
	}
	if service.lastOpts.RepoURL != "https://github.com/example/ops.git" {
		t.Fatalf("repo url = %q", service.lastOpts.RepoURL)
	}
	if service.lastOpts.Pusher != "aws-123456789012" {
		t.Fatalf("pusher = %q", service.lastOpts.Pusher)
	}
}

func TestHandleUnmanagedAssociateValidation(t *testing.T) {
	srv := newUnmanagedTestServer(t, &fakeUnmanagedService{assocErr: errors.New("repoUrl is required")})
	rec := registryRequest(t, srv, "POST", "/api/aws/unmanaged/ForeignStack/associate", map[string]any{})
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400 (repoUrl required)", rec.Code)
	}
	rec = registryRequest(t, srv, "GET", "/api/aws/unmanaged/ForeignStack/associate", nil)
	if rec.Code != 405 {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	rec = registryRequest(t, srv, "POST", "/api/aws/unmanaged/", map[string]any{})
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
