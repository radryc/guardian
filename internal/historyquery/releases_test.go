package historyquery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	releasedomain "github.com/rydzu/ainfra/guardian/internal/domain/release"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func seedReleaseLog(t *testing.T, ctx context.Context, store *memory.Store, partition string, log *releasedomain.PartitionReleaseLog) {
	t.Helper()
	content, err := json.Marshal(log)
	if err != nil {
		t.Fatalf("Marshal(release log) error = %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.PartitionReleaseLog(partition), Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed release log"},
	}); err != nil {
		t.Fatalf("UpsertFiles(release log) error = %v", err)
	}
}

func TestLoadPartitionReleaseLogReadsFromStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := memory.New()
	createdAt := time.Date(2026, 7, 16, 12, 57, 0, 0, time.UTC)
	seedReleaseLog(t, ctx, store, "lolipop", &releasedomain.PartitionReleaseLog{
		APIVersion: "guardian/v1alpha1",
		Kind:       "PartitionReleaseLog",
		Partition:  "lolipop",
		UpdatedAt:  createdAt,
		Releases: map[string]releasedomain.ReleaseRecord{
			"lolipop-backend": {
				APIVersion:         "guardian/v1alpha1",
				Kind:               "ReleaseRecord",
				DeploymentRevision: "dep_abc",
				Partition:          "lolipop",
				Intent:             "lolipop-backend",
				AssetVersions:      map[string]string{"lolipop-backend": "release-hash-20260716-1257"},
				CreatedAt:          createdAt,
			},
		},
	})

	log, err := LoadPartitionReleaseLog(ctx, store, "lolipop")
	if err != nil {
		t.Fatalf("LoadPartitionReleaseLog() error = %v", err)
	}
	if log.Partition != "lolipop" {
		t.Fatalf("partition = %q, want %q", log.Partition, "lolipop")
	}
	if len(log.Releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(log.Releases))
	}
}

func TestLoadLatestReleaseReturnsErrReleaseNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := memory.New()
	seedReleaseLog(t, ctx, store, "lolipop", &releasedomain.PartitionReleaseLog{
		APIVersion: "guardian/v1alpha1",
		Kind:       "PartitionReleaseLog",
		Partition:  "lolipop",
		Releases: map[string]releasedomain.ReleaseRecord{
			"different-intent": {Intent: "different-intent", DeploymentRevision: "dep_xx"},
		},
	})

	_, err := LoadLatestRelease(ctx, store, "lolipop", "lolipop-backend")
	if !IsReleaseNotFound(err) {
		t.Fatalf("expected IsReleaseNotFound=true, got err=%v", err)
	}
}

func TestLoadLatestReleaseErrsWhenLogMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := memory.New()
	_, err := LoadLatestRelease(ctx, store, "lolipop", "lolipop-backend")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got err=%v", err)
	}
}

func TestLoadLatestDeploymentsForPartitionEmptyWhenLogMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := memory.New()
	out, err := LoadLatestDeploymentsForPartition(ctx, store, "lolipop")
	if err != nil {
		t.Fatalf("LoadLatestDeploymentsForPartition() error = %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty map, got %v", out)
	}
}

func TestLoadDeploymentRecordsFastPathUsesReleaseLog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := memory.New()
	createdAt := time.Date(2026, 7, 16, 12, 57, 0, 0, time.UTC)
	seedReleaseLog(t, ctx, store, "lolipop", &releasedomain.PartitionReleaseLog{
		APIVersion: "guardian/v1alpha1",
		Kind:       "PartitionReleaseLog",
		Partition:  "lolipop",
		UpdatedAt:  createdAt,
		Releases: map[string]releasedomain.ReleaseRecord{
			"lolipop-backend": {
				APIVersion:         "guardian/v1alpha1",
				Kind:               "ReleaseRecord",
				DeploymentRevision: "dep_fast",
				Partition:          "lolipop",
				Intent:             "lolipop-backend",
				AssetVersionIDs:    map[string]string{"lolipop-backend": "asset-v"},
				AssetVersions:      map[string]string{"lolipop-backend": "release-v-20260716-1257"},
				Outputs:            map[string]string{"k": "v"},
				TaskIDs:            []string{"task-fast"},
				CreatedAt:          createdAt,
			},
		},
	})

	records, err := LoadDeploymentRecords(ctx, store, "lolipop", "lolipop-backend", DeploymentFilter{Limit: 1})
	if err != nil {
		t.Fatalf("LoadDeploymentRecords(limit=1) error = %v", err)
	}
	if got, want := len(records), 1; got != want {
		t.Fatalf("record count = %d, want %d", got, want)
	}
	if records[0].DeploymentRevision != "dep_fast" {
		t.Fatalf("expected dep_fast from release log fast path, got %q", records[0].DeploymentRevision)
	}
	if records[0].AssetVersions["lolipop-backend"] != "release-v-20260716-1257" {
		t.Fatalf("AssetVersions did not round-trip: %v", records[0].AssetVersions)
	}
}

func TestLoadDeploymentRecordsFallsBackWhenLogMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := memory.New()
	createdAt := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	// No release log; archive directory scan should pick up the legacy record.
	oldRecord := seedLegacyArchiveRecord(t, ctx, store, "lolipop", "lolipop-backend", "dep_legacy", createdAt)

	records, err := LoadDeploymentRecords(ctx, store, "lolipop", "lolipop-backend", DeploymentFilter{Limit: 1})
	if err != nil {
		t.Fatalf("LoadDeploymentRecords fallback error = %v", err)
	}
	if got, want := len(records), 1; got != want {
		t.Fatalf("record count = %d, want %d", got, want)
	}
	if records[0].DeploymentRevision != oldRecord {
		t.Fatalf("expected legacy archive revision %q, got %q", oldRecord, records[0].DeploymentRevision)
	}
}

func seedLegacyArchiveRecord(t *testing.T, ctx context.Context, store *memory.Store, partition, intent, deploymentRevision string, createdAt time.Time) string {
	t.Helper()
	record := map[string]any{
		"apiVersion":         "guardian/v1alpha1",
		"kind":               "DeploymentRecord",
		"deploymentRevision": deploymentRevision,
		"partition":          partition,
		"intent":             intent,
		"assetVersionIDs":    map[string]string{},
		"assetVersions":      map[string]string{},
		"taskIDs":            []string{},
		"outputs":            map[string]string{},
		"createdAt":          createdAt,
	}
	content, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal(legacy record) error = %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.ArchiveState(partition, intent, deploymentRevision), Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed archive state"},
	}); err != nil {
		t.Fatalf("UpsertFiles(archive state) error = %v", err)
	}
	return deploymentRevision
}
