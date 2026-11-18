package awsdriver

import (
	"testing"

	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/pusher/secrets"
)

func TestRegisterRejectsMissingDependencies(t *testing.T) {
	if err := Register(nil, NewBackend(), secrets.NoopResolver{}); err == nil {
		t.Fatal("expected an error for a nil registry")
	}
	if err := Register(registry.New(), nil, secrets.NoopResolver{}); err == nil {
		t.Fatal("expected an error for a nil backend")
	}
}
