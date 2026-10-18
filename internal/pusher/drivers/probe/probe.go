// Package probe registers the Probe asset with provider driver registries.
// Probes are executed by the pusher-local probe runner, not by a deployment
// driver, so this driver is intentionally a no-op.
package probe

import (
	"context"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func (*Driver) Type() string                  { return assetdomain.TypeProbe }
func (*Driver) Validate(map[string]any) error { return nil }
func (*Driver) Check(ctx context.Context, _ registry.AssetInput) error {
	return ctx.Err()
}
func (*Driver) Diff(_ context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	return taskdomain.DriftReport{Status: "InSync", Summary: "probe is executed by the pusher-local probe runner"}, nil
}
func (*Driver) Apply(context.Context, registry.AssetInput) (registry.AssetResult, error) {
	return registry.AssetResult{}, nil
}
func (*Driver) Destroy(ctx context.Context, _ registry.AssetInput) error {
	return ctx.Err()
}
func (*Driver) ObserveState(context.Context, registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "probe is executed by the pusher-local probe runner"},
		&taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "probe configuration is ready"}, nil
}
