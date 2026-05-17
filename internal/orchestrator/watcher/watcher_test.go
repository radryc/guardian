package watcher

import (
	"context"
	"testing"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestWatchDetectsNewPartitionConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := memory.New()
	w := &Watcher{}
	ch, err := w.Watch(ctx, store, nil, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: paths.PartitionConfig("new-partition"),
			Content: []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: new-partition
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`),
		}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed"},
	}); err != nil {
		t.Fatalf("UpsertFiles() error = %v", err)
	}

	select {
	case got := <-ch:
		if got != "new-partition" {
			t.Fatalf("partition = %q, want %q", got, "new-partition")
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for partition event")
	}
}

func TestWatchIgnoresStateEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := memory.New()
	w := &Watcher{}
	ch, err := w.Watch(ctx, store, nil, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: paths.EventState("demo", "evt1"),
			Content:     []byte(`{"eventID":"evt1"}`),
		}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "event"},
	}); err != nil {
		t.Fatalf("UpsertFiles() error = %v", err)
	}

	select {
	case got := <-ch:
		t.Fatalf("unexpected partition event %q", got)
	case <-time.After(150 * time.Millisecond):
	}
}
