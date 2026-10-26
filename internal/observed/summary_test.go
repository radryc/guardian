package observed

import (
	"strings"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/awsobserve"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
)

func TestSummarizeResourceDrift(t *testing.T) {
	resources := []statedomain.ResourceObservation{
		{LogicalID: "OrdersBucket", CfnType: "AWS::S3::Bucket", Status: statedomain.ResourceChanged},
		{LogicalID: "OrdersQueue", CfnType: "AWS::SQS::Queue", Status: statedomain.ResourceInSync},
	}
	drifted := []awsobserve.DriftedResource{
		{LogicalID: "OrdersRole", ResourceType: "AWS::IAM::Role", Status: "MODIFIED"},
		{LogicalID: "OrdersBucket", ResourceType: "AWS::S3::Bucket", Status: "MODIFIED"},
	}
	got := summarizeResourceDrift(resources, drifted)
	for _, want := range []string{
		"OrdersBucket (AWS::S3::Bucket, Changed)",
		"OrdersRole (AWS::IAM::Role, MODIFIED)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "OrdersQueue") {
		t.Fatalf("in-sync resource leaked into summary: %q", got)
	}
	if strings.Count(got, "OrdersBucket") != 1 {
		t.Fatalf("duplicate resource in summary: %q", got)
	}
}

func TestSummarizeResourceDriftTruncates(t *testing.T) {
	resources := make([]statedomain.ResourceObservation, 0, 20)
	for i := 0; i < 20; i++ {
		resources = append(resources, statedomain.ResourceObservation{
			LogicalID: "Resource" + string(rune('A'+i)),
			CfnType:   "AWS::S3::Bucket",
			Status:    statedomain.ResourceChanged,
		})
	}
	got := summarizeResourceDrift(resources, nil)
	if !strings.Contains(got, "+8 more") {
		t.Fatalf("truncated summary = %q, want +8 more", got)
	}
}
