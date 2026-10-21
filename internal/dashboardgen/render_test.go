package dashboardgen

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderIncludesPartitionSignals(t *testing.T) {
	raw, err := Render(Info{
		Partition: "customer-prod-1",
		Account:   "123456789012",
		Region:    "eu-west-1",
		Stacks:    []string{"CustomerAppStack"},
		LogGroup:  "/aws/ecs/customer-app",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	var dashboard map[string]any
	if err := json.Unmarshal(raw, &dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if dashboard["uid"] != "partition-customer-prod-1" {
		t.Fatalf("uid = %v", dashboard["uid"])
	}
	serialized := string(raw)
	for _, want := range []string{
		"guardian_probe_up",
		"@resource.guardian.partition",
		"/aws/ecs/customer-app",
		"CustomerAppStack",
		"doctor-logs",
		"doctor-traces",
	} {
		if !strings.Contains(serialized, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
}

func TestRenderRequiresPartition(t *testing.T) {
	if _, err := Render(Info{}); err == nil {
		t.Fatalf("expected partition error")
	}
}
