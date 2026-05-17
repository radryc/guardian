package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cliformat "github.com/rydzu/ainfra/guardian/internal/cli/format"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	manifestpkg "github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

func TestPartitionTagsListDistinctTagsForPartition(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	seedFile(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`))
	seedJSON(t, ctx, store, paths.ArchiveState("demo", "api", "dep_demo"), historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_demo",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-demo-config-v1"},
		AssetVersions:      map[string]string{"config": "release-a"},
		CreatedAt:          time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	})
	seedFile(t, ctx, store, paths.ArchiveManifest("demo", "api", "dep_demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: local
  assets:
    - type: Config
      name: config
      version: release-a
`))
	seedJSON(t, ctx, store, paths.ArchiveState("demo", "worker", "dep_worker_a"), historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_worker_a",
		Partition:          "demo",
		Intent:             "worker",
		AssetVersionIDs:    map[string]string{"binary": "asset-demo-binary-v1"},
		AssetVersions:      map[string]string{"binary": "release-a"},
		CreatedAt:          time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	})
	seedFile(t, ctx, store, paths.ArchiveManifest("demo", "worker", "dep_worker_a"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: worker
spec:
  targetPusher: local
  assets:
    - type: Compute
      name: binary
      version: release-a
`))
	seedJSON(t, ctx, store, paths.ArchiveState("demo", "api", "dep_demo_new"), historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_demo_new",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-demo-config-v2"},
		AssetVersions:      map[string]string{"config": "release-b"},
		CreatedAt:          time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	})
	seedFile(t, ctx, store, paths.ArchiveManifest("demo", "api", "dep_demo_new"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: local
  assets:
    - type: Config
      name: config
      version: release-b
`))
	seedJSON(t, ctx, store, paths.ArchiveState("demo", "worker", "dep_worker_b"), historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_worker_b",
		Partition:          "demo",
		Intent:             "worker",
		AssetVersionIDs:    map[string]string{"binary": "asset-demo-binary-v2"},
		AssetVersions:      map[string]string{"binary": "release-b"},
		CreatedAt:          time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	})
	seedFile(t, ctx, store, paths.ArchiveManifest("demo", "worker", "dep_worker_b"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: worker
spec:
  targetPusher: local
  assets:
    - type: Compute
      name: binary
      version: release-b
`))

	if err := reg.Run(ctx, []string{"partition", "tags", "--partition", "demo"}); err != nil {
		t.Fatalf("Run(partition tags) error = %v", err)
	}

	var tags []partitionTagRecord
	if err := json.Unmarshal(buf.Bytes(), &tags); err != nil {
		t.Fatalf("Unmarshal(partition tags output) error = %v", err)
	}
	if got, want := len(tags), 2; got != want {
		t.Fatalf("tag count = %d, want %d", got, want)
	}
	if got, want := tags[0].Tag, "release-b"; got != want {
		t.Fatalf("latest tag = %q, want %q", got, want)
	}
	if !tags[0].Current {
		t.Fatalf("expected latest tag to be marked current")
	}
	if tags[1].Current {
		t.Fatalf("expected older tag to not be marked current")
	}
	if got, want := strings.Join(tags[0].Intents, ","), "api,worker"; got != want {
		t.Fatalf("latest tag intents = %q, want %q", got, want)
	}
}

func TestPartitionRollbackRestoresTaggedArchivedManifests(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	seedFile(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`))
	seedFile(t, ctx, store, paths.IntentManifest("demo", "api"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: local
  assets:
    - type: Compute
      name: current
`))
	seedFile(t, ctx, store, paths.IntentManifest("demo", "worker"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: worker
spec:
  targetPusher: local
  assets:
    - type: Compute
      name: current
`))
	archivedAPI := []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: local
  assets:
    - type: Compute
      name: restored-api
      version: release-a
`)
	archivedWorker := []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: worker
spec:
  targetPusher: local
  assets:
    - type: Compute
      name: restored-worker
      version: release-a
`)
	seedFile(t, ctx, store, paths.ArchiveManifest("demo", "api", "dep_api_release_a"), archivedAPI)
	seedFile(t, ctx, store, paths.ArchiveManifest("demo", "worker", "dep_worker_release_a"), archivedWorker)
	seedJSON(t, ctx, store, paths.ArchiveState("demo", "api", "dep_api_release_a"), historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_api_release_a",
		Partition:          "demo",
		Intent:             "api",
		IntentVersionID:    "intent-api-release-a",
		AssetVersions:      map[string]string{"restored-api": "release-a"},
		CreatedAt:          time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	})
	seedJSON(t, ctx, store, paths.ArchiveState("demo", "worker", "dep_worker_release_a"), historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_worker_release_a",
		Partition:          "demo",
		Intent:             "worker",
		IntentVersionID:    "intent-worker-release-a",
		AssetVersions:      map[string]string{"restored-worker": "release-a"},
		CreatedAt:          time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	})

	if err := reg.Run(ctx, []string{"partition", "rollback", "--partition", "demo", "--tag", "release-a"}); err != nil {
		t.Fatalf("Run(partition rollback) error = %v", err)
	}

	var result partitionRollbackResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal(partition rollback output) error = %v", err)
	}
	if got, want := len(result.RestoredIntents), 2; got != want {
		t.Fatalf("restored intent count = %d, want %d", got, want)
	}
	gotAPI, err := store.ReadFile(ctx, paths.IntentManifest("demo", "api"))
	if err != nil {
		t.Fatalf("ReadFile(api intent) error = %v", err)
	}
	if strings.TrimSpace(string(gotAPI)) != strings.TrimSpace(string(archivedAPI)) {
		t.Fatalf("api intent manifest mismatch:\n%s", gotAPI)
	}
	gotWorker, err := store.ReadFile(ctx, paths.IntentManifest("demo", "worker"))
	if err != nil {
		t.Fatalf("ReadFile(worker intent) error = %v", err)
	}
	if strings.TrimSpace(string(gotWorker)) != strings.TrimSpace(string(archivedWorker)) {
		t.Fatalf("worker intent manifest mismatch:\n%s", gotWorker)
	}
}

func TestPartitionRollbackPreviousRestoresPreviousTaggedManifests(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	fixture := seedPartitionRollbackHistory(t, ctx, store, "demo")

	if err := reg.Run(ctx, []string{"partition", "rollback", "--partition", "demo", "--previous"}); err != nil {
		t.Fatalf("Run(partition rollback --previous) error = %v", err)
	}

	var result partitionRollbackResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal(partition rollback output) error = %v", err)
	}
	if got, want := result.Tag, "release-a"; got != want {
		t.Fatalf("rollback tag = %q, want %q", got, want)
	}
	gotAPI, err := store.ReadFile(ctx, paths.IntentManifest("demo", "api"))
	if err != nil {
		t.Fatalf("ReadFile(api intent) error = %v", err)
	}
	if strings.TrimSpace(string(gotAPI)) != strings.TrimSpace(string(fixture.previousAPI)) {
		t.Fatalf("api intent manifest mismatch:\n%s", gotAPI)
	}
	gotWorker, err := store.ReadFile(ctx, paths.IntentManifest("demo", "worker"))
	if err != nil {
		t.Fatalf("ReadFile(worker intent) error = %v", err)
	}
	if strings.TrimSpace(string(gotWorker)) != strings.TrimSpace(string(fixture.previousWorker)) {
		t.Fatalf("worker intent manifest mismatch:\n%s", gotWorker)
	}
}

func TestPartitionRollbackRequiresExactlyOneSelector(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	err := reg.Run(ctx, []string{"partition", "rollback", "--partition", "demo", "--tag", "release-a", "--previous"})
	if err == nil {
		t.Fatalf("Run(partition rollback) expected error")
	}
	if !strings.Contains(err.Error(), "exactly one of --tag or --previous is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPartitionTagDefaultsVersionForAllAssetsInDir(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(nil, printer)

	dir := writeTestPartitionBundle(t, nil)
	secondIntent := []byte("\napiVersion: guardian/v1alpha1\nkind: Intent\nmetadata:\n  name: worker\nspec:\n  targetPusher: docker-main\n  target:\n    cluster: docker-main\n  assets:\n    - type: Compute\n      name: worker\n      properties:\n        image: worker:v1\n")
	if err := os.WriteFile(filepath.Join(dir, "intents", "02-worker.yaml"), secondIntent, 0o644); err != nil {
		t.Fatalf("WriteFile(second intent) error = %v", err)
	}

	originalNow := rolloutTagNow
	rolloutTagNow = func() time.Time { return time.Date(2026, 5, 8, 13, 24, 45, 0, time.UTC) }
	defer func() { rolloutTagNow = originalNow }()

	if err := reg.Run(ctx, []string{"partition", "tag", "--dir", dir}); err != nil {
		t.Fatalf("Run(partition tag) error = %v", err)
	}

	var result rolloutTagResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal(partition tag output) error = %v", err)
	}
	if got, want := result.Version, "20260508-1324"; got != want {
		t.Fatalf("result version = %q, want %q", got, want)
	}
	if got, want := len(result.UpdatedAssets), 2; got != want {
		t.Fatalf("updated asset count = %d, want %d", got, want)
	}

	appVersions := readLocalIntentAssetVersions(t, filepath.Join(dir, "intents", "01-app.yaml"))
	if got, want := appVersions["app"], "20260508-1324"; got != want {
		t.Fatalf("app asset version = %q, want %q", got, want)
	}
	workerVersions := readLocalIntentAssetVersions(t, filepath.Join(dir, "intents", "02-worker.yaml"))
	if got, want := workerVersions["worker"], "20260508-1324"; got != want {
		t.Fatalf("worker asset version = %q, want %q", got, want)
	}
}

func TestPartitionTagUpdatesOnlyMatchingIntentAndAsset(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(nil, printer)

	dir := writeTestPartitionBundle(t, nil)
	primaryIntent := []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: docker-main
  target:
    cluster: docker-main
  assets:
    - type: Compute
      name: web
      properties:
        image: web:v1
    - type: Compute
      name: sidecar
      properties:
        image: sidecar:v1
`)
	secondaryIntent := []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: worker
spec:
  targetPusher: docker-main
  target:
    cluster: docker-main
  assets:
    - type: Compute
      name: sidecar
      properties:
        image: worker-sidecar:v1
`)
	if err := os.WriteFile(filepath.Join(dir, "intents", "01-app.yaml"), primaryIntent, 0o644); err != nil {
		t.Fatalf("WriteFile(primary intent) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "intents", "02-worker.yaml"), secondaryIntent, 0o644); err != nil {
		t.Fatalf("WriteFile(secondary intent) error = %v", err)
	}

	if err := reg.Run(ctx, []string{"partition", "tag", "--dir", dir, "--intent", "api", "--asset", "sidecar", "--version", "release-demo"}); err != nil {
		t.Fatalf("Run(partition tag) error = %v", err)
	}

	var result rolloutTagResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal(partition tag output) error = %v", err)
	}
	if got, want := len(result.UpdatedAssets), 1; got != want {
		t.Fatalf("updated asset count = %d, want %d", got, want)
	}
	if got, want := result.UpdatedAssets[0].Intent, "api"; got != want {
		t.Fatalf("updated asset intent = %q, want %q", got, want)
	}

	apiVersions := readLocalIntentAssetVersions(t, filepath.Join(dir, "intents", "01-app.yaml"))
	if got, want := apiVersions["sidecar"], "release-demo"; got != want {
		t.Fatalf("api sidecar version = %q, want %q", got, want)
	}
	if got := apiVersions["web"]; got != "" {
		t.Fatalf("api web version = %q, want empty", got)
	}
	workerVersions := readLocalIntentAssetVersions(t, filepath.Join(dir, "intents", "02-worker.yaml"))
	if got := workerVersions["sidecar"]; got != "" {
		t.Fatalf("worker sidecar version = %q, want empty", got)
	}
}

func TestPartitionTagHelpPrintsUsage(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(nil, printer)
	dir := writeTestPartitionBundle(t, nil)

	output := captureStderr(t, func() {
		if err := reg.Run(ctx, []string{"partition", "tag", "--dir", dir, "--help"}); err != nil {
			t.Fatalf("Run(partition tag --help) error = %v", err)
		}
	})
	if !strings.Contains(output, "Usage:") {
		t.Fatalf("expected usage output, got %q", output)
	}
	if !strings.Contains(output, "partition tag") {
		t.Fatalf("expected command name in help, got %q", output)
	}
	if !strings.Contains(output, "-dir") || !strings.Contains(output, "-version") || !strings.Contains(output, "-intent") || !strings.Contains(output, "-asset") {
		t.Fatalf("expected partition tag flags in help, got %q", output)
	}
}

func TestAllRegisteredCommandsPrintHelp(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(nil, printer)

	for _, entry := range reg.Entries() {
		t.Run(entry.Group+"-"+entry.Name, func(t *testing.T) {
			output := captureStderr(t, func() {
				if err := reg.Run(ctx, []string{entry.Group, entry.Name, "--help"}); err != nil {
					t.Fatalf("Run(%s %s --help) error = %v", entry.Group, entry.Name, err)
				}
			})
			if !strings.Contains(output, "Usage:") {
				t.Fatalf("expected usage output, got %q", output)
			}
			fullName := entry.Group + " " + entry.Name
			if !strings.Contains(output, fullName) {
				t.Fatalf("expected command name %q in help, got %q", fullName, output)
			}
		})
	}
}

func TestAllRegisteredCommandsPrintExtendedHelp(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(nil, printer)

	for _, entry := range reg.Entries() {
		t.Run(entry.Group+"-"+entry.Name, func(t *testing.T) {
			output := captureStderr(t, func() {
				if err := reg.Run(ctx, []string{entry.Group, entry.Name, "--help-full"}); err != nil {
					t.Fatalf("Run(%s %s --help-full) error = %v", entry.Group, entry.Name, err)
				}
			})
			if !strings.Contains(output, "Usage:") {
				t.Fatalf("expected usage output, got %q", output)
			}
			if !strings.Contains(output, "Extended help:") {
				t.Fatalf("expected extended help output, got %q", output)
			}
			fullName := entry.Group + " " + entry.Name
			if !strings.Contains(output, fullName) {
				t.Fatalf("expected command name %q in help, got %q", fullName, output)
			}
		})
	}
}

func TestAllCommandGroupsPrintHelp(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(nil, printer)

	seen := map[string]struct{}{}
	for _, entry := range reg.Entries() {
		if _, ok := seen[entry.Group]; ok {
			continue
		}
		seen[entry.Group] = struct{}{}

		t.Run(entry.Group, func(t *testing.T) {
			output := captureStderr(t, func() {
				if err := reg.Run(ctx, []string{entry.Group, "--help"}); err != nil {
					t.Fatalf("Run(%s --help) error = %v", entry.Group, err)
				}
			})
			if !strings.Contains(output, "Usage:") {
				t.Fatalf("expected usage output, got %q", output)
			}
			if !strings.Contains(output, entry.Group+" <command>") {
				t.Fatalf("expected group usage for %q, got %q", entry.Group, output)
			}
		})
	}
}

func TestCommandSurfaceOmitsRemovedCommands(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(nil, printer)

	visible := map[string]struct{}{}
	for _, entry := range reg.Entries() {
		visible[entry.Group+" "+entry.Name] = struct{}{}
	}

	for _, commandName := range []string{
		"docker assets",
		"doctor check",
		"doctor run",
		"events tail",
		"file put",
		"history list",
		"partition deploy",
		"partition put",
		"rollback apply",
		"reconcile run",
		"rollouts list",
		"rollout tag",
		"rollouts tag",
	} {
		if _, ok := visible[commandName]; ok {
			t.Fatalf("removed command %q should not be registered", commandName)
		}
		parts := strings.SplitN(commandName, " ", 2)
		err := reg.Run(ctx, parts)
		if err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("expected removed command %q to fail with unknown command, got %v", commandName, err)
		}
	}
	for _, commandName := range []string{
		"partition rollback",
		"partition push",
		"partition reconcile",
		"partition set-config",
		"partition tag",
		"partition tags",
		"partition wait",
	} {
		if _, ok := visible[commandName]; !ok {
			t.Fatalf("expected visible command %q", commandName)
		}
	}

	output := captureStderr(t, func() {
		if err := reg.Run(ctx, []string{"partition", "--help"}); err != nil {
			t.Fatalf("Run(partition --help) error = %v", err)
		}
	})
	if !strings.Contains(output, "reconcile") {
		t.Fatalf("expected partition help to advertise reconcile, got %q", output)
	}
	if strings.Contains(output, "deploy") {
		t.Fatalf("expected partition help to omit deploy, got %q", output)
	}
	if strings.Contains(output, "file <command>") || strings.Contains(output, "doctor <command>") || strings.Contains(output, "history <command>") || strings.Contains(output, "events <command>") || strings.Contains(output, "reconcile <command>") {
		t.Fatalf("expected removed command groups to be absent from visible help, got %q", output)
	}
}

func TestPartitionListRequiresStoreInsteadOfPanicking(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(nil, printer)

	err := reg.Run(ctx, []string{"partition", "list"})
	if err == nil {
		t.Fatalf("Run(partition list) expected error")
	}
	if !strings.Contains(err.Error(), storeRequiredMessage) {
		t.Fatalf("error = %q, want store-required message", err)
	}
}

func TestAssetCatalogWorksWithoutStore(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(nil, printer)

	if err := reg.Run(ctx, []string{"asset", "catalog"}); err != nil {
		t.Fatalf("Run(asset catalog) error = %v", err)
	}
	var catalog []assetdefs.CatalogTemplate
	if err := json.Unmarshal(buf.Bytes(), &catalog); err != nil {
		t.Fatalf("Unmarshal(asset catalog output) error = %v", err)
	}
	if len(catalog) == 0 {
		t.Fatalf("expected asset catalog entries")
	}
}

func TestAssetCatalogDoesNotOpenAutoStore(t *testing.T) {
	ctx := context.Background()
	resetGuardianAutoStoreHooks(t)

	opened := false
	guardianStoreOpener = func(ctx context.Context, storeDir, monofsRouter, monofsToken, monofsPrincipalID string, monofsUseExternalAddresses bool) (guardianapi.Store, interface{ Close() error }, error) {
		opened = true
		return memory.New(), nil, nil
	}

	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(newAutoStore(autoStoreRequest{}), printer)

	if err := reg.Run(ctx, []string{"asset", "catalog"}); err != nil {
		t.Fatalf("Run(asset catalog) error = %v", err)
	}
	if opened {
		t.Fatalf("expected asset catalog to avoid opening the store")
	}
}

func TestPartitionListUsesGuardianDiscoveryWithoutFlags(t *testing.T) {
	ctx := context.Background()
	resetGuardianAutoStoreHooks(t)
	clearGuardianConnectionEnv(t)

	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/client-config" {
			t.Fatalf("unexpected discovery path %q", r.URL.Path)
		}
		writeJSONResponse(t, w, guardianClientConfig{MonoFS: &guardianMonoFSClientConfig{
			RouterAddr:           "router.dev:9443",
			Token:                "guardian-token",
			UseExternalAddresses: true,
		}})
	}))
	defer discovery.Close()
	guardianDiscoveryURL = discovery.URL
	guardianDiscoveryHTTPClient = discovery.Client()

	store := memory.New()
	seedFile(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`))

	var got struct {
		router      string
		token       string
		principalID string
		useExternal bool
	}
	guardianStoreOpener = func(ctx context.Context, storeDir, monofsRouter, monofsToken, monofsPrincipalID string, monofsUseExternalAddresses bool) (guardianapi.Store, interface{ Close() error }, error) {
		got.router = monofsRouter
		got.token = monofsToken
		got.principalID = monofsPrincipalID
		got.useExternal = monofsUseExternalAddresses
		return store, nil, nil
	}

	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &buf}
	reg := registerCommands(newAutoStore(autoStoreRequest{monofsPrincipalID: "guardianctl"}), printer)

	if err := reg.Run(ctx, []string{"partition", "list"}); err != nil {
		t.Fatalf("Run(partition list) error = %v", err)
	}
	if !strings.Contains(buf.String(), "demo") {
		t.Fatalf("expected partition list output, got %q", buf.String())
	}
	if got.router != "router.dev:9443" || got.token != "guardian-token" || got.principalID != "guardianctl" || !got.useExternal {
		t.Fatalf("unexpected resolved connection: %+v", got)
	}
}

func TestPartitionListUsesGuardianDiscoveryToken(t *testing.T) {
	ctx := context.Background()
	resetGuardianAutoStoreHooks(t)
	clearGuardianConnectionEnv(t)

	const token = "discovery-secret"
	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(guardianDiscoveryTokenHeader); got != token {
			t.Fatalf("discovery token header = %q, want %q", got, token)
		}
		writeJSONResponse(t, w, guardianClientConfig{MonoFS: &guardianMonoFSClientConfig{
			RouterAddr:           "router.dev:9443",
			Token:                "guardian-token",
			UseExternalAddresses: true,
		}})
	}))
	defer discovery.Close()
	guardianDiscoveryURL = discovery.URL
	guardianDiscoveryHTTPClient = discovery.Client()

	store := memory.New()
	seedFile(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`))

	guardianStoreOpener = func(ctx context.Context, storeDir, monofsRouter, monofsToken, monofsPrincipalID string, monofsUseExternalAddresses bool) (guardianapi.Store, interface{ Close() error }, error) {
		return store, nil, nil
	}

	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &buf}
	reg := registerCommands(newAutoStore(autoStoreRequest{monofsPrincipalID: "guardianctl", guardianDiscoveryToken: token}), printer)

	if err := reg.Run(ctx, []string{"partition", "list"}); err != nil {
		t.Fatalf("Run(partition list) error = %v", err)
	}
}

func TestPartitionListDiscoveryForbiddenIncludesKubernetesHelp(t *testing.T) {
	ctx := context.Background()
	resetGuardianAutoStoreHooks(t)
	clearGuardianConnectionEnv(t)

	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "client config requires loopback access or a valid discovery token"})
	}))
	defer discovery.Close()

	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &buf}
	reg := registerCommands(newAutoStore(autoStoreRequest{monofsPrincipalID: "guardianctl", guardianURL: discovery.URL}), printer)

	err := reg.Run(ctx, []string{"partition", "list"})
	if err == nil {
		t.Fatalf("Run(partition list) expected discovery error")
	}
	message := err.Error()
	for _, want := range []string{"guardian.clientDiscoveryToken", "monofs.clientApiEndpoint", "--guardian-discovery-token"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected discovery error to mention %q, got %q", want, message)
		}
	}
}

func TestPartitionListFallsBackToLocalDogfoodDefaults(t *testing.T) {
	ctx := context.Background()
	resetGuardianAutoStoreHooks(t)
	clearGuardianConnectionEnv(t)

	guardianDiscoveryURL = "http://127.0.0.1:1"
	guardianDiscoveryHTTPClient = &http.Client{Timeout: 10 * time.Millisecond}

	store := memory.New()
	seedFile(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`))

	var got struct {
		router      string
		token       string
		useExternal bool
	}
	guardianStoreOpener = func(ctx context.Context, storeDir, monofsRouter, monofsToken, monofsPrincipalID string, monofsUseExternalAddresses bool) (guardianapi.Store, interface{ Close() error }, error) {
		got.router = monofsRouter
		got.token = monofsToken
		got.useExternal = monofsUseExternalAddresses
		return store, nil, nil
	}

	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &buf}
	reg := registerCommands(newAutoStore(autoStoreRequest{monofsPrincipalID: "guardianctl"}), printer)

	if err := reg.Run(ctx, []string{"partition", "list"}); err != nil {
		t.Fatalf("Run(partition list) error = %v", err)
	}
	if got.router != defaultLocalMonoFSRouter || got.token != defaultLocalMonoFSToken || !got.useExternal {
		t.Fatalf("unexpected fallback connection: %+v", got)
	}
}

func TestPartitionListUsesGuardianAPIAliases(t *testing.T) {
	ctx := context.Background()
	resetGuardianAutoStoreHooks(t)
	clearGuardianConnectionEnv(t)

	const token = "client-discovery-secret"
	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(guardianDiscoveryTokenHeader); got != token {
			t.Fatalf("discovery token header = %q, want %q", got, token)
		}
		writeJSONResponse(t, w, guardianClientConfig{MonoFS: &guardianMonoFSClientConfig{
			RouterAddr:           "router.dev:9443",
			Token:                "guardian-token",
			UseExternalAddresses: true,
		}})
	}))
	defer discovery.Close()
	t.Setenv("GUARDIAN_API_URL", discovery.URL)
	t.Setenv("GUARDIAN_CLIENT_DISCOVERY_TOKEN", token)

	store := memory.New()
	seedFile(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`))

	var got struct {
		router      string
		token       string
		principalID string
		useExternal bool
	}
	guardianStoreOpener = func(ctx context.Context, storeDir, monofsRouter, monofsToken, monofsPrincipalID string, monofsUseExternalAddresses bool) (guardianapi.Store, interface{ Close() error }, error) {
		got.router = monofsRouter
		got.token = monofsToken
		got.principalID = monofsPrincipalID
		got.useExternal = monofsUseExternalAddresses
		return store, nil, nil
	}

	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &buf}
	reg := registerCommands(newAutoStore(autoStoreRequest{monofsPrincipalID: "guardianctl"}), printer)

	if err := reg.Run(ctx, []string{"partition", "list"}); err != nil {
		t.Fatalf("Run(partition list) error = %v", err)
	}
	if got.router != "router.dev:9443" || got.token != "guardian-token" || got.principalID != "guardianctl" || !got.useExternal {
		t.Fatalf("unexpected resolved connection: %+v", got)
	}
}

func TestPartitionPutStoresManifest(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	manifestPath := filepath.Join(t.TempDir(), "partition.yaml")
	manifestContent := []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: monofs-demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 2m
    jitterPercent: 5
  defaults:
    targetPusher: docker-main
    target:
      cluster: docker-main
`)
	if err := os.WriteFile(manifestPath, manifestContent, 0o644); err != nil {
		t.Fatalf("WriteFile(partition manifest) error = %v", err)
	}

	if err := reg.Run(ctx, []string{"partition", "set-config", "--file", manifestPath}); err != nil {
		t.Fatalf("Run(partition set-config) error = %v", err)
	}

	got, err := store.ReadFile(ctx, paths.PartitionConfig("monofs-demo"))
	if err != nil {
		t.Fatalf("ReadFile(partition config) error = %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("Unmarshal(stored partition) error = %v", err)
	}
	metadata, _ := parsed["metadata"].(map[string]any)
	if metadata["name"] != "monofs-demo" {
		t.Fatalf("stored partition name = %v", metadata["name"])
	}
}

func TestPartitionPushSkipsUnchangedFiles(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	dir := writeTestPartitionBundle(t, map[string][]byte{
		"payloads/app/app/payload.docker.yaml": []byte("ports:\n  - host: 18080\n    container: 8080\n"),
	})

	if err := reg.Run(ctx, []string{"partition", "push", "--dir", dir}); err != nil {
		t.Fatalf("Run(partition push) error = %v", err)
	}
	if err := reg.Run(ctx, []string{"partition", "push", "--dir", dir}); err != nil {
		t.Fatalf("Run(partition push second time) error = %v", err)
	}

	assertVersionCount(t, ctx, store, paths.PartitionConfig("demo"), 1)
	assertVersionCount(t, ctx, store, paths.IntentManifest("demo", "app"), 1)
	payloadPath := "/partitions/demo/payloads/app/app/payload.docker.yaml"
	assertVersionCount(t, ctx, store, payloadPath, 1)

	if err := os.WriteFile(filepath.Join(dir, "payloads", "app", "app", "payload.docker.yaml"), []byte("ports:\n  - host: 19090\n    container: 8080\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(updated payload) error = %v", err)
	}
	if err := reg.Run(ctx, []string{"partition", "push", "--dir", dir}); err != nil {
		t.Fatalf("Run(partition push after update) error = %v", err)
	}

	assertVersionCount(t, ctx, store, paths.PartitionConfig("demo"), 1)
	assertVersionCount(t, ctx, store, paths.IntentManifest("demo", "app"), 1)
	assertVersionCount(t, ctx, store, payloadPath, 2)
}

func TestPartitionPushReportsRolloutDiffForIntentVersionChange(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	dir := writeTestPartitionBundle(t, map[string][]byte{
		"payloads/app/app/payload.docker.yaml": []byte("ports:\n  - host: 18080\n    container: 8080\n"),
	})
	if err := reg.Run(ctx, []string{"partition", "push", "--dir", dir}); err != nil {
		t.Fatalf("Run(partition push initial) error = %v", err)
	}
	buf.Reset()

	intentWithVersion := []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: app
spec:
  targetPusher: docker-main
  target:
    cluster: docker-main
  assets:
    - type: Compute
      name: app
      version: 0.0.3
      payload:
        docker: /partitions/demo/payloads/app/app/payload.docker.yaml
      properties:
        image: demo:v1
`)
	if err := os.WriteFile(filepath.Join(dir, "intents", "01-app.yaml"), intentWithVersion, 0o644); err != nil {
		t.Fatalf("WriteFile(intent with version) error = %v", err)
	}

	if err := reg.Run(ctx, []string{"partition", "push", "--dir", dir}); err != nil {
		t.Fatalf("Run(partition push updated) error = %v", err)
	}

	var result partitionPushResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal(partition push output) error = %v", err)
	}
	if got, want := len(result.Rollouts), 1; got != want {
		t.Fatalf("rollout count = %d, want %d", got, want)
	}
	if got, want := result.Rollouts[0].Intent, "app"; got != want {
		t.Fatalf("rollout intent = %q, want %q", got, want)
	}
	if got, want := result.Rollouts[0].Summary, "Rollout: 1 asset updated"; got != want {
		t.Fatalf("rollout summary = %q, want %q", got, want)
	}
	if got, want := len(result.Rollouts[0].Assets), 1; got != want {
		t.Fatalf("rollout asset count = %d, want %d", got, want)
	}
	if got, want := result.Rollouts[0].Assets[0].Name, "app"; got != want {
		t.Fatalf("rollout asset name = %q, want %q", got, want)
	}
	if got, want := result.Rollouts[0].Assets[0].Version, "0.0.3"; got != want {
		t.Fatalf("rollout asset version = %q, want %q", got, want)
	}
}

func TestPartitionPushTextReportsNoRolloutChanges(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &buf}
	reg := registerCommands(store, printer)

	dir := writeTestPartitionBundle(t, map[string][]byte{
		"payloads/app/app/payload.docker.yaml": []byte("ports:\n  - host: 18080\n    container: 8080\n"),
	})

	if err := reg.Run(ctx, []string{"partition", "push", "--dir", dir}); err != nil {
		t.Fatalf("Run(partition push initial) error = %v", err)
	}
	buf.Reset()
	if err := reg.Run(ctx, []string{"partition", "push", "--dir", dir}); err != nil {
		t.Fatalf("Run(partition push second) error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "rollout diff: no asset changes") {
		t.Fatalf("expected no-change rollout message, got %q", output)
	}
}

func TestPartitionPushRemovesMissingManagedFilesButKeepsSecrets(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	dir := writeTestPartitionBundle(t, map[string][]byte{
		"payloads/app/app/payload.docker.yaml": []byte("ports:\n  - host: 18080\n    container: 8080\n"),
	})

	seedFile(t, ctx, store, paths.IntentManifest("demo", "old"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: old
spec:
  targetPusher: docker-main
  target:
    cluster: docker-main
  assets:
    - type: Compute
      name: old
      properties:
        image: old:v1
`))
	seedFile(t, ctx, store, "/partitions/demo/payloads/app/obsolete.txt", []byte("old"))
	seedFile(t, ctx, store, "/partitions/demo/secrets/encryption-key", []byte("keep-me"))

	if err := reg.Run(ctx, []string{"partition", "push", "--dir", dir}); err != nil {
		t.Fatalf("Run(partition push) error = %v", err)
	}

	if _, err := store.ReadFile(ctx, paths.IntentManifest("demo", "old")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old intent should be deleted, got %v", err)
	}
	if _, err := store.ReadFile(ctx, "/partitions/demo/payloads/app/obsolete.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("obsolete payload should be deleted, got %v", err)
	}
	secret, err := store.ReadFile(ctx, "/partitions/demo/secrets/encryption-key")
	if err != nil {
		t.Fatalf("ReadFile(secret) error = %v", err)
	}
	if string(secret) != "keep-me" {
		t.Fatalf("secret content = %q", secret)
	}
}

func TestPartitionReconcileRunsAfterPush(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	dir := writeTestPartitionBundle(t, map[string][]byte{
		"payloads/app/app/payload.docker.yaml": []byte("ports:\n  - host: 18080\n    container: 8080\n"),
	})

	if err := reg.Run(ctx, []string{"partition", "push", "--dir", dir}); err != nil {
		t.Fatalf("Run(partition push) error = %v", err)
	}
	buf.Reset()

	if err := reg.Run(ctx, []string{"partition", "reconcile", "--partition", "demo"}); err != nil {
		t.Fatalf("Run(partition reconcile) error = %v", err)
	}

	var result partitionReconcileResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal(partition reconcile output) error = %v", err)
	}
	if !result.Success || result.Partition != "demo" {
		t.Fatalf("unexpected reconcile result: %+v", result)
	}

	queueFiles, err := walkFiles(ctx, store, "/.queues/docker-main")
	if err != nil {
		t.Fatalf("walkFiles(queue) error = %v", err)
	}
	if len(queueFiles) == 0 {
		t.Fatalf("expected reconcile to enqueue work, got no queue files")
	}
}

func TestPartitionPushReportsManifestPathForInvalidConfigYAML(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	dir := writeTestPartitionBundle(t, map[string][]byte{
		"payloads/app/app/payload.docker.yaml": []byte("ports:\n  - host: 18080\n    container: 8080\n"),
	})
	configPath := filepath.Join(dir, "config.yaml")
	brokenConfig := []byte("apiVersion: guardian/v1alpha1\nkind: Partition\nmetadata:\n  name: demo\nspec:\n  deletionPolicy: orphan\n  reconciliation:\n    mode: auto\n    interval: 30s\n  labels:\n    stack: k8s\n      endpoint: http://localhost:5000/v2/\n")
	if err := os.WriteFile(configPath, brokenConfig, 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	err := reg.Run(ctx, []string{"partition", "push", "--dir", dir})
	if err == nil {
		t.Fatalf("Run(partition push) expected error")
	}
	if !strings.Contains(err.Error(), configPath) {
		t.Fatalf("error = %q, want config path %q", err, configPath)
	}
	if !strings.Contains(err.Error(), "mapping values are not allowed in this context") {
		t.Fatalf("error = %q, want YAML parse detail", err)
	}
	if !strings.Contains(err.Error(), "endpoint: http://localhost:5000/v2/") {
		t.Fatalf("error = %q, want offending source line", err)
	}
}

func TestPartitionStatusReportsState(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	seedFile(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`))
	seedJSON(t, ctx, store, paths.PartitionState("demo"), statedomain.PartitionState{
		APIVersion:       "guardian/v1alpha1",
		Kind:             "PartitionState",
		Partition:        "demo",
		Status:           "Healthy",
		DisplayStatus:    "Stable",
		Summary:          "1 intent(s): 1 healthy",
		LastReconciledAt: time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		Metrics: statedomain.PartitionStatusMetrics{
			TotalIntents:   1,
			HealthyIntents: 1,
		},
	})
	seedJSON(t, ctx, store, paths.IntentState("demo", "app"), statedomain.IntentState{
		APIVersion: "guardian/v1alpha1",
		Kind:       "IntentState",
		Partition:  "demo",
		Intent:     "app",
		Status:     statedomain.StatusHealthy,
	})

	if err := reg.Run(ctx, []string{"partition", "status", "--partition", "demo"}); err != nil {
		t.Fatalf("Run(partition status) error = %v", err)
	}

	var result partitionStatusResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal(partition status output) error = %v", err)
	}
	if got, want := result.Status, "Healthy"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if got, want := result.IntentStatuses["app"], "Healthy"; got != want {
		t.Fatalf("intent status = %q, want %q", got, want)
	}
}

func TestPartitionWaitReturnsWhenHealthy(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	seedFile(t, ctx, store, paths.PartitionConfig("demo"), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`))

	errCh := make(chan error, 1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		content, err := json.Marshal(statedomain.PartitionState{
			APIVersion:    "guardian/v1alpha1",
			Kind:          "PartitionState",
			Partition:     "demo",
			Status:        "Healthy",
			DisplayStatus: "Stable",
			Summary:       "ready",
		})
		if err != nil {
			errCh <- err
			return
		}
		_, err = store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes:  []guardianapi.PathWrite{{LogicalPath: paths.PartitionState("demo"), Content: content}},
			Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "ready"},
		})
		errCh <- err
	}()

	if err := reg.Run(ctx, []string{"partition", "wait", "--partition", "demo", "--timeout", "300ms", "--interval", "10ms"}); err != nil {
		t.Fatalf("Run(partition wait) error = %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("async state write error = %v", err)
	}
}

func TestPartitionReconcileWaitsForHealthyState(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	dir := writeTestPartitionBundle(t, map[string][]byte{
		"payloads/app/app/payload.docker.yaml": []byte("ports:\n  - host: 18080\n    container: 8080\n"),
	})
	if err := reg.Run(ctx, []string{"partition", "push", "--dir", dir}); err != nil {
		t.Fatalf("Run(partition push) error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		time.Sleep(30 * time.Millisecond)
		content, err := json.Marshal(statedomain.PartitionState{
			APIVersion:    "guardian/v1alpha1",
			Kind:          "PartitionState",
			Partition:     "demo",
			Status:        "Healthy",
			DisplayStatus: "Stable",
			Summary:       "ready",
		})
		if err != nil {
			errCh <- err
			return
		}
		_, err = store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes:  []guardianapi.PathWrite{{LogicalPath: paths.PartitionState("demo"), Content: content}},
			Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "ready"},
		})
		if err == nil {
			intentContent, marshalErr := json.Marshal(statedomain.IntentState{
				APIVersion: "guardian/v1alpha1",
				Kind:       "IntentState",
				Partition:  "demo",
				Intent:     "app",
				Status:     statedomain.StatusHealthy,
			})
			if marshalErr != nil {
				err = marshalErr
			} else {
				_, err = store.UpsertFiles(ctx, guardianapi.MutationBatch{
					Writes:  []guardianapi.PathWrite{{LogicalPath: paths.IntentState("demo", "app"), Content: intentContent}},
					Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "ready"},
				})
			}
		}
		errCh <- err
	}()

	buf.Reset()
	if err := reg.Run(ctx, []string{"partition", "reconcile", "--partition", "demo", "--wait", "--wait-timeout", "400ms", "--wait-interval", "10ms"}); err != nil {
		t.Fatalf("Run(partition reconcile --wait) error = %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("async ready write error = %v", err)
	}

	var result partitionReconcileResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal(partition reconcile --wait output) error = %v", err)
	}
	if !result.Waited || result.Status == nil || result.Status.Status != "Healthy" {
		t.Fatalf("unexpected reconcile wait result: %+v", result)
	}
}

func TestPartitionPushIncludesSecretsWhenRequested(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	dir := writeTestPartitionBundle(t, map[string][]byte{
		"payloads/app/app/payload.docker.yaml": []byte("ports:\n  - host: 18080\n    container: 8080\n"),
		"secrets/encryption-key":               []byte("secret-material\n"),
	})

	if err := reg.Run(ctx, []string{"partition", "push", "--dir", dir, "--include-secrets"}); err != nil {
		t.Fatalf("Run(partition push --include-secrets) error = %v", err)
	}

	got, err := store.ReadFile(ctx, "/partitions/demo/secrets/encryption-key")
	if err != nil {
		t.Fatalf("ReadFile(secret) error = %v", err)
	}
	if string(got) != "secret-material\n" {
		t.Fatalf("stored secret mismatch: %q", got)
	}
}

func TestAssetDescribeIncludesFieldHints(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	if err := reg.Run(ctx, []string{"asset", "describe", "--type", "Compute"}); err != nil {
		t.Fatalf("Run(asset describe) error = %v", err)
	}

	var item assetdefs.CatalogTemplate
	if err := json.Unmarshal(buf.Bytes(), &item); err != nil {
		t.Fatalf("Unmarshal(asset describe output) error = %v", err)
	}
	if item.Type != "Compute" {
		t.Fatalf("asset type = %q, want Compute", item.Type)
	}
	if !hasCatalogHint(item.Hints, "image") {
		t.Fatalf("expected image hint in %+v", item.Hints)
	}
	if !hasCatalogHint(item.Hints, "ports[].containerPort") {
		t.Fatalf("expected nested port hint in %+v", item.Hints)
	}
	if !hasCatalogField(item.Fields, "networks") {
		t.Fatalf("expected networks field in %+v", item.Fields)
	}
	if !strings.Contains(buf.String(), "Container image to run") {
		t.Fatalf("expected image description in output, got %s", buf.String())
	}
	buf.Reset()

	if err := reg.Run(ctx, []string{"asset", "catalog"}); err != nil {
		t.Fatalf("Run(asset catalog) error = %v", err)
	}

	var catalog []assetdefs.CatalogTemplate
	if err := json.Unmarshal(buf.Bytes(), &catalog); err != nil {
		t.Fatalf("Unmarshal(asset catalog output) error = %v", err)
	}
	if !hasCatalogType(catalog, "Network") {
		t.Fatalf("expected Network catalog entry in %+v", catalog)
	}
}

func TestIntentDescribeAndManifestAwareAssetDescribeMergeYamlHints(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var buf bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatJSON, Writer: &buf}
	reg := registerCommands(store, printer)

	seedFile(t, ctx, store, paths.IntentManifest("demo", "web"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: web
spec:
  targetPusher: local
  target:
    cluster: local
  hints:
    - path: outputs.url
      description: Public URL for clients.
    - path: assets.api.ports[0].containerPort
      description: Primary HTTP listener.
  assets:
    - type: Compute
      name: api
      hints:
        - path: image
          description: Manifest image override.
      properties:
        image: ghcr.io/example/api:latest
        ports:
          - containerPort: 8080
`))

	if err := reg.Run(ctx, []string{"intent", "describe", "--partition", "demo", "--intent", "web"}); err != nil {
		t.Fatalf("Run(intent describe) error = %v", err)
	}
	var intentResp intentDescribeResponse
	if err := json.Unmarshal(buf.Bytes(), &intentResp); err != nil {
		t.Fatalf("Unmarshal(intent describe output) error = %v", err)
	}
	if len(intentResp.OutputHints) != 1 || intentResp.OutputHints[0].Path != "outputs.url" {
		t.Fatalf("unexpected output hints: %+v", intentResp.OutputHints)
	}
	if len(intentResp.Assets) != 1 {
		t.Fatalf("asset count = %d, want 1", len(intentResp.Assets))
	}
	if got := intentResp.Assets[0].Manifest.Properties["image"]; got != "ghcr.io/example/api:latest" {
		t.Fatalf("asset manifest image = %v, want ghcr.io/example/api:latest", got)
	}
	buf.Reset()

	if err := reg.Run(ctx, []string{"asset", "describe", "--partition", "demo", "--intent", "web", "--asset", "api"}); err != nil {
		t.Fatalf("Run(asset describe manifest-aware) error = %v", err)
	}
	var item assetdefs.CatalogTemplate
	if err := json.Unmarshal(buf.Bytes(), &item); err != nil {
		t.Fatalf("Unmarshal(asset describe merged output) error = %v", err)
	}
	imageFound := false
	portFound := false
	for _, hint := range item.Hints {
		switch hint.Path {
		case "image":
			imageFound = hint.Description == "Manifest image override."
		case "ports[].containerPort":
			portFound = hint.Description == "Primary HTTP listener."
		}
	}
	if !imageFound || !portFound {
		t.Fatalf("unexpected merged hints: %+v", item.Hints)
	}
}

func writeTestPartitionBundle(t *testing.T, extraFiles map[string][]byte) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(filepath.Join(dir, "intents"), 0o755); err != nil {
		t.Fatalf("MkdirAll(intents) error = %v", err)
	}
	config := []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
`)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), config, 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	intent := []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: app
spec:
  targetPusher: docker-main
  target:
    cluster: docker-main
  assets:
    - type: Compute
      name: app
      payload:
        docker: /partitions/demo/payloads/app/app/payload.docker.yaml
      properties:
        image: demo:v1
`)
	if err := os.WriteFile(filepath.Join(dir, "intents", "01-app.yaml"), intent, 0o644); err != nil {
		t.Fatalf("WriteFile(intent) error = %v", err)
	}
	for relativePath, content := range extraFiles {
		fullPath := filepath.Join(dir, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", relativePath, err)
		}
		if err := os.WriteFile(fullPath, content, 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", relativePath, err)
		}
	}
	return dir
}

func readLocalIntentAssetVersions(t *testing.T, manifestPath string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", manifestPath, err)
	}
	parsed, err := manifestpkg.ParseIntent(data)
	if err != nil {
		t.Fatalf("ParseIntent(%s) error = %v", manifestPath, err)
	}
	versions := make(map[string]string, len(parsed.Spec.Assets))
	for _, asset := range parsed.Spec.Assets {
		versions[asset.Name] = strings.TrimSpace(asset.Version)
	}
	return versions
}

func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer reader.Close()
	originalStderr := os.Stderr
	os.Stderr = writer
	defer func() { os.Stderr = originalStderr }()

	outputCh := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		outputCh <- string(data)
	}()

	run()
	_ = writer.Close()
	return <-outputCh
}

func assertVersionCount(t *testing.T, ctx context.Context, store *memory.Store, logicalPath string, want int) {
	t.Helper()
	versions, err := store.ListVersions(ctx, logicalPath)
	if err != nil {
		t.Fatalf("ListVersions(%s) error = %v", logicalPath, err)
	}
	if len(versions) != want {
		t.Fatalf("version count for %s = %d, want %d", logicalPath, len(versions), want)
	}
}

func seedFile(t *testing.T, ctx context.Context, store guardianapi.WriteStore, logicalPath string, content []byte) {
	t.Helper()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed"},
	}); err != nil {
		t.Fatalf("UpsertFiles(%s) error = %v", logicalPath, err)
	}
}

func seedJSON(t *testing.T, ctx context.Context, store guardianapi.WriteStore, logicalPath string, value any) {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(value); err != nil {
		t.Fatalf("Encode(%s) error = %v", logicalPath, err)
	}
	seedFile(t, ctx, store, logicalPath, buf.Bytes())
}

type partitionRollbackHistoryFixture struct {
	previousAPI    []byte
	previousWorker []byte
	currentAPI     []byte
	currentWorker  []byte
}

func seedPartitionRollbackHistory(t *testing.T, ctx context.Context, store guardianapi.Store, partitionName string) partitionRollbackHistoryFixture {
	t.Helper()
	seedFile(t, ctx, store, paths.PartitionConfig(partitionName), []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
`))
	seedFile(t, ctx, store, paths.IntentManifest(partitionName, "api"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: local
  assets:
    - type: Compute
      name: drifted-api
`))
	seedFile(t, ctx, store, paths.IntentManifest(partitionName, "worker"), []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: worker
spec:
  targetPusher: local
  assets:
    - type: Compute
      name: drifted-worker
`))

	fixture := partitionRollbackHistoryFixture{
		previousAPI: []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: local
  assets:
    - type: Compute
      name: api
      version: release-a
`),
		previousWorker: []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: worker
spec:
  targetPusher: local
  assets:
    - type: Compute
      name: worker
      version: release-a
`),
		currentAPI: []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: api
spec:
  targetPusher: local
  assets:
    - type: Compute
      name: api
      version: release-b
`),
		currentWorker: []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: worker
spec:
  targetPusher: local
  assets:
    - type: Compute
      name: worker
      version: release-b
`),
	}

	seedFile(t, ctx, store, paths.ArchiveManifest(partitionName, "api", "dep_api_release_a"), fixture.previousAPI)
	seedFile(t, ctx, store, paths.ArchiveManifest(partitionName, "worker", "dep_worker_release_a"), fixture.previousWorker)
	seedFile(t, ctx, store, paths.ArchiveManifest(partitionName, "api", "dep_api_release_b"), fixture.currentAPI)
	seedFile(t, ctx, store, paths.ArchiveManifest(partitionName, "worker", "dep_worker_release_b"), fixture.currentWorker)

	seedJSON(t, ctx, store, paths.ArchiveState(partitionName, "api", "dep_api_release_a"), historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_api_release_a",
		Partition:          partitionName,
		Intent:             "api",
		IntentVersionID:    "intent-api-release-a",
		AssetVersions:      map[string]string{"api": "release-a"},
		CreatedAt:          time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	})
	seedJSON(t, ctx, store, paths.ArchiveState(partitionName, "worker", "dep_worker_release_a"), historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_worker_release_a",
		Partition:          partitionName,
		Intent:             "worker",
		IntentVersionID:    "intent-worker-release-a",
		AssetVersions:      map[string]string{"worker": "release-a"},
		CreatedAt:          time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	})
	seedJSON(t, ctx, store, paths.ArchiveState(partitionName, "api", "dep_api_release_b"), historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_api_release_b",
		Partition:          partitionName,
		Intent:             "api",
		IntentVersionID:    "intent-api-release-b",
		AssetVersions:      map[string]string{"api": "release-b"},
		CreatedAt:          time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	})
	seedJSON(t, ctx, store, paths.ArchiveState(partitionName, "worker", "dep_worker_release_b"), historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_worker_release_b",
		Partition:          partitionName,
		Intent:             "worker",
		IntentVersionID:    "intent-worker-release-b",
		AssetVersions:      map[string]string{"worker": "release-b"},
		CreatedAt:          time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	})

	return fixture
}

func hasCatalogType(items []assetdefs.CatalogTemplate, want string) bool {
	for _, item := range items {
		if item.Type == want {
			return true
		}
	}
	return false
}

func hasCatalogHint(items []assetdefs.CatalogHint, want string) bool {
	for _, item := range items {
		if item.Path == want && item.Description != "" {
			return true
		}
	}
	return false
}

func hasCatalogField(items []assetdefs.CatalogField, want string) bool {
	for _, item := range items {
		if item.Path == want {
			return true
		}
	}
	return false
}

func resetGuardianAutoStoreHooks(t *testing.T) {
	t.Helper()
	oldURL := guardianDiscoveryURL
	oldClient := guardianDiscoveryHTTPClient
	oldOpener := guardianStoreOpener
	t.Cleanup(func() {
		guardianDiscoveryURL = oldURL
		guardianDiscoveryHTTPClient = oldClient
		guardianStoreOpener = oldOpener
	})
}

func clearGuardianConnectionEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GUARDIAN_STORE_DIR",
		"GUARDIAN_MONOFS_ROUTER",
		"GUARDIAN_MONOFS_TOKEN",
		"GUARDIAN_MONOFS_PRINCIPAL",
		"GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES",
		"GUARDIAN_URL",
		"GUARDIAN_API_URL",
		"GUARDIAN_DISCOVERY_TOKEN",
		"GUARDIAN_CLIENT_DISCOVERY_TOKEN",
	} {
		t.Setenv(key, "")
	}
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("Encode(response) error = %v", err)
	}
}
