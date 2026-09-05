package assets

import (
	"fmt"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type VolumeSpec struct {
	Size       string `json:"size,omitempty" yaml:"size,omitempty"`
	Class      string `json:"class,omitempty" yaml:"class,omitempty"`
	AccessMode string `json:"accessMode,omitempty" yaml:"accessMode,omitempty"`
	Ephemeral  *bool  `json:"ephemeral,omitempty" yaml:"ephemeral,omitempty"`
}

type volumeDefinition struct{}

func init() {
	Register(volumeDefinition{})
}

func (volumeDefinition) Type() string { return assetdomain.TypeVolume }

func (volumeDefinition) NewSpec() any { return &VolumeSpec{} }

func (volumeDefinition) Validate(spec any, _ ValidationContext) error {
	typed, ok := spec.(*VolumeSpec)
	if !ok {
		return fmt.Errorf("internal volume spec type mismatch")
	}
	switch typed.AccessMode {
	case "", "ReadWriteOnce", "ReadWriteMany", "ReadOnlyMany":
		return nil
	default:
		return fmt.Errorf("property accessMode has unsupported value %q", typed.AccessMode)
	}
}
