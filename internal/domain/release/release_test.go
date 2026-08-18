package release

import (
	"testing"
	"time"
)

func TestCloneReleaseRecordIsDeep(t *testing.T) {
	t.Parallel()
	original := ReleaseRecord{
		AssetVersionIDs:    map[string]string{"backend": "asset-v1"},
		AssetVersions:      map[string]string{"backend": "release-v1-20260429"},
		Outputs:            map[string]string{"backend.id": "compute-1"},
		TaskIDs:            []string{"task-1", "task-2"},
		DeploymentRevision: "dep_abc",
		CreatedAt:          time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	}

	cloned := CloneReleaseRecord(original)
	cloned.AssetVersions["backend"] = "tampered"
	cloned.Outputs["backend.id"] = "tampered"
	cloned.TaskIDs[0] = "tampered"

	if original.AssetVersions["backend"] != "release-v1-20260429" {
		t.Fatalf("original AssetVersions was mutated: %v", original.AssetVersions)
	}
	if original.Outputs["backend.id"] != "compute-1" {
		t.Fatalf("original Outputs was mutated: %v", original.Outputs)
	}
	if original.TaskIDs[0] != "task-1" {
		t.Fatalf("original TaskIDs was mutated: %v", original.TaskIDs)
	}
}

func TestCloneReleaseRecordPtr(t *testing.T) {
	t.Parallel()
	src := &ReleaseRecord{
		AssetVersionIDs: map[string]string{"x": "asset-v"},
		Outputs:         map[string]string{"k": "v"},
		TaskIDs:         []string{"a"},
	}
	dst := CloneReleaseRecordPtr(src)
	if dst == nil {
		t.Fatalf("CloneReleaseRecordPtr returned nil")
	}
	dst.TaskIDs[0] = "b"
	if src.TaskIDs[0] != "a" {
		t.Fatalf("CloneReleaseRecordPtr did not deep-copy TaskIDs")
	}
	if CloneReleaseRecordPtr(nil) != nil {
		t.Fatalf("CloneReleaseRecordPtr(nil) must return nil")
	}
}

func TestClonePartitionReleaseLogIsDeep(t *testing.T) {
	t.Parallel()
	log := NewPartitionReleaseLog("payments")
	log.Releases["api"] = ReleaseRecord{
		DeploymentRevision: "dep_1",
		AssetVersions:      map[string]string{"backend": "v1"},
		TaskIDs:            []string{"task-1"},
	}
	cloned := ClonePartitionReleaseLog(log)
	cloned.Releases["api"].AssetVersions["backend"] = "tampered"
	cloned.Releases["api"].TaskIDs[0] = "tampered"

	if log.Releases["api"].AssetVersions["backend"] != "v1" {
		t.Fatalf("original release was mutated: %v", log.Releases)
	}
	if log.Releases["api"].TaskIDs[0] != "task-1" {
		t.Fatalf("original release TaskIDs was mutated: %v", log.Releases)
	}
	if ClonePartitionReleaseLog(nil) != nil {
		t.Fatalf("ClonePartitionReleaseLog(nil) must return nil")
	}
}

func TestNewPartitionReleaseLogHasEmptyMap(t *testing.T) {
	t.Parallel()
	log := NewPartitionReleaseLog("demo")
	if log.Releases == nil {
		t.Fatalf("NewPartitionReleaseLog must initialise Releases map")
	}
	if log.Kind != "PartitionReleaseLog" {
		t.Fatalf("unexpected Kind: %q", log.Kind)
	}
	if log.APIVersion != "guardian/v1alpha1" {
		t.Fatalf("unexpected APIVersion: %q", log.APIVersion)
	}
}
