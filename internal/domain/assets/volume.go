package assets

import (
	"fmt"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
)

type VolumeSpec struct {
	Size       string `json:"size,omitempty" yaml:"size,omitempty"`
	Class      string `json:"class,omitempty" yaml:"class,omitempty"`
	AccessMode string `json:"accessMode,omitempty" yaml:"accessMode,omitempty"`
	Ephemeral  *bool  `json:"ephemeral,omitempty" yaml:"ephemeral,omitempty"`
	// ObserveExisting adopts a volume that already exists outside Guardian
	// instead of creating and managing one. Drivers that support it resolve
	// the live volume from ExistingID (falling back to the asset name) and
	// only verify that it exists; they never modify or delete it.
	ObserveExisting bool   `json:"observeExisting,omitempty" yaml:"observeExisting,omitempty"`
	ExistingID      string `json:"existingID,omitempty" yaml:"existingID,omitempty"`
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
	default:
		return fmt.Errorf("property accessMode has unsupported value %q", typed.AccessMode)
	}
	if typed.ObserveExisting && typed.Ephemeral != nil && *typed.Ephemeral {
		return fmt.Errorf("property ephemeral cannot be combined with observeExisting")
	}
	return nil
}
