package assets

import (
	"fmt"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
)

type ObservabilitySpec struct {
	Provider    string   `json:"provider,omitempty" yaml:"provider,omitempty"`
	Mode        string   `json:"mode,omitempty" yaml:"mode,omitempty"` // auto|env|sidecar|gateway|collector
	ServiceName string   `json:"serviceName,omitempty" yaml:"serviceName,omitempty"`
	Targets     []string `json:"targets,omitempty" yaml:"targets,omitempty"`
	Config      string   `json:"config,omitempty" yaml:"config,omitempty"`
	Volume      string   `json:"volume,omitempty" yaml:"volume,omitempty"`
	Endpoint    string   `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	Protocol    string   `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Networks    []string `json:"networks,omitempty" yaml:"networks,omitempty"`
	Receivers   []string `json:"receivers,omitempty" yaml:"receivers,omitempty"`
	Exporters   []string `json:"exporters,omitempty" yaml:"exporters,omitempty"`
	CloudWatch  *bool    `json:"cloudwatch,omitempty" yaml:"cloudwatch,omitempty"`
}

type observabilityDefinition struct{}

func init() {
	Register(observabilityDefinition{})
}

func (observabilityDefinition) Type() string { return assetdomain.TypeObservability }

func (observabilityDefinition) NewSpec() any { return &ObservabilitySpec{} }

func (observabilityDefinition) Validate(spec any, ctx ValidationContext) error {
	typed, ok := spec.(*ObservabilitySpec)
	if !ok {
		return fmt.Errorf("internal observability spec type mismatch")
	}
	switch typed.Provider {
	case "", "otel":
	default:
		return fmt.Errorf("property provider has unsupported value %q", typed.Provider)
	}
	switch typed.Mode {
	case "", "auto", "env", "sidecar", "gateway", "collector":
	default:
		return fmt.Errorf("property mode has unsupported value %q", typed.Mode)
	}
	if typed.Config == "" && typed.Endpoint == "" && len(typed.Exporters) == 0 && typed.Mode == "" {
		return fmt.Errorf("requires property config, endpoint, or exporters")
	}
	if typed.Config != "" {
		if err := validateAssetRef(ctx, typed.Config, assetdomain.TypeConfig, "config"); err != nil {
			return err
		}
	}
	if typed.Volume != "" {
		if err := validateAssetRef(ctx, typed.Volume, assetdomain.TypeVolume, "volume"); err != nil {
			return err
		}
	}
	if err := validateStringList(typed.Targets, "targets"); err != nil {
		return err
	}
	for idx, target := range typed.Targets {
		if err := validateAssetRef(ctx, target, assetdomain.TypeCompute, fmt.Sprintf("targets[%d]", idx)); err != nil {
			return err
		}
	}
	if err := validateStringList(typed.Receivers, "receivers"); err != nil {
		return err
	}
	if err := validateStringList(typed.Exporters, "exporters"); err != nil {
		return err
	}
	return nil
}
