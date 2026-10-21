package probes

import (
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/onboarding"
)

func TestTargetsFromOnboarding(t *testing.T) {
	retries := 2
	descriptors := []onboarding.Descriptor{{
		Stack:     "CustomerAppStack",
		Partition: "customer-prod-1",
		Intent:    "app",
		Probes: []onboarding.Probe{
			{
				Name:         "health",
				URL:          "https://api.example.com/healthz",
				Type:         "https",
				ExpectStatus: []int{200},
				Retries:      &retries,
			},
			{
				Name:    "db",
				Address: "db.internal",
				Port:    5432,
				Type:    "tcp",
			},
		},
	}}
	targets := TargetsFromOnboarding("aws-123", descriptors)
	if len(targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(targets))
	}
	if targets[0].Partition != "customer-prod-1" || targets[0].Intent != "app" {
		t.Fatalf("target identity = %+v", targets[0])
	}
	if targets[0].Asset != "CustomerAppStack" || targets[0].Service == "" {
		t.Fatalf("target asset/service = %+v", targets[0])
	}
	if targets[0].ID == "" || targets[0].Retries != 2 {
		t.Fatalf("target id/retries = %+v", targets[0])
	}
	if targets[1].Type != "tcp" || targets[1].Port != 5432 {
		t.Fatalf("tcp target = %+v", targets[1])
	}
}

func TestTargetsFromOnboardingSkipsIncomplete(t *testing.T) {
	targets := TargetsFromOnboarding("aws-123", []onboarding.Descriptor{{
		Stack:     "S",
		Partition: "p",
		Intent:    "i",
		Probes:    []onboarding.Probe{{Name: "empty"}},
	}})
	if len(targets) != 0 {
		t.Fatalf("targets = %d, want 0", len(targets))
	}
}
