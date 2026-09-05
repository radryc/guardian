// Package release holds the canonical Release domain object for Guardian.
//
// A Release is a first-class projection of a successful apply for a single
// intent within a partition. The release log (PartitionReleaseLog) is the
// authoritative pointer that answers "what is the latest release for intent X
// in partition P?". Per-deployment manifest and log artifacts remain under
// /.archive/<partition>/<intent>/<deployment_revision>/ for historical detail.
package release

import (
	"time"
)

// ReleaseRecord captures the durable outcome of one successful apply for a
// single intent. It carries every field the UI needs to render the asset
// cards on a partition page (assetVersionIDs, assetVersions, outputs, taskIDs)
// without forcing the UI to walk the archive directory.
type ReleaseRecord struct {
	APIVersion         string            `json:"apiVersion"`
	Kind               string            `json:"kind"`
	DeploymentRevision string            `json:"deploymentRevision"`
	Partition          string            `json:"partition"`
	Intent             string            `json:"intent"`
	PartitionRevision  string            `json:"partitionRevision"`
	IntentVersionID    string            `json:"intentVersionID"`
	AssetVersionIDs    map[string]string `json:"assetVersionIDs"`
	AssetVersions      map[string]string `json:"assetVersions"`
	TaskIDs            []string          `json:"taskIDs"`
	SelfHealing        bool              `json:"selfHealing,omitempty"`
	Rollback           bool              `json:"rollback,omitempty"`
	RollbackTo         string            `json:"rollbackTo,omitempty"`
	RollbackReason     string            `json:"rollbackReason,omitempty"`
	Outputs            map[string]string `json:"outputs"`
	CreatedAt          time.Time         `json:"createdAt"`
}

// PartitionReleaseLog is the per-partition authoritative pointer for "the
// latest release of each intent". It is persisted at
// /partitions/<partition>/.state/releases.json.
//
// Writes are serialised by Dispatcher.lockPartitionWrites, so concurrent
// writes from a single reconciler pool cannot interleave. Each intent has at
// most one ReleaseRecord; older writes for the same intent are superseded.
// CreatedAt is monotonically non-decreasing per intent (Guardian queues
// applies serially per intent), so a simple last-writer-wins policy
// (CreatedAt-as-clock) is correct without a CRDT merge.
type PartitionReleaseLog struct {
	APIVersion string                   `json:"apiVersion"`
	Kind       string                   `json:"kind"`
	Partition  string                   `json:"partition"`
	Releases   map[string]ReleaseRecord `json:"releases"`
	UpdatedAt  time.Time                `json:"updatedAt"`
}

// NewPartitionReleaseLog returns an empty log for the given partition.
func NewPartitionReleaseLog(partition string) *PartitionReleaseLog {
	return &PartitionReleaseLog{
		APIVersion: "guardian/v1alpha1",
		Kind:       "PartitionReleaseLog",
		Partition:  partition,
		Releases:   map[string]ReleaseRecord{},
	}
}

// CloneReleaseRecord returns a deep copy of the record so callers can mutate
// without affecting cached state.
func CloneReleaseRecord(in ReleaseRecord) ReleaseRecord {
	out := in
	out.AssetVersionIDs = cloneStringMap(in.AssetVersionIDs)
	out.AssetVersions = cloneStringMap(in.AssetVersions)
	out.Outputs = cloneStringMap(in.Outputs)
	if in.TaskIDs != nil {
		out.TaskIDs = append([]string(nil), in.TaskIDs...)
	}
	return out
}

// CloneReleaseRecordPtr returns a *ReleaseRecord with the same deep-copy
// semantics as CloneReleaseRecord. Useful for call sites that need a pointer
// (e.g. map value types stored in caches).
func CloneReleaseRecordPtr(in *ReleaseRecord) *ReleaseRecord {
	if in == nil {
		return nil
	}
	out := CloneReleaseRecord(*in)
	return &out
}

// ClonePartitionReleaseLog returns a deep copy of the log so callers can
// mutate without affecting cached state.
func ClonePartitionReleaseLog(in *PartitionReleaseLog) *PartitionReleaseLog {
	if in == nil {
		return nil
	}
	out := *in
	out.Releases = make(map[string]ReleaseRecord, len(in.Releases))
	for k, v := range in.Releases {
		out.Releases[k] = CloneReleaseRecord(v)
	}
	return &out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
