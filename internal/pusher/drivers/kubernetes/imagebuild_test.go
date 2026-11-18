package kubernetesdriver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	statedomain "github.com/radryc/guardian/internal/domain/state"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
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
		Outputs:       map[string]string{"web.imageRef": "registry.example.com/app:1"},
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

type buildSourceStore struct {
	*memory.Store
	commit string
}

func (s *buildSourceStore) RepoCommit(_ context.Context, displayPath string) (string, error) {
	if displayPath != "github.com/radryc/memebank" {
		return "", errors.New("unexpected repo")
	}
	return s.commit, nil
}

type recordingImageBuildBackend struct {
	exists   bool
	buildReq ImageBuildRequest
	builds   int
	files    map[string]os.FileMode
}

func (b *recordingImageBuildBackend) BuildAndPublish(_ context.Context, req ImageBuildRequest) (ImageBuildResult, error) {
	b.buildReq = req
	b.builds++
	b.files = map[string]os.FileMode{}
	_ = filepath.WalkDir(req.WorkspaceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(req.WorkspaceDir, path)
		if relErr != nil {
			return relErr
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		b.files[filepath.ToSlash(rel)] = info.Mode()
		return nil
	})
	return ImageBuildResult{ImageRef: req.ImageRef}, nil
}
func (b *recordingImageBuildBackend) LoadAndPush(context.Context, ImageLoadRequest) (ImageBuildResult, error) {
	return ImageBuildResult{}, nil
}
func (b *recordingImageBuildBackend) StampImage(context.Context, string, string) error { return nil }
func (b *recordingImageBuildBackend) ImageExists(context.Context, string) (bool, error) {
	return b.exists, nil
}

func buildSourceInput(store *buildSourceStore) registry.AssetInput {
	return registry.AssetInput{
		PartitionName: "memebank",
		IntentName:    "images",
		Asset: taskdomain.AbstractAsset{
			Name: "memebank-build",
			Type: "ImageBuild",
			Properties: map[string]any{
				"buildSource": "/.repos/github.com/radryc/memebank",
				"dockerfile":  "Dockerfile",
				"registry":    "registry.strata.local:5000",
				"repository":  "memebank",
			},
		},
		Store: store,
	}
}

func TestImageBuildApplyFromMonoFSSource(t *testing.T) {
	ctx := context.Background()
	store := &buildSourceStore{Store: memory.New(), commit: "c1015eb54c7eb7ee007831e2589e896e5f431dd5"}
	writeBuildSourceFile(t, ctx, store, "/.repos/github.com/radryc/memebank/Dockerfile", "FROM alpine\n")
	writeBuildSourceFile(t, ctx, store, "/.repos/github.com/radryc/memebank/server/main.go", "package main\n")
	writeBuildSourceFile(t, ctx, store, "/.repos/github.com/radryc/memebank/scripts/release.sh", "#!/bin/sh\n")

	backend := &recordingImageBuildBackend{exists: false}
	driver := &ImageBuildDriver{backend: backend, defaultRegistry: "registry.strata.local:5000"}
	result, err := driver.Apply(ctx, buildSourceInput(store))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if backend.builds != 1 {
		t.Fatalf("builds = %d, want 1", backend.builds)
	}
	wantRef := "registry.strata.local:5000/memebank:c1015eb54c7e"
	if got := result.Outputs["imageRef"]; got != wantRef {
		t.Fatalf("imageRef = %q, want %q", got, wantRef)
	}
	if backend.buildReq.ImageRef != wantRef {
		t.Fatalf("build imageRef = %q, want %q", backend.buildReq.ImageRef, wantRef)
	}
	for _, rel := range []string{"Dockerfile", "server/main.go", "scripts/release.sh"} {
		if _, ok := backend.files[rel]; !ok {
			t.Fatalf("staged %s missing from %v", rel, backend.files)
		}
	}
	if mode := backend.files["scripts/release.sh"]; mode&0o111 == 0 {
		t.Fatalf("script mode = %v, want executable", mode)
	}
}

func TestImageBuildDiffTracksSourceCommit(t *testing.T) {
	ctx := context.Background()
	store := &buildSourceStore{Store: memory.New(), commit: "c1015eb54c7eb7ee007831e2589e896e5f431dd5"}
	writeBuildSourceFile(t, ctx, store, "/.repos/github.com/radryc/memebank/Dockerfile", "FROM alpine\n")
	backend := &recordingImageBuildBackend{exists: true}
	driver := &ImageBuildDriver{backend: backend, defaultRegistry: "registry.strata.local:5000"}

	in := buildSourceInput(store)
	in.Outputs = map[string]string{"memebank-build.imageRef": "registry.strata.local:5000/memebank:471e5e9"}
	drift, err := driver.Diff(ctx, in)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if drift.Status != "Changed" {
		t.Fatalf("drift = %+v, want Changed", drift)
	}

	in.Outputs["memebank-build.imageRef"] = "registry.strata.local:5000/memebank:c1015eb54c7e"
	drift, err = driver.Diff(ctx, in)
	if err != nil {
		t.Fatalf("Diff up to date: %v", err)
	}
	if drift.Status != "InSync" {
		t.Fatalf("drift = %+v, want InSync", drift)
	}
}

func writeBuildSourceFile(t *testing.T, ctx context.Context, store *buildSourceStore, logicalPath, content string) {
	t.Helper()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: []byte(content)}},
		Context: guardianapi.MutationContext{PrincipalID: "test"},
	}); err != nil {
		t.Fatalf("write %s: %v", logicalPath, err)
	}
}
