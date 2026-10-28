package registry

import (
	"context"

	targetdomain "github.com/radryc/guardian/internal/domain/target"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/pkg/guardianapi"
)

type Registry struct {
	drivers map[string]AssetDriver
}

type AssetDriver interface {
	Type() string
	Validate(props map[string]any) error
	Check(ctx context.Context, in AssetInput) error
	Diff(ctx context.Context, in AssetInput) (taskdomain.DriftReport, error)
	Apply(ctx context.Context, in AssetInput) (AssetResult, error)
	Destroy(ctx context.Context, in AssetInput) error
}

type ObservedStateDriver interface {
	ObserveState(ctx context.Context, in AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error)
}

// ContextValidator is implemented by drivers whose validation depends on the
// rest of the intent's assets (for example Observability targets or Compute
// mounts). The runtime prefers it over AssetDriver.Validate, which only sees
// the asset in isolation.
type ContextValidator interface {
	ValidateInContext(props map[string]any, assets map[string]taskdomain.AbstractAsset) error
}

type AssetInput struct {
	PartitionName string
	IntentName    string
	IntentType    string
	PusherName    string
	Asset         taskdomain.AbstractAsset
	Assets        map[string]taskdomain.AbstractAsset
	AssetVersions map[string]string
	Target        targetdomain.Placement
	Store         guardianapi.ReadStore
	WorkerID      string
}

type AssetResult struct {
	Outputs map[string]string
	Logs    []taskdomain.LogEntry
}

func New() *Registry {
	return &Registry{drivers: map[string]AssetDriver{}}
}

func (r *Registry) Register(d AssetDriver) {
	if d == nil {
		return
	}
	r.RegisterAs(d.Type(), d)
}

func (r *Registry) RegisterAs(assetType string, d AssetDriver) {
	if d == nil {
		return
	}
	if r.drivers == nil {
		r.drivers = map[string]AssetDriver{}
	}
	r.drivers[assetType] = d
}

func (r *Registry) Get(assetType string) (AssetDriver, bool) {
	if r == nil {
		return nil, false
	}
	driver, ok := r.drivers[assetType]
	return driver, ok
}
