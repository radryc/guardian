package assets

import (
	"fmt"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type SecretSpec struct {
	Value     string `json:"value,omitempty" yaml:"value,omitempty"`
	SecretRef string `json:"secretRef,omitempty" yaml:"secretRef,omitempty"`
}

type secretDefinition struct{}

func init() {
	Register(secretDefinition{})
}

func (secretDefinition) Type() string { return assetdomain.TypeSecret }

func (secretDefinition) NewSpec() any { return &SecretSpec{} }

func (secretDefinition) Validate(spec any, _ ValidationContext) error {
	typed, ok := spec.(*SecretSpec)
	if !ok {
		return fmt.Errorf("internal secret spec type mismatch")
	}
	hasValue := typed.Value != ""
	hasSecretRef := typed.SecretRef != ""
	if !hasValue && !hasSecretRef {
		return fmt.Errorf("requires either property value or property secretRef")
	}
	if hasValue && hasSecretRef {
		return fmt.Errorf("property value and property secretRef are mutually exclusive")
	}
	if hasSecretRef {
		if err := requireString(typed.SecretRef, "secretRef"); err != nil {
			return err
		}
	}
	return nil
}
