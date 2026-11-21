package gcpdriver

import (
	"context"
	"testing"

	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/pusher/secrets"
)

type nopBackend struct{}

func (nopBackend) CheckEnvironment(context.Context, StackRequest) error { return nil }
func (nopBackend) Plan(context.Context, StackRequest) (PlanOutcome, error) {
	return PlanOutcome{}, nil
}
func (nopBackend) Apply(context.Context, StackRequest) (map[string]string, error) {
	return nil, nil
}
func (nopBackend) Destroy(context.Context, StackRequest) error { return nil }
func (nopBackend) Get(context.Context, StackRequest) (StackState, error) {
	return StackState{}, nil
}

func TestRegisterRejectsMissingDependencies(t *testing.T) {
	if err := Register(nil, nopBackend{}, secrets.NoopResolver{}); err == nil {
		t.Fatal("expected an error for a nil registry")
	}
	if err := Register(registry.New(), nil, secrets.NoopResolver{}); err == nil {
		t.Fatal("expected an error for a nil backend")
	}
	if err := Register(registry.New(), nopBackend{}, secrets.NoopResolver{}); err != nil {
		t.Fatalf("Register with a complete backend: %v", err)
	}
}
