package reconciler_test

import (
	"context"
	"testing"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/orchestrator/dispatcher"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/reconciler"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
)

// TestRunInvalidInterval verifies that Run() fails fast when the interval
// string is neither a valid cron expression nor a Go duration.
func TestRunInvalidInterval(t *testing.T) {
	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "test")
	r := reconciler.NewReconcilerWithOptions(store, disp, "not-a-valid-interval", 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := r.Run(ctx)
	if err == nil {
		t.Fatal("expected error from Run() with invalid interval, got nil")
	}
	if !containsStr(err.Error(), "invalid interval") && !containsStr(err.Error(), "neither") {
		t.Errorf("error %q does not mention invalid interval", err.Error())
	}
}

// TestRunSchedulesAndReconciles seeds a partition with a very short interval,
// starts Run() in the background, then verifies that the partition state file
// is written (i.e. a reconcile cycle executed) within a generous deadline.
func TestRunSchedulesAndReconciles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := memory.New()
	disp := dispatcher.NewDispatcher(store, "test")

	// Seed a minimal, valid partition.
	seedRaw(t, ctx, store, paths.PartitionConfig("sched-smoke"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: sched-smoke
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 200ms
`))
	seedRaw(t, ctx, store, paths.IntentManifest("sched-smoke", "only"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: only
spec:
  targetPusher: local
  joins: []
  target:
    cluster: local
    namespace: default
  assets:
    - type: Compute
      name: app
      properties:
        image: nginx:latest
`))

	runErr := make(chan error, 1)
	go func() {
		r := reconciler.NewReconcilerWithOptions(store, disp, "200ms", 0)
		runErr <- r.Run(ctx)
	}()

	// Poll until the partition state file exists (written after first reconcile).
	stateKey := paths.PartitionState("sched-smoke")
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		_, err := store.ReadFile(ctx, stateKey)
		if err == nil {
			// State file written — reconcile executed successfully.
			cancel() // signal Run() to stop
			// Run() should exit cleanly (nil or context-cancelled).
			select {
			case err := <-runErr:
				if err != nil && err != context.Canceled {
					t.Errorf("Run() returned unexpected error: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Error("Run() did not exit after context cancel")
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("partition state file was not written within deadline")
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
