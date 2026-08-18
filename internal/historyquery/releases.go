package historyquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	releasedomain "github.com/rydzu/ainfra/guardian/internal/domain/release"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

// LoadPartitionReleaseLog reads the authoritative per-partition release log.
// Returns os.ErrNotExist if the partition has no recorded releases yet (for
// example, a brand-new partition that has never had a successful apply).
func LoadPartitionReleaseLog(ctx context.Context, store guardianapi.ReadStore, partition string) (*releasedomain.PartitionReleaseLog, error) {
	if partition == "" {
		return nil, fmt.Errorf("historyquery: partition is required")
	}
	raw, err := store.ReadFile(ctx, paths.PartitionReleaseLog(partition))
	if err != nil {
		return nil, err
	}
	var log releasedomain.PartitionReleaseLog
	if err := json.Unmarshal(raw, &log); err != nil {
		return nil, fmt.Errorf("historyquery: decode release log %s: %w", paths.PartitionReleaseLog(partition), err)
	}
	if log.Releases == nil {
		log.Releases = map[string]releasedomain.ReleaseRecord{}
	}
	if log.Partition == "" {
		log.Partition = partition
	}
	return releasedomain.ClonePartitionReleaseLog(&log), nil
}

// LoadLatestRelease returns the authoritative latest release for an intent.
// Returns os.ErrNotExist when the partition has no release log or the intent
// has no recorded apply. Callers that want a "current view" of the intent
// should fall back to the archive scan in this case so old partitions without
// a release log still render.
func LoadLatestRelease(ctx context.Context, store guardianapi.ReadStore, partition, intent string) (*releasedomain.ReleaseRecord, error) {
	log, err := LoadPartitionReleaseLog(ctx, store, partition)
	if err != nil {
		return nil, err
	}
	rec, ok := log.Releases[intent]
	if !ok {
		return nil, errReleaseNotFound{partition: partition, intent: intent}
	}
	return releasedomain.CloneReleaseRecordPtr(&rec), nil
}

// LoadLatestDeploymentsForPartition returns the latest release per intent for
// a partition. Missing intents are simply absent from the result. Used by the
// UI one-shot reads and by the reconciler recovery probe.
func LoadLatestDeploymentsForPartition(ctx context.Context, store guardianapi.ReadStore, partition string) (map[string]*releasedomain.ReleaseRecord, error) {
	log, err := LoadPartitionReleaseLog(ctx, store, partition)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]*releasedomain.ReleaseRecord{}, nil
		}
		return nil, err
	}
	out := make(map[string]*releasedomain.ReleaseRecord, len(log.Releases))
	for k, v := range log.Releases {
		rec := v
		out[k] = releasedomain.CloneReleaseRecordPtr(&rec)
	}
	return out, nil
}

// errReleaseNotFound is an internal helper so callers can choose to fall back
// (latestDeploymentsByIntent) or surface the error (strict history calls).
type errReleaseNotFound struct {
	partition string
	intent    string
}

func (e errReleaseNotFound) Error() string {
	return fmt.Sprintf("historyquery: no release recorded for %s/%s", e.partition, e.intent)
}

// IsReleaseNotFound reports whether err originated from LoadLatestRelease when
// no entry exists for the requested intent.
func IsReleaseNotFound(err error) bool {
	var target errReleaseNotFound
	return errors.As(err, &target)
}

// ReleaseRecordToDeploymentRecord converts a stored release into the
// DeploymentRecord shape that the UI has historically consumed from the
// archive directory. AssetVersionIDs / AssetVersions / Outputs / TaskIDs flow
// through unchanged so the UI surface is indistinguishable from the legacy
// archive-scan path.
func ReleaseRecordToDeploymentRecord(rec *releasedomain.ReleaseRecord) historydomain.DeploymentRecord {
	if rec == nil {
		return historydomain.DeploymentRecord{}
	}
	return historydomain.DeploymentRecord{
		APIVersion:         "guardian/v1alpha1",
		Kind:               "DeploymentRecord",
		DeploymentRevision: rec.DeploymentRevision,
		Partition:          rec.Partition,
		Intent:             rec.Intent,
		PartitionRevision:  rec.PartitionRevision,
		IntentVersionID:    rec.IntentVersionID,
		AssetVersionIDs:    rec.AssetVersionIDs,
		AssetVersions:      rec.AssetVersions,
		TaskIDs:            rec.TaskIDs,
		SelfHealing:        rec.SelfHealing,
		Rollback:           rec.Rollback,
		RollbackTo:         rec.RollbackTo,
		RollbackReason:     rec.RollbackReason,
		Outputs:            rec.Outputs,
		CreatedAt:          rec.CreatedAt,
	}
}

// AsOf returns the CreatedAt value of the latest release for an intent, or the
// zero time when the release log is missing/empty. Cheap helper used in
// diagnostics and recovery probes.
func AsOf(ctx context.Context, store guardianapi.ReadStore, partition, intent string) time.Time {
	rec, err := LoadLatestRelease(ctx, store, partition, intent)
	if err != nil || rec == nil {
		return time.Time{}
	}
	return rec.CreatedAt
}
