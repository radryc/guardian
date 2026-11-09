// Package gcpdriver implements Guardian asset drivers for Google Cloud using
// OpenTofu as the deployment engine.
package gcpdriver

import (
	"context"

	taskdomain "github.com/radryc/guardian/internal/domain/task"
)

// StackRequest describes one intent's desired state.
type StackRequest struct {
	Partition string
	Intent    string
	// Asset is the asset name inside the intent. Each asset owns a separate
	// OpenTofu workspace and GCS state prefix: one shared state would make an
	// apply of one asset destroy every other asset in the intent.
	Asset   string
	Project string
	Region  string
	// Blueprint is the embedded blueprint directory name (for example
	// "objectstore").
	Blueprint string
	// Variables are passed to the blueprint as terraform.tfvars.json.
	Variables map[string]any
	// DesiredHash is written as the guardian-hash label so drift can be
	// detected without a full plan.
	DesiredHash string
}

// StackState is the observed state of one intent workspace.
type StackState struct {
	Exists    bool
	Outputs   map[string]string
	Resources []string
}

// PlanOutcome reports whether the plan contains changes.
type PlanOutcome struct {
	Drift   bool
	Summary string
}

// BackendAPI is the deployment backend contract. One implementation uses
// OpenTofu; tests use an in-memory fake.
type BackendAPI interface {
	CheckEnvironment(ctx context.Context, req StackRequest) error
	Plan(ctx context.Context, req StackRequest) (PlanOutcome, error)
	Apply(ctx context.Context, req StackRequest) (map[string]string, error)
	Destroy(ctx context.Context, req StackRequest) error
	Get(ctx context.Context, req StackRequest) (StackState, error)
}

func changedDrift(assetName, summary string) taskdomain.DriftReport {
	return taskdomain.DriftReport{
		Status:        "Changed",
		Summary:       summary,
		ChangedAssets: []string{assetName},
	}
}

func inSyncDrift(_ string, summary string) taskdomain.DriftReport {
	return taskdomain.DriftReport{Status: "InSync", Summary: summary}
}

// assetTypeMap adapts an asset graph for reference validation.
func assetTypeMap(assets map[string]taskdomain.AbstractAsset) map[string]string {
	out := make(map[string]string, len(assets))
	for name, asset := range assets {
		out[name] = asset.Type
	}
	return out
}
