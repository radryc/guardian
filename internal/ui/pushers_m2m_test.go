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
	var pusher registry.Pusher
	if err := json.Unmarshal(rec.Body.Bytes(), &pusher); err != nil {
		t.Fatalf("decode pusher: %v", err)
	}
	if pusher.Name != "aws-test" || pusher.Token != "pusher-token" {
		t.Fatalf("pusher = %#v", pusher)
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
	var page pusherListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Pushers) != 2 || page.Total != 3 || !page.HasMore {
		t.Fatalf("page = %#v", page)
	}
}
