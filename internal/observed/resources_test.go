package observed

import (
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/cdktemplate"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
)

func TestBuildResourceObservationsMarksStatuses(t *testing.T) {
	desired := []cdktemplate.Resource{
		{LogicalID: "Bucket", Type: "AWS::S3::Bucket"},
		{LogicalID: "OrdersTable", Type: "AWS::DynamoDB::Table", DependsOn: []string{"Bucket"}},
	}
	live := []cdktemplate.Resource{
		{LogicalID: "Bucket", Type: "AWS::S3::Bucket"},
		{LogicalID: "OldQueue", Type: "AWS::SQS::Queue"},
	}
	resources := buildResourceObservations(desired, live, []string{
		"changed:OrdersTable",
		"removed:OldQueue",
	})

	if len(resources) != 3 {
		t.Fatalf("resources = %+v, want 3", resources)
	}
	// Sorted by logical ID: Bucket, OldQueue, OrdersTable.
	byID := map[string]statedomain.ResourceObservation{}
	for _, resource := range resources {
		byID[resource.LogicalID] = resource
	}
	if got := byID["Bucket"]; got.Status != statedomain.ResourceInSync || got.AssetName != "res-bucket" {
		t.Fatalf("bucket = %+v", got)
	}
	if got := byID["OldQueue"]; got.Status != statedomain.ResourceRemoved || got.AssetName != "res-old-queue" {
		t.Fatalf("old queue = %+v", got)
	}
	if got := byID["OrdersTable"]; got.Status != statedomain.ResourceChanged || got.AssetName != "res-orders-table" {
		t.Fatalf("table = %+v", got)
	}
	if got := byID["OrdersTable"].DependsOn; len(got) != 1 || got[0] != "Bucket" {
		t.Fatalf("table dependsOn = %v", got)
	}
}

func TestBuildResourceObservationsAddsNewResources(t *testing.T) {
	resources := buildResourceObservations(
		[]cdktemplate.Resource{{LogicalID: "NewQueue", Type: "AWS::SQS::Queue"}},
		nil,
		[]string{"added:NewQueue"},
	)
	if len(resources) != 1 || resources[0].Status != statedomain.ResourceAdded {
		t.Fatalf("resources = %+v", resources)
	}
}

func TestAssignResourceAssetNamesDeduplicates(t *testing.T) {
	resources := []statedomain.ResourceObservation{
		{LogicalID: "FooBar"},
		{LogicalID: "Foo-Bar"},
		{LogicalID: "FooBar", Condition: "Duplicate"},
	}
	assignResourceAssetNames(resources)
	if resources[0].AssetName != "res-foo-bar" || resources[1].AssetName != "res-foo-bar-2" || resources[2].AssetName != "res-foo-bar-3" {
		t.Fatalf("names = %q, %q, %q", resources[0].AssetName, resources[1].AssetName, resources[2].AssetName)
	}
}

func TestParseChangedResources(t *testing.T) {
	got := parseChangedResources([]string{"changed:A", "added:B", "removed:C", "bogus", "changed:"})
	if len(got) != 3 {
		t.Fatalf("statuses = %+v", got)
	}
	if got["A"] != statedomain.ResourceChanged || got["B"] != statedomain.ResourceAdded || got["C"] != statedomain.ResourceRemoved {
		t.Fatalf("statuses = %+v", got)
	}
}
