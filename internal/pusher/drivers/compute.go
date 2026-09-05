package drivers

import (
	"context"
	"fmt"
	"sync"

	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
)

type ComputeDriver struct {
	mu        sync.Mutex
	instances map[string]ComputeInstance
}

type ComputeInstance struct {
	Image   string
	Running bool
}

func NewComputeDriver() *ComputeDriver {
	return &ComputeDriver{instances: map[string]ComputeInstance{}}
}

func (d *ComputeDriver) Type() string { return "Compute" }

func (d *ComputeDriver) Validate(props map[string]any) error {
	image, ok := stringProperty(props, "image")
	if !ok || image == "" {
		return fmt.Errorf("compute asset requires string property image")
	}
	return nil
}

func (d *ComputeDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

func (d *ComputeDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	key := instanceKey(in)
	d.mu.Lock()
	current, ok := d.instances[key]
	d.mu.Unlock()
	readiness := &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "local driver has no external dependencies"}
	if !ok {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "local compute instance is missing"}, readiness, nil
	}
	if !current.Running {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "local compute instance is not running"}, readiness, nil
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "local compute instance is running"}, readiness, nil
}

func (d *ComputeDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	desiredImage, _ := stringProperty(in.Asset.Properties, "image")
	key := instanceKey(in)
	d.mu.Lock()
	defer d.mu.Unlock()
	current, ok := d.instances[key]
	if !ok || current.Image != desiredImage {
		return taskdomain.DriftReport{
			Status:        "Changed",
			Summary:       fmt.Sprintf("compute asset %s differs", in.Asset.Name),
			ChangedAssets: []string{in.Asset.Name},
		}, nil
	}
	return taskdomain.DriftReport{Status: "InSync", Summary: fmt.Sprintf("compute asset %s is in sync", in.Asset.Name)}, nil
}

func (d *ComputeDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	desiredImage, _ := stringProperty(in.Asset.Properties, "image")
	key := instanceKey(in)
	d.mu.Lock()
	d.instances[key] = ComputeInstance{Image: desiredImage, Running: true}
	d.mu.Unlock()
	return registry.AssetResult{Outputs: map[string]string{
		"id":      key,
		"image":   desiredImage,
		"running": "true",
	}}, nil
}

func (d *ComputeDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	delete(d.instances, instanceKey(in))
	d.mu.Unlock()
	return nil
}

func instanceKey(in registry.AssetInput) string {
	return fmt.Sprintf("%s/%s/%s", in.PartitionName, in.IntentName, in.Asset.Name)
}
