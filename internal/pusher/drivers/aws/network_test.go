package awsdriver

import (
	"context"
	"testing"

	targetdomain "github.com/radryc/guardian/internal/domain/target"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/store/memory"
)

func TestAWSNetworkDriverApplyDiffDestroy(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	driver := &NetworkDriver{baseDriver{backend: backend}}
	in := registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "stack",
		Asset: taskdomain.AbstractAsset{
			Type: "Network",
			Name: "backend",
			Properties: map[string]any{
				"cidr":  "10.42.0.0/16",
				"scope": "partition",
			},
		},
		Target: targetdomain.Placement{Account: "123456789012", Region: "eu-west-1"},
		Store:  store,
	}

	result, err := driver.Apply(ctx, in)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Outputs["vpcId"] != "vpc-test" || result.Outputs["cidr"] != "10.42.0.0/16" {
		t.Fatalf("outputs = %+v", result.Outputs)
	}

	// The control plane snapshots prior outputs into the task; Diff/Destroy
	// consume that snapshot instead of re-reading live runtime state.
	in.Outputs = map[string]string{"backend.vpcId": "vpc-test"}

	report, err := driver.Diff(ctx, in)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("Diff() = %+v, want InSync", report)
	}

	if err := driver.Destroy(ctx, in); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if _, ok, _ := backend.GetVPC(ctx, "vpc-test"); ok {
		t.Fatalf("VPC still present after destroy")
	}
}
