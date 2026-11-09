package gcpdriver

import (
	"context"
	"fmt"
	"strings"

	"github.com/radryc/guardian/internal/pusher/drivers/gcp/opentofu"
)

// OpenTofuBackend implements BackendAPI with one locked OpenTofu workspace per
// partition/intent.
type OpenTofuBackend struct {
	backend *opentofu.Backend
}

// NewOpenTofuBackend builds an OpenTofu-backed deployment backend.
func NewOpenTofuBackend(cfg opentofu.Config) (*OpenTofuBackend, error) {
	backend, err := opentofu.New(cfg)
	if err != nil {
		return nil, err
	}
	return &OpenTofuBackend{backend: backend}, nil
}

// NewOpenTofuBackendWith wraps an existing opentofu.Backend (tests).
func NewOpenTofuBackendWith(backend *opentofu.Backend) *OpenTofuBackend {
	return &OpenTofuBackend{backend: backend}
}

func (b *OpenTofuBackend) prepare(ctx context.Context, req StackRequest) (*opentofu.Workspace, error) {
	if strings.TrimSpace(req.Project) == "" {
		return nil, fmt.Errorf("gcp: target project is required")
	}
	workspace, err := b.backend.Open(req.Partition, req.Intent, req.Asset)
	if err != nil {
		return nil, err
	}
	cleanup := func(err error) (*opentofu.Workspace, error) {
		_ = workspace.Close()
		return nil, err
	}

	files, err := BlueprintFiles(req.Blueprint)
	if err != nil {
		return cleanup(err)
	}
	if err := workspace.WriteBlueprint(files); err != nil {
		return cleanup(err)
	}

	variables := make(map[string]any, len(req.Variables)+3)
	for key, value := range req.Variables {
		variables[key] = value
	}
	variables["project"] = req.Project
	variables["region"] = req.Region
	if req.DesiredHash != "" {
		variables["hash"] = shortHash(req.DesiredHash)
	}
	if err := workspace.WriteVariables(variables); err != nil {
		return cleanup(err)
	}
	if err := workspace.Init(ctx); err != nil {
		return cleanup(err)
	}
	return workspace, nil
}

// CheckEnvironment initializes the workspace, which validates the provider
// download, GCS backend access, and state-encryption key.
func (b *OpenTofuBackend) CheckEnvironment(ctx context.Context, req StackRequest) error {
	workspace, err := b.prepare(ctx, req)
	if err != nil {
		return err
	}
	return workspace.Close()
}

// Plan reports whether the workspace has pending changes.
func (b *OpenTofuBackend) Plan(ctx context.Context, req StackRequest) (PlanOutcome, error) {
	workspace, err := b.prepare(ctx, req)
	if err != nil {
		return PlanOutcome{}, err
	}
	defer workspace.Close()

	result, err := workspace.Plan(ctx)
	if err != nil {
		return PlanOutcome{Summary: result.Summary()}, err
	}
	return PlanOutcome{Drift: result.Drift, Summary: result.Summary()}, nil
}

// Apply applies the workspace and returns its outputs.
func (b *OpenTofuBackend) Apply(ctx context.Context, req StackRequest) (map[string]string, error) {
	workspace, err := b.prepare(ctx, req)
	if err != nil {
		return nil, err
	}
	defer workspace.Close()

	if err := workspace.Apply(ctx); err != nil {
		return nil, err
	}
	return workspace.Output(ctx)
}

// Destroy destroys the workspace resources. It is a no-op for empty state.
func (b *OpenTofuBackend) Destroy(ctx context.Context, req StackRequest) error {
	workspace, err := b.prepare(ctx, req)
	if err != nil {
		return err
	}
	defer workspace.Close()

	resources, err := workspace.StateList(ctx)
	if err != nil {
		return err
	}
	if len(resources) == 0 {
		return nil
	}
	return workspace.Destroy(ctx)
}

// Get returns the observed outputs and resources for the workspace.
func (b *OpenTofuBackend) Get(ctx context.Context, req StackRequest) (StackState, error) {
	workspace, err := b.prepare(ctx, req)
	if err != nil {
		return StackState{}, err
	}
	defer workspace.Close()

	resources, err := workspace.StateList(ctx)
	if err != nil {
		return StackState{}, err
	}
	if len(resources) == 0 {
		return StackState{Exists: false}, nil
	}
	outputs, err := workspace.Output(ctx)
	if err != nil {
		return StackState{}, err
	}
	return StackState{Exists: true, Outputs: outputs, Resources: resources}, nil
}
