package state

import (
	"testing"
	"time"

	taskdomain "github.com/radryc/guardian/internal/domain/task"
)

func TestMergeIntentStatePreservesConcurrentObservations(t *testing.T) {
	t.Parallel()

	old := time.Now().UTC().Add(-10 * time.Minute)
	fresh := time.Now().UTC()

	// base is what the task writer loaded before the observed collector ran.
	base := &IntentState{
		Status:  StatusDiffing,
		Outputs: map[string]string{"a": "1"},
		Timestamps: StateTimestamps{
			LastDiffAt: old,
		},
	}
	// incoming is the task writer's computed state.
	incoming := CloneIntentState(base)
	incoming.Status = StatusHealthy
	incoming.Timestamps.LastDiffAt = fresh

	// dst is the latest persisted state, already refreshed by the collector.
	dst := CloneIntentState(base)
	dst.Status = StatusDiffing
	dst.Resources = []ResourceObservation{{LogicalID: "Cluster", AssetName: "res-cluster"}}
	dst.Timestamps.LastObservedAt = fresh

	MergeIntentState(dst, base, incoming)

	if dst.Status != StatusHealthy {
		t.Fatalf("status = %q, want %q", dst.Status, StatusHealthy)
	}
	if len(dst.Resources) != 1 || dst.Resources[0].AssetName != "res-cluster" {
		t.Fatalf("resources = %#v, want collector observation preserved", dst.Resources)
	}
	if !dst.Timestamps.LastObservedAt.Equal(fresh) {
		t.Fatalf("lastObservedAt = %s, want %s", dst.Timestamps.LastObservedAt, fresh)
	}
	if !dst.Timestamps.LastDiffAt.Equal(fresh) {
		t.Fatalf("lastDiffAt = %s, want %s", dst.Timestamps.LastDiffAt, fresh)
	}
}

func TestMergeIntentStateDoesNotMoveTimestampsBackwards(t *testing.T) {
	t.Parallel()

	newer := time.Now().UTC()
	older := newer.Add(-time.Hour)

	base := &IntentState{Timestamps: StateTimestamps{LastObservedAt: older}}
	incoming := &IntentState{Timestamps: StateTimestamps{LastObservedAt: older}}
	dst := &IntentState{Timestamps: StateTimestamps{LastObservedAt: newer}}

	MergeIntentState(dst, base, incoming)

	if !dst.Timestamps.LastObservedAt.Equal(newer) {
		t.Fatalf("lastObservedAt = %s, want %s", dst.Timestamps.LastObservedAt, newer)
	}
}

func TestMergeIntentStateAppliesObserverChanges(t *testing.T) {
	t.Parallel()

	old := time.Now().UTC().Add(-time.Minute)
	fresh := time.Now().UTC()

	base := &IntentState{
		Status:     StatusBlocked,
		Resources:  nil,
		Health:     nil,
		Timestamps: StateTimestamps{LastObservedAt: old},
		LastTaskID: "",
		LastError:  nil,
	}
	incoming := CloneIntentState(base)
	incoming.Status = StatusHealthy
	incoming.Resources = []ResourceObservation{{LogicalID: "Cluster", AssetName: "res-cluster"}}
	incoming.Health = &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "ready"}
	incoming.Timestamps.LastObservedAt = fresh

	dst := CloneIntentState(base)
	dst.Status = StatusBlocked

	MergeIntentState(dst, base, incoming)

	if dst.Status != StatusHealthy {
		t.Fatalf("status = %q, want %q", dst.Status, StatusHealthy)
	}
	if len(dst.Resources) != 1 {
		t.Fatalf("resources = %#v, want one observation", dst.Resources)
	}
	if dst.Health == nil || dst.Health.Summary != "ready" {
		t.Fatalf("health = %#v, want incoming health", dst.Health)
	}
	if !dst.Timestamps.LastObservedAt.Equal(fresh) {
		t.Fatalf("lastObservedAt = %s, want %s", dst.Timestamps.LastObservedAt, fresh)
	}
}

func TestMergeIntentStateNilBasePreservesConcurrentFields(t *testing.T) {
	t.Parallel()

	fresh := time.Now().UTC()
	dst := &IntentState{
		Resources:  []ResourceObservation{{LogicalID: "Cluster", AssetName: "res-cluster"}},
		Timestamps: StateTimestamps{LastObservedAt: fresh},
	}
	incoming := &IntentState{Partition: "demo", Intent: "app", Status: StatusReady}

	MergeIntentState(dst, nil, incoming)

	if dst.Status != StatusReady {
		t.Fatalf("status = %q, want %q", dst.Status, StatusReady)
	}
	if len(dst.Resources) != 1 || !dst.Timestamps.LastObservedAt.Equal(fresh) {
		t.Fatalf("concurrent observation was cleared: %#v", dst)
	}
}

func TestMergeIntentStateNilBaseCopiesIncoming(t *testing.T) {
	t.Parallel()

	incoming := &IntentState{
		Status:     StatusReady,
		Partition:  "demo",
		Intent:     "app",
		Outputs:    map[string]string{"x": "y"},
		Resources:  []ResourceObservation{{LogicalID: "Cluster", AssetName: "res-cluster"}},
		Timestamps: StateTimestamps{LastDiffAt: time.Now().UTC()},
	}
	dst := &IntentState{Partition: "demo", Intent: "app"}

	MergeIntentState(dst, nil, incoming)

	if dst.Status != StatusReady || len(dst.Resources) != 1 || dst.Outputs["x"] != "y" {
		t.Fatalf("dst = %#v, want incoming copied", dst)
	}
}

func TestMergeIntentStateMergesMapKeys(t *testing.T) {
	t.Parallel()

	base := &IntentState{
		Outputs:         map[string]string{"a": "1"},
		AssetVersions:   map[string]string{"a": "v1"},
		AssetVersionIDs: map[string]string{"a": "id1"},
	}
	dst := CloneIntentState(base)
	dst.Outputs["c"] = "3"
	dst.AssetVersions["c"] = "v3"
	dst.AssetVersionIDs["c"] = "id3"

	incoming := CloneIntentState(base)
	incoming.Outputs["b"] = "2"
	incoming.AssetVersions["b"] = "v2"
	incoming.AssetVersionIDs["b"] = "id2"

	MergeIntentState(dst, base, incoming)

	for key, want := range map[string]string{"a": "1", "b": "2", "c": "3"} {
		if got := dst.Outputs[key]; got != want {
			t.Errorf("outputs[%s] = %q, want %q", key, got, want)
		}
		if got := dst.AssetVersions[key]; got != "v"+want {
			t.Errorf("assetVersions[%s] = %q", key, got)
		}
		if got := dst.AssetVersionIDs[key]; got != "id"+want {
			t.Errorf("assetVersionIDs[%s] = %q", key, got)
		}
	}
}
