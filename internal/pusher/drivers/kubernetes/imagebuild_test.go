package kubernetesdriver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestImageBuildObserveStateReturnsErrorOnBackendFailure(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	seedIntentStateK8s(t, ctx, store, "demo", "svc", map[string]string{"web.imageRef": "registry.example.com/app:1"})
	driver := &ImageBuildDriver{backend: &failingImageBuildBackend{err: errors.New("registry unavailable")}}

	_, _, err := driver.ObserveState(ctx, registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "svc",
		Asset:         taskdomain.AbstractAsset{Name: "web", Type: "ImageBuild"},
		Store:         store,
	})
	if err == nil {
		t.Fatal("expected backend error, got nil")
	}
}

func seedIntentStateK8s(t *testing.T, ctx context.Context, store *memory.Store, partition, intent string, outputs map[string]string) {
	t.Helper()
	state := statedomain.IntentState{APIVersion: "guardian/v1alpha1", Kind: "IntentState", Partition: partition, Intent: intent, Outputs: outputs}
	content := mustJSONK8s(t, state)
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{Writes: []guardianapi.PathWrite{{LogicalPath: paths.IntentState(partition, intent), Content: content}}, Context: guardianapi.MutationContext{PrincipalID: "test"}}); err != nil {
		t.Fatalf("seed intent state: %v", err)
	}
}

func mustJSONK8s(t *testing.T, value any) []byte {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return content
}

type failingImageBuildBackend struct{ err error }

func (b *failingImageBuildBackend) BuildAndPublish(context.Context, ImageBuildRequest) (ImageBuildResult, error) {
	return ImageBuildResult{}, nil
}
func (b *failingImageBuildBackend) LoadAndPush(context.Context, ImageLoadRequest) (ImageBuildResult, error) {
	return ImageBuildResult{}, nil
}
func (b *failingImageBuildBackend) StampImage(context.Context, string, string) error { return nil }
func (b *failingImageBuildBackend) ImageExists(context.Context, string) (bool, error) {
	return false, b.err
}
