package assets

import (
	"fmt"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type NetworkSpec struct {
	Driver   string `json:"driver,omitempty" yaml:"driver,omitempty"`
	Internal bool   `json:"internal,omitempty" yaml:"internal,omitempty"`
	Scope    string `json:"scope,omitempty" yaml:"scope,omitempty"`
}

type networkDefinition struct{}

func init() {
	Register(networkDefinition{})
}

func (networkDefinition) Type() string { return assetdomain.TypeNetwork }

func (networkDefinition) NewSpec() any { return &NetworkSpec{} }

func (networkDefinition) Validate(spec any, _ ValidationContext) error {
	typed, ok := spec.(*NetworkSpec)
	if !ok {
		return fmt.Errorf("internal network spec type mismatch")
	}
	switch typed.Driver {
	case "", "bridge", "overlay", "host", "none":
	default:
		return fmt.Errorf("property driver has unsupported value %q", typed.Driver)
	}
	switch typed.Scope {
	case "", "partition", "cluster":
	default:
		return fmt.Errorf("property scope has unsupported value %q", typed.Scope)
	}
	return nil
}
