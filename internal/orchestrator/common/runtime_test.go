package common

import (
	"context"
	"encoding/json"
	"testing"

	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestLoadAllIntentStatesUsesPartitionRuntimeSnapshot(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	runtime := statedomain.NewPartitionRuntime("demo")
	runtime.Intents["api"] = &statedomain.IntentState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "IntentState",
		Partition:         "demo",
		Intent:            "api",
		Status:            statedomain.StatusHealthy,
		IntentVersionID:   "intent-v1",
		IntentSpecHash:    "hash-v1",
		PartitionRevision: "partition-rev-v1",
		TargetPusher:      "local",
		Outputs:           map[string]string{"url": "https://demo.example"},
	}
	seedJSON(t, ctx, store, paths.PartitionRuntime("demo"), runtime)

	states, err := LoadAllIntentStates(ctx, store, "demo")
	if err != nil {
		t.Fatalf("LoadAllIntentStates() error = %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("state count = %d, want 1", len(states))
	}
	if got := states["api"]; got == nil || got.Outputs["url"] != "https://demo.example" {
		t.Fatalf("loaded state = %+v", got)
	}
}

func seedJSON(t *testing.T, ctx context.Context, store *memory.Store, logicalPath string, value any) {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal(%s) error = %v", logicalPath, err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed"},
	}); err != nil {
		t.Fatalf("UpsertFiles(%s) error = %v", logicalPath, err)
	}
}
