package drivers

import (
	"context"
	"fmt"
	"sync"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
)

type DatabaseDriver struct {
	mu        sync.Mutex
	databases map[string]DatabaseInstance
}

type DatabaseInstance struct {
	Engine string
	URL    string
}

func NewDatabaseDriver() *DatabaseDriver {
	return &DatabaseDriver{databases: map[string]DatabaseInstance{}}
}

func (d *DatabaseDriver) Type() string { return assetdomain.TypeDatabase }

func (d *DatabaseDriver) Validate(props map[string]any) error {
	engine, ok := stringProperty(props, "engine")
	if !ok || engine == "" {
		return fmt.Errorf("database asset requires string property engine")
	}
	return nil
}

func (d *DatabaseDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

func (d *DatabaseDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	key := instanceKey(in)
	d.mu.Lock()
	_, exists := d.databases[key]
	d.mu.Unlock()
	readiness := &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "local driver has no external dependencies"}
	if !exists {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "local database instance is missing"}, readiness, nil
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "local database instance is available"}, readiness, nil
}

func (d *DatabaseDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	engine, _ := stringProperty(in.Asset.Properties, "engine")
	url, ok := stringProperty(in.Asset.Properties, "url")
	if !ok || url == "" {
		url = defaultDatabaseURL(engine, in)
	}
	key := instanceKey(in)
	d.mu.Lock()
	defer d.mu.Unlock()
	current, exists := d.databases[key]
	if !exists || current.Engine != engine || current.URL != url {
		return taskdomain.DriftReport{
			Status:        "Changed",
			Summary:       fmt.Sprintf("database asset %s differs", in.Asset.Name),
			ChangedAssets: []string{in.Asset.Name},
		}, nil
	}
	return taskdomain.DriftReport{Status: "InSync", Summary: fmt.Sprintf("database asset %s is in sync", in.Asset.Name)}, nil
}

func (d *DatabaseDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	engine, _ := stringProperty(in.Asset.Properties, "engine")
	url, ok := stringProperty(in.Asset.Properties, "url")
	if !ok || url == "" {
		url = defaultDatabaseURL(engine, in)
	}
	key := instanceKey(in)
	d.mu.Lock()
	d.databases[key] = DatabaseInstance{Engine: engine, URL: url}
	d.mu.Unlock()
	return registry.AssetResult{Outputs: map[string]string{
		"engine": engine,
		"url":    url,
	}}, nil
}

func (d *DatabaseDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	delete(d.databases, instanceKey(in))
	d.mu.Unlock()
	return nil
}

func defaultDatabaseURL(engine string, in registry.AssetInput) string {
	return fmt.Sprintf("%s://db.local/%s/%s/%s", engine, in.PartitionName, in.IntentName, in.Asset.Name)
}
