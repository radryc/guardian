package historyquery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestPrepareArchiveIndexContentBootstrapsExistingArchives(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	oldRecord := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_old",
		Partition:          "demo",
		Intent:             "api",
		CreatedAt:          time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
	}
	newRecord := historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: "dep_new",
		Partition:          "demo",
		Intent:             "api",
		CreatedAt:          time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	}
	seedJSON(t, ctx, store, paths.ArchiveState("demo", "api", oldRecord.DeploymentRevision), oldRecord)

	content, err := PrepareArchiveIndexContent(ctx, store, newRecord)
	if err != nil {
		t.Fatalf("PrepareArchiveIndexContent() error = %v", err)
	}

	var index DeploymentIndex
	if err := json.Unmarshal(content, &index); err != nil {
		t.Fatalf("Unmarshal(index) error = %v", err)
	}
	if got, want := len(index.Records), 2; got != want {
		t.Fatalf("record count = %d, want %d", got, want)
	}
	if got, want := index.Records[0].DeploymentRevision, "dep_new"; got != want {
		t.Fatalf("latest deployment = %q, want %q", got, want)
	}
}

func TestLoadDeploymentRecordsPrefersIndex(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	index := DeploymentIndex{
		APIVersion: "guardian/v1alpha1",
		Kind:       "DeploymentIndex",
		Partition:  "demo",
		Intent:     "api",
		Records: []historydomain.DeploymentRecord{
			{
				APIVersion:         "guardian/v1alpha1",
				Kind:               "DeploymentRecord",
				DeploymentRevision: "dep_new",
				Partition:          "demo",
				Intent:             "api",
				CreatedAt:          time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
			},
			{
				APIVersion:         "guardian/v1alpha1",
				Kind:               "DeploymentRecord",
				DeploymentRevision: "dep_old",
				Partition:          "demo",
				Intent:             "api",
				CreatedAt:          time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	seedJSON(t, ctx, store, paths.ArchiveIndex("demo", "api"), index)

	records, err := LoadDeploymentRecords(ctx, store, "demo", "api", DeploymentFilter{
		Limit: 1,
		Since: timePointer(time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("LoadDeploymentRecords() error = %v", err)
	}
	if got, want := len(records), 1; got != want {
		t.Fatalf("record count = %d, want %d", got, want)
	}
	if got, want := records[0].DeploymentRevision, "dep_new"; got != want {
		t.Fatalf("deployment revision = %q, want %q", got, want)
	}
}

func seedJSON(t *testing.T, ctx context.Context, store *memory.Store, logicalPath string, value any) {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal(seed) error = %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: content}},
		Context: guardianapi.MutationContext{
			PrincipalID: "test",
			Reason:      "seed historyquery test",
		},
	}); err != nil {
		t.Fatalf("UpsertFiles(seed) error = %v", err)
	}
}

func timePointer(v time.Time) *time.Time {
	return &v
}
