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
	}

	for _, tc := range cases {
		t.Run(tc.logical, func(t *testing.T) {
			if got := mapLogicalToPhysical(tc.logical); got != tc.physical {
				t.Fatalf("mapLogicalToPhysical(%q) = %q, want %q", tc.logical, got, tc.physical)
			}
			if got := mapPhysicalToLogical(tc.physical); got != tc.logical {
				t.Fatalf("mapPhysicalToLogical(%q) = %q, want %q", tc.physical, got, tc.logical)
			}
		})
	}
}
