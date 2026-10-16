package kubernetesdriver

import (
	"context"
	"testing"

	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
)

func TestKubernetesNetworkDriverApplyDiffDestroy(t *testing.T) {
	ctx := context.Background()
	backend := NewBackend()
	driver := &NetworkDriver{baseDriver{backend: backend}}
	in := registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "stack",
		Asset: taskdomain.AbstractAsset{
			Type: "Network",
			Name: "backend",
			Properties: map[string]any{
				"scope":    "cluster",
				"internal": true,
			},
		},
		Target: targetdomain.Placement{Cluster: "main", Namespace: "platform"},
	}

	report, err := driver.Diff(ctx, in)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if report.Status == "InSync" {
		t.Fatalf("Diff() = %+v, want changed before apply", report)
	}

	result, err := driver.Apply(ctx, in)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	policy, ok, err := backend.GetNetworkPolicy("platform", networkPolicyName(in))
	if err != nil || !ok {
		t.Fatalf("network policy missing: ok=%v err=%v", ok, err)
	}
	if !policy.AllowCluster || !policy.Internal {
		t.Fatalf("policy flags = %+v", policy)
	}
	if policy.PodSelector[networkPodLabel("backend")] != "true" {
		t.Fatalf("pod selector = %+v", policy.PodSelector)
	}
	if result.Outputs["name"] != policy.Name {
		t.Fatalf("outputs = %+v", result.Outputs)
	}

	report, err = driver.Diff(ctx, in)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("Diff() = %+v, want InSync", report)
	}

	if err := driver.Destroy(ctx, in); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if _, ok, _ := backend.GetNetworkPolicy("platform", networkPolicyName(in)); ok {
		t.Fatalf("network policy still present after destroy")
	}
}

func TestKubernetesComputeJoinsNetworks(t *testing.T) {
	ctx := context.Background()
	backend := NewBackend()
	driver := &ComputeDriver{baseDriver{backend: backend}}
	in := registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "stack",
		Asset: taskdomain.AbstractAsset{
			Type: "Compute",
			Name: "api",
			Properties: map[string]any{
				"image":    "demo:v1",
				"networks": []any{"backend"},
			},
		},
		Target: targetdomain.Placement{Cluster: "main", Namespace: "platform"},
	}
	if _, err := driver.Apply(ctx, in); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	deployment, ok, err := backend.GetDeployment("platform", computeDeploymentName(in, nil))
	if err != nil || !ok {
		t.Fatalf("deployment missing: ok=%v err=%v", ok, err)
	}
	if deployment.Labels[networkPodLabel("backend")] != "true" {
		t.Fatalf("deployment labels = %+v", deployment.Labels)
	}
}
