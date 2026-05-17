package assets

import (
	"fmt"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type ConfigSpec struct {
	Format  string            `json:"format,omitempty" yaml:"format,omitempty"`
	Content string            `json:"content,omitempty" yaml:"content,omitempty"`
	Data    map[string]string `json:"data,omitempty" yaml:"data,omitempty"`
}

type configDefinition struct{}

func init() {
	Register(configDefinition{})
}

func (configDefinition) Type() string { return assetdomain.TypeConfig }

func (configDefinition) NewSpec() any { return &ConfigSpec{} }

func (configDefinition) Validate(spec any, _ ValidationContext) error {
	typed, ok := spec.(*ConfigSpec)
	if !ok {
		return fmt.Errorf("internal config spec type mismatch")
	}
	if typed.Content == "" && len(typed.Data) == 0 {
		return fmt.Errorf("requires either property content or property data")
	}
	for key, value := range typed.Data {
		if err := requireString(key, "data key"); err != nil {
			return fmt.Errorf("property data contains invalid key: %w", err)
		}
		if err := requireString(value, fmt.Sprintf("data[%s]", key)); err != nil {
			return err
		}
	}
	return nil
}
