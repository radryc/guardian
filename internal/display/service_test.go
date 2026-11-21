package display

import (
	"context"
	"testing"

	"github.com/radryc/guardian/internal/store/memory"
	shared "github.com/radryc/monofs/pkg/display"
)

func TestServiceLifecycle(t *testing.T) {
	ctx := context.Background()
	svc := NewService(memory.New())
	subject := "user:alice@example.com"

	created, err := svc.Create(ctx, subject, shared.Profile{
		ID:    "engineering",
		Scope: "personal",
		Keys:  map[string]string{"partition": "eng_service", "intent": "deployment"},
	}, false)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Owner != subject {
		t.Fatalf("Owner = %q, want %q", created.Owner, subject)
	}

	list, err := svc.List(ctx, subject, false)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != "engineering" {
		t.Fatalf("List() = %+v", list)
	}

	if err := svc.SetDefault(ctx, subject, "engineering"); err != nil {
		t.Fatalf("SetDefault() error = %v", err)
	}
	bundle, _, err := svc.Bundle(ctx)
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}
	if bundle.Defaults[subject] != "engineering" {
		t.Fatalf("bundle defaults = %+v", bundle.Defaults)
	}
	profile, _, err := svc.Resolve(ctx, subject, "")
	if err != nil || profile == nil || profile.ID != "engineering" {
		t.Fatalf("Resolve() = %+v, %v", profile, err)
	}

	if _, err := svc.Get(ctx, "user:bob@example.com", "engineering", false); err == nil {
		t.Fatal("expected another user's personal profile to be invisible")
	}
}
