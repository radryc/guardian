package k8sobserve

import (
	"testing"
)

func TestGroupSplitsResourcesByPartitionIntent(t *testing.T) {
	t.Parallel()

	resources := []Resource{
		{Partition: "doctor", Intent: "controller", Namespace: "doctor", Kind: "Deployment", Name: "controller"},
		{Partition: "doctor", Intent: "controller", Namespace: "doctor", Kind: "Service", Name: "controller"},
		{Partition: "diva-shared", Intent: "k8s", Namespace: "div-demo", Kind: "Deployment", Name: "client"},
	}
	groups := Group(resources, "k8s-main")
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if groups[0].Partition != "diva-shared" || groups[0].Intent != "k8s" {
		t.Fatalf("first group = %+v", groups[0])
	}
	if groups[1].Partition != "doctor" || len(groups[1].Resources) != 2 {
		t.Fatalf("second group = %+v", groups[1])
	}
	if groups[1].Resources[0].Kind != "Deployment" || groups[1].Resources[1].Kind != "Service" {
		t.Fatalf("resources not sorted: %+v", groups[1].Resources)
	}
}
