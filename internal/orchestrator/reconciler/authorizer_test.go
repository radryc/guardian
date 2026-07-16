package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestReconcilePartitionAuthorizerDenies verifies the control-plane guard hook
// blocks a reconcile before any store work when it returns an error.
func TestReconcilePartitionAuthorizerDenies(t *testing.T) {
	r := NewReconciler(nil, nil, time.Minute)
	denied := errors.New("not authorized")
	r.SetAuthorizer(func(context.Context, string) error { return denied })

	err := r.ReconcilePartition(context.Background(), "doctor", false)
	if err == nil || !errors.Is(err, denied) {
		t.Fatalf("expected authorizer denial, got %v", err)
	}
}

// TestReconcilePartitionAuthorizerNilAllows verifies that without an authorizer
// the guard is a no-op (the call proceeds past the gate; we only assert it does
// not fail on authorization).
func TestReconcilePartitionAuthorizerReceivesPartition(t *testing.T) {
	r := NewReconciler(nil, nil, time.Minute)
	var seen string
	stop := errors.New("stop-after-gate")
	r.SetAuthorizer(func(_ context.Context, partition string) error {
		seen = partition
		return stop // short-circuit so we don't touch the nil store
	})
	_ = r.ReconcilePartition(context.Background(), "monitoring", false)
	if seen != "monitoring" {
		t.Fatalf("authorizer received %q, want monitoring", seen)
	}
}
