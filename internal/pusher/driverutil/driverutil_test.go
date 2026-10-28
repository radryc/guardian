package driverutil

import (
	"testing"

	targetdomain "github.com/radryc/guardian/internal/domain/target"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/registry"
)

func TestLimitLabelValueTruncatesOverlongValues(t *testing.T) {
	value := "1234567890123456789012345678901234567890123456789012345678901234"
	got := limitLabelValue(value)
	if len(got) != 63 {
		t.Fatalf("limitLabelValue() len = %d, want 63", len(got))
	}
	if got != value[:63] {
		t.Fatalf("limitLabelValue() = %q, want %q", got, value[:63])
	}
}

func TestLimitLabelValueKeepsShortValues(t *testing.T) {
	value := "short-value"
	if got := limitLabelValue(value); got != value {
		t.Fatalf("limitLabelValue() = %q, want %q", got, value)
	}
}

func TestLabelsOmitsEmptyCluster(t *testing.T) {
	in := registry.AssetInput{
		PartitionName: "demo-deploy",
		IntentName:    "web",
		Asset:         taskdomain.AbstractAsset{Name: "api", Type: "Compute"},
		Target:        targetdomain.Placement{Account: "807359647816", Region: "us-east-1"},
	}
	labels := Labels("aws", in, "hash")
	if _, ok := labels["guardian.cluster"]; ok {
		t.Fatalf("Labels() must omit an empty cluster tag, got %v", labels)
	}
	if labels["guardian.partition"] != "demo-deploy" || labels["guardian.hash"] != "hash" {
		t.Fatalf("Labels() = %v", labels)
	}

	in.Target.Cluster = "prod-cluster"
	labels = Labels("aws", in, "hash")
	if labels["guardian.cluster"] != "prod-cluster" {
		t.Fatalf("Labels() cluster = %q", labels["guardian.cluster"])
	}
}
