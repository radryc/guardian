package k8sread

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radryc/guardian/internal/k8sobserve"
)

type fakeLister struct {
	resources []k8sobserve.Resource
}

func (f fakeLister) ListManagedResources(context.Context) ([]k8sobserve.Resource, error) {
	return f.resources, nil
}

func TestHandlerServesStacks(t *testing.T) {
	t.Parallel()

	service := k8sobserve.NewService(fakeLister{resources: []k8sobserve.Resource{
		{Partition: "doctor", Intent: "controller", Namespace: "doctor", Kind: "Deployment", Name: "controller"},
	}}, "k8s-main")
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	handler := NewHandler(service, "k8s-main", "k8s-main", "secret")

	req := httptest.NewRequest(http.MethodGet, "/v1/k8s/observed/stacks", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Stacks []k8sobserve.Inventory `json:"stacks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(response.Stacks) != 1 || response.Stacks[0].Partition != "doctor" {
		t.Fatalf("stacks = %+v", response.Stacks)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/k8s/observed/stacks?partition=doctor&intent=controller", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered status = %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode filtered: %v", err)
	}
	if len(response.Stacks) != 1 {
		t.Fatalf("filtered stacks = %+v", response.Stacks)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/k8s/observed/stacks?partition=other", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("other status = %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode other: %v", err)
	}
	if len(response.Stacks) != 0 {
		t.Fatalf("other stacks = %+v", response.Stacks)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/k8s/observed/stacks/doctor/controller/resources", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resources status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerRejectsMissingToken(t *testing.T) {
	t.Parallel()

	handler := NewHandler(k8sobserve.NewService(fakeLister{}, "k8s-main"), "k8s-main", "k8s-main", "secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/k8s/observed/stacks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestHandlerFailsClosedWhenTokenFileUnreadable(t *testing.T) {
	t.Parallel()

	handler := NewHandler(k8sobserve.NewService(fakeLister{}, "k8s-main"), "k8s-main", "k8s-main", "").
		WithTokenFile("/nonexistent/guardian-token")
	req := httptest.NewRequest(http.MethodGet, "/v1/k8s/observed/stacks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
