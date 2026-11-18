package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/store/memory"
)

func TestM2MPusherDetailRequiresInternalToken(t *testing.T) {
	store := memory.New()
	srv, err := New(Options{
		Store:             store,
		PrincipalID:       "test-ui",
		AccessPolicyToken: "internal-token",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := registry.New(store).PutPusher(context.Background(), &registry.Pusher{
		Name:           "aws-test",
		Account:        "123456789012",
		Region:         "us-east-1",
		ObservedAPIURL: "http://pusher:19090",
		Token:          "pusher-token",
	}); err != nil {
		t.Fatalf("PutPusher() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/m2m/pushers/aws-test", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated status = %d, body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/m2m/pushers/aws-test", nil)
	req.Header.Set("X-Guardian-Internal-Token", "internal-token")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var pusher m2mPusher
	if err := json.Unmarshal(rec.Body.Bytes(), &pusher); err != nil {
		t.Fatalf("decode pusher: %v", err)
	}
	if pusher.Name != "aws-test" || pusher.Token != "" || !pusher.HasToken {
		t.Fatalf("pusher = %#v, want redacted token with HasToken=true", pusher)
	}

	if err := registry.New(store).PutPusher(context.Background(), &registry.Pusher{
		Name:           "aws-no-token",
		Account:        "123456789012",
		Region:         "us-east-1",
		ObservedAPIURL: "http://pusher2:19090",
	}); err != nil {
		t.Fatalf("PutPusher(no-token) error = %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/m2m/pushers/aws-no-token", nil)
	req.Header.Set("X-Guardian-Internal-Token", "internal-token")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var noToken m2mPusher
	if err := json.Unmarshal(rec.Body.Bytes(), &noToken); err != nil {
		t.Fatalf("decode pusher without token: %v", err)
	}
	if noToken.Token != "" || noToken.HasToken {
		t.Fatalf("pusher without token = %#v, want Token=\"\" and HasToken=false", noToken)
	}
}

func TestM2MPusherListPaginates(t *testing.T) {
	store := memory.New()
	srv, err := New(Options{
		Store:             store,
		PrincipalID:       "test-ui",
		AccessPolicyToken: "internal-token",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, name := range []string{"aws-a", "aws-b", "aws-c"} {
		if err := registry.New(store).PutPusher(context.Background(), &registry.Pusher{
			Name:           name,
			Account:        "123456789012",
			Region:         "us-east-1",
			ObservedAPIURL: "http://" + name + ":19090",
			Token:          name + "-token",
		}); err != nil {
			t.Fatalf("PutPusher(%s) error = %v", name, err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/m2m/pushers?limit=2", nil)
	req.Header.Set("X-Guardian-Internal-Token", "internal-token")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var page m2mPusherListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Pushers) != 2 || page.Total != 3 || !page.HasMore {
		t.Fatalf("page = %#v", page)
	}
	for _, p := range page.Pushers {
		if p.Token != "" || !p.HasToken {
			t.Fatalf("listed pusher %q leaked token or missing HasToken: %#v", p.Name, p)
		}
	}
}

func TestM2MOverviewRequiresInternalToken(t *testing.T) {
	srv, err := New(Options{
		Store:             memory.New(),
		PrincipalID:       "test-ui",
		AccessPolicyToken: "internal-token",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/m2m/overview", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated status = %d, body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/m2m/overview", nil)
	req.Header.Set("X-Guardian-Internal-Token", "internal-token")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, body=%s", rec.Code, rec.Body.String())
	}
}
