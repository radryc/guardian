package historyquery

import (
	"context"
	"testing"
	"time"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

func TestLoadPartitionRolloutsDetectsInitialAndUpdatedAssets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()

	apiOld := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_api_old",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-config-v1"},
		AssetVersions:      map[string]string{"config": "config-v1"},
		CreatedAt:          time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
	}
	apiNew := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_api_new",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-config-v2", "binary": "asset-binary-v1"},
		AssetVersions:      map[string]string{"config": "config-v2", "binary": "binary-v1"},
		CreatedAt:          time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	}
	jobsInitial := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_jobs_initial",
		Partition:          "demo",
		Intent:             "jobs",
		AssetVersionIDs:    map[string]string{"jobs-config": "asset-jobs-config-v1"},
		AssetVersions:      map[string]string{"jobs-config": "jobs-config-v1"},
		CreatedAt:          time.Date(2026, 4, 29, 15, 0, 0, 0, time.UTC),
	}

	seedArchiveDeployment(t, ctx, store, apiOld, archivedIntent("api", assetdomain.Spec{Name: "config", Type: "Config", Version: "config-v1"}))
	seedArchiveDeployment(t, ctx, store, apiNew, archivedIntent("api", assetdomain.Spec{Name: "config", Type: "Config", Version: "config-v2"}, assetdomain.Spec{Name: "binary", Type: "Compute", Version: "binary-v1"}))
	seedArchiveDeployment(t, ctx, store, jobsInitial, archivedIntent("jobs", assetdomain.Spec{Name: "jobs-config", Type: "Config", Version: "jobs-config-v1"}))

	rollouts, err := LoadPartitionRollouts(ctx, store, "demo", DeploymentFilter{})
	if err != nil {
		t.Fatalf("LoadPartitionRollouts() error = %v", err)
	}
	if got, want := len(rollouts), 3; got != want {
		t.Fatalf("rollout count = %d, want %d", got, want)
	}

	if got, want := rollouts[0].DeploymentRevision, "dep_api_new"; got != want {
		t.Fatalf("latest rollout deployment = %q, want %q", got, want)
	}
	if rollouts[0].NewIntent {
		t.Fatalf("expected updated rollout for %s to not be marked new", rollouts[0].Intent)
	}
	if got, want := rollouts[0].Summary, "Rollout: 1 asset added, 1 asset updated"; got != want {
		t.Fatalf("updated rollout summary = %q, want %q", got, want)
	}
	if got, want := len(rollouts[0].Assets), 2; got != want {
		t.Fatalf("updated rollout asset count = %d, want %d", got, want)
	}
	if got, want := rollouts[0].Assets[0].Name, "binary"; got != want {
		t.Fatalf("first updated asset = %q, want %q", got, want)
	}
	if got, want := rollouts[0].Assets[0].Change, "added"; got != want {
		t.Fatalf("binary change = %q, want %q", got, want)
	}
	if got, want := rollouts[0].Assets[1].Name, "config"; got != want {
		t.Fatalf("second updated asset = %q, want %q", got, want)
	}
	if got, want := rollouts[0].Assets[1].Change, "updated"; got != want {
		t.Fatalf("config change = %q, want %q", got, want)
	}

	if got, want := rollouts[1].DeploymentRevision, "dep_jobs_initial"; got != want {
		t.Fatalf("second rollout deployment = %q, want %q", got, want)
	}
	if !rollouts[1].NewIntent {
		t.Fatalf("expected jobs rollout to be marked new")
	}
	if got, want := rollouts[1].Summary, "Initial rollout: 1 asset added"; got != want {
		t.Fatalf("initial rollout summary = %q, want %q", got, want)
	}
	if got, want := rollouts[1].Assets[0].Type, "Config"; got != want {
		t.Fatalf("initial rollout asset type = %q, want %q", got, want)
	}
}

func TestLoadPartitionRolloutsAppliesGlobalLimitAndTimeframe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()

	seedArchiveDeployment(t, ctx, store,
		historydomain.DeploymentRecord{
			APIVersion:         "guardian/v1alpha1",
			Kind:               "DeploymentRecord",
			DeploymentRevision: "dep_old",
			Partition:          "demo",
			Intent:             "api",
			AssetVersionIDs:    map[string]string{"config": "asset-v1"},
			CreatedAt:          time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		},
		archivedIntent("api", assetdomain.Spec{Name: "config", Type: "Config", Version: "v1"}),
	)
	seedArchiveDeployment(t, ctx, store,
		historydomain.DeploymentRecord{
			APIVersion:         "guardian/v1alpha1",
			Kind:               "DeploymentRecord",
			DeploymentRevision: "dep_newer",
			Partition:          "demo",
			Intent:             "api",
			AssetVersionIDs:    map[string]string{"config": "asset-v2"},
			CreatedAt:          time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		},
		archivedIntent("api", assetdomain.Spec{Name: "config", Type: "Config", Version: "v2"}),
	)
	seedArchiveDeployment(t, ctx, store,
		historydomain.DeploymentRecord{
			APIVersion:         "guardian/v1alpha1",
			Kind:               "DeploymentRecord",
			DeploymentRevision: "dep_latest",
			Partition:          "demo",
			Intent:             "jobs",
			AssetVersionIDs:    map[string]string{"jobs-config": "asset-jobs-v1"},
			CreatedAt:          time.Date(2026, 4, 30, 18, 0, 0, 0, time.UTC),
		},
		archivedIntent("jobs", assetdomain.Spec{Name: "jobs-config", Type: "Config", Version: "v1"}),
	)

	rollouts, err := LoadPartitionRollouts(ctx, store, "demo", DeploymentFilter{
		Limit: 1,
		Since: timePointer(time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("LoadPartitionRollouts() error = %v", err)
	}
	if got, want := len(rollouts), 1; got != want {
		t.Fatalf("rollout count = %d, want %d", got, want)
	}
	if got, want := rollouts[0].DeploymentRevision, "dep_latest"; got != want {
		t.Fatalf("limited rollout deployment = %q, want %q", got, want)
	}
}

func TestLoadPartitionRolloutsDefaultsReleaseToDeploymentRevision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()

	record := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_release_fallback",
		Partition:          "demo",
		Intent:             "api",
		CreatedAt:          time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	seedArchiveDeployment(t, ctx, store, record, archivedIntent("api", assetdomain.Spec{Name: "config", Type: "Config"}))

	rollouts, err := LoadPartitionRollouts(ctx, store, "demo", DeploymentFilter{})
	if err != nil {
		t.Fatalf("LoadPartitionRollouts() error = %v", err)
	}
	if got, want := len(rollouts), 1; got != want {
		t.Fatalf("rollout count = %d, want %d", got, want)
	}
	if got, want := rollouts[0].Assets[0].Version, record.DeploymentRevision; got != want {
		t.Fatalf("default rollout asset version = %q, want %q", got, want)
	}
}

func TestLoadPartitionRolloutsDefaultsReleaseToDerivedAssetHashLabel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()

	record := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_release_fallback",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset_8a5d002f265f4848"},
		CreatedAt:          time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	seedArchiveDeployment(t, ctx, store, record, archivedIntent("api", assetdomain.Spec{Name: "config", Type: "Config"}))

	rollouts, err := LoadPartitionRollouts(ctx, store, "demo", DeploymentFilter{})
	if err != nil {
		t.Fatalf("LoadPartitionRollouts() error = %v", err)
	}
	if got, want := len(rollouts), 1; got != want {
		t.Fatalf("rollout count = %d, want %d", got, want)
	}
	if got, want := rollouts[0].Assets[0].Version, revisions.DerivedAssetVersionAt(record.AssetVersionIDs["config"], record.CreatedAt); got != want {
		t.Fatalf("default rollout asset version = %q, want %q", got, want)
	}
}

func TestLoadPartitionRolloutsMarksEquivalentApplyAsSelfHeal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()

	initial := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_api_initial",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-config-v1"},
		AssetVersions:      map[string]string{"config": "config-v1"},
		ChangedAssets:      []string{"config"},
		CreatedAt:          time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	selfHeal := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_api_selfheal",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-config-v1"},
		AssetVersions:      map[string]string{"config": "config-v1"},
		ChangedAssets:      []string{"config"},
		CreatedAt:          time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC),
	}
	intent := archivedIntent("api", assetdomain.Spec{Name: "config", Type: "Config", Version: "config-v1"})
	seedArchiveDeployment(t, ctx, store, initial, intent)
	seedArchiveDeployment(t, ctx, store, selfHeal, intent)

	rollouts, err := LoadPartitionRollouts(ctx, store, "demo", DeploymentFilter{})
	if err != nil {
		t.Fatalf("LoadPartitionRollouts() error = %v", err)
	}
	if got, want := len(rollouts), 2; got != want {
		t.Fatalf("rollout count = %d, want %d", got, want)
	}
	if !rollouts[0].SelfHealing {
		t.Fatalf("expected latest deployment to be marked self-healing")
	}
	if got, want := rollouts[0].Summary, "Self-heal: 1 asset refreshed"; got != want {
		t.Fatalf("self-heal summary = %q, want %q", got, want)
	}
	if got, want := rollouts[0].Assets[0].Change, "refreshed"; got != want {
		t.Fatalf("self-heal asset change = %q, want %q", got, want)
	}
}

func TestLoadPartitionRolloutsMarksFirstVisibleAsCurrentWhenFiltered(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()

	oldRecord := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_old",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-config-v1"},
		AssetVersions:      map[string]string{"config": "config-v1"},
		CreatedAt:          time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	newRecord := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_new",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-config-v2"},
		AssetVersions:      map[string]string{"config": "config-v2"},
		CreatedAt:          time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC),
	}
	seedArchiveDeployment(t, ctx, store, oldRecord, archivedIntent("api", assetdomain.Spec{Name: "config", Type: "Config", Version: "config-v1"}))
	seedArchiveDeployment(t, ctx, store, newRecord, archivedIntent("api", assetdomain.Spec{Name: "config", Type: "Config", Version: "config-v2"}))

	until := time.Date(2026, 5, 1, 12, 30, 0, 0, time.UTC)
	rollouts, err := LoadPartitionRollouts(ctx, store, "demo", DeploymentFilter{Until: &until})
	if err != nil {
		t.Fatalf("LoadPartitionRollouts() error = %v", err)
	}
	if got, want := len(rollouts), 1; got != want {
		t.Fatalf("rollout count = %d, want %d", got, want)
	}
	if !rollouts[0].Current {
		t.Fatalf("expected first visible rollout to be marked current")
	}
}

func TestLoadPartitionRolloutsCollapsesEquivalentDeploymentsAndMarksCurrent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()

	initial := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_api_v1",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-config-v1"},
		AssetVersions:      map[string]string{"config": "config-v1"},
		CreatedAt:          time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	updated := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_api_v2",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-config-v2"},
		AssetVersions:      map[string]string{"config": "config-v2"},
		TaskIDs:            []string{"task-1"},
		CreatedAt:          time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC),
	}
	retry := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_api_v2_retry",
		Partition:          "demo",
		Intent:             "api",
		AssetVersionIDs:    map[string]string{"config": "asset-config-v2"},
		AssetVersions:      map[string]string{"config": "config-v2"},
		TaskIDs:            []string{"task-2"},
		CreatedAt:          time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC),
	}

	seedArchiveDeployment(t, ctx, store, initial, archivedIntent("api", assetdomain.Spec{Name: "config", Type: "Config", Version: "config-v1"}))
	seedArchiveDeployment(t, ctx, store, updated, archivedIntent("api", assetdomain.Spec{Name: "config", Type: "Config", Version: "config-v2"}))
	seedArchiveDeployment(t, ctx, store, retry, archivedIntent("api", assetdomain.Spec{Name: "config", Type: "Config", Version: "config-v2"}))

	rollouts, err := LoadPartitionRollouts(ctx, store, "demo", DeploymentFilter{})
	if err != nil {
		t.Fatalf("LoadPartitionRollouts() error = %v", err)
	}
	if got, want := len(rollouts), 2; got != want {
		t.Fatalf("rollout count = %d, want %d", got, want)
	}
	if got, want := rollouts[0].DeploymentRevision, retry.DeploymentRevision; got != want {
		t.Fatalf("current rollout deployment = %q, want %q", got, want)
	}
	if !rollouts[0].Current {
		t.Fatalf("expected latest distinct rollout to be marked current")
	}
	if rollouts[1].Current {
		t.Fatalf("expected older rollout to not be marked current")
	}
	if got, want := len(rollouts[0].Assets), 1; got != want {
		t.Fatalf("current rollout asset count = %d, want %d", got, want)
	}
	if got, want := rollouts[0].Assets[0].Version, "config-v2"; got != want {
		t.Fatalf("current rollout asset version = %q, want %q", got, want)
	}
}

func seedArchiveDeployment(t *testing.T, ctx context.Context, store *memory.Store, record historydomain.DeploymentRecord, intent intentdomain.Intent) {
	t.Helper()
	seedJSON(t, ctx, store, paths.ArchiveState(record.Partition, record.Intent, record.DeploymentRevision), record)
	content, err := yaml.Marshal(intent)
	if err != nil {
		t.Fatalf("Marshal(intent) error = %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: paths.ArchiveManifest(record.Partition, record.Intent, record.DeploymentRevision),
			Content:     content,
		}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed rollout test"},
	}); err != nil {
		t.Fatalf("UpsertFiles(archive manifest) error = %v", err)
	}
}

func archivedIntent(name string, assets ...assetdomain.Spec) intentdomain.Intent {
	return intentdomain.Intent{
		APIVersion: "guardian/v1alpha1",
		Kind:       "Intent",
		Metadata:   intentdomain.Metadata{Name: name},
		Spec: intentdomain.IntentSpec{
			TargetPusher: "local",
			Assets:       assets,
		},
	}
}
