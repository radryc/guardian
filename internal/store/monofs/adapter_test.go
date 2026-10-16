//go:build monofs

package monofs

import "testing"

func TestPathMappingRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		logical  string
		physical string
	}{
		{
			logical:  "/partitions",
			physical: "guardian",
		},
		{
			logical:  "/partitions/payments/partition.yaml",
			physical: "guardian/payments/partition.yaml",
		},
		{
			logical:  "/partitions/payments/intents/api.yaml",
			physical: "guardian/payments/intents/api.yaml",
		},
		{
			logical:  "/.queues",
			physical: "guardian-system/.queues",
		},
		{
			logical:  "/.queues/local/tasks/task-1.json",
			physical: "guardian-system/.queues/local/tasks/task-1.json",
		},
		{
			logical:  "/.archive",
			physical: "guardian-system/.archive",
		},
		{
			logical:  "/.archive/payments/api/rev-1/record.json",
			physical: "guardian-system/.archive/payments/api/rev-1/record.json",
		},
		{
			logical:  "/.scans",
			physical: "guardian-system/.scans",
		},
		{
			logical:  "/.scans/aws-123456789012/requests/scan-1.json",
			physical: "guardian-system/.scans/aws-123456789012/requests/scan-1.json",
		},
		{
			logical:  "/.registry/repos/strata-aws.json",
			physical: "guardian-system/.registry/repos/strata-aws.json",
		},
		{
			logical:  "/.templates/aws/v1/network/template.yaml",
			physical: "guardian-system/.templates/aws/v1/network/template.yaml",
		},
		{
			logical:  "/.repos/github.com/example/strata/guardian/partitions/prod/partition.yaml",
			physical: "github.com/example/strata/guardian/partitions/prod/partition.yaml",
		},
	}

	for _, tc := range cases {
		t.Run(tc.logical, func(t *testing.T) {
			got, err := mapLogicalToPhysicalChecked(tc.logical)
			if err != nil {
				t.Fatalf("mapLogicalToPhysicalChecked(%q) error = %v", tc.logical, err)
			}
			if got != tc.physical {
				t.Fatalf("mapLogicalToPhysicalChecked(%q) = %q, want %q", tc.logical, got, tc.physical)
			}
		})
	}
}

func TestRepoMappingRejectsTraversal(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/.repos/", "/.repos/../secrets", "/unknown/path"} {
		if _, err := mapLogicalToPhysicalChecked(path); err == nil {
			t.Fatalf("expected error for %q", path)
		}
	}
}
