package registry

import "testing"

func TestRepoBindingPaths(t *testing.T) {
	binding := RepoBinding{
		Partition:     "doctor",
		PartitionPath: "/partitions/doctor",
		SourcePath:    "/src/doctor",
	}
	if got := binding.PartitionDir(); got != "partitions/doctor" {
		t.Fatalf("PartitionDir() = %q", got)
	}
	if got := binding.SourceDir(); got != "/src/doctor" {
		t.Fatalf("SourceDir() = %q", got)
	}
	if got := binding.PayloadDir(); got != "/partitions/doctor/payloads/aws" {
		t.Fatalf("PayloadDir() = %q", got)
	}

	defaults := RepoBinding{Partition: "doctor"}
	if got := defaults.PartitionDir(); got != "guardian/partitions/doctor" {
		t.Fatalf("default PartitionDir() = %q", got)
	}
	if got := defaults.SourceDir(); got != "/partitions/doctor/payloads/aws" {
		t.Fatalf("default SourceDir() = %q", got)
	}
}
