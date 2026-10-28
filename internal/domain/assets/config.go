package assets

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
)

type ConfigSpec struct {
	Format            string            `json:"format,omitempty" yaml:"format,omitempty"`
	Content           string            `json:"content,omitempty" yaml:"content,omitempty"`
	Data              map[string]string `json:"data,omitempty" yaml:"data,omitempty"`
	ExistingParameter string            `json:"existingParameter,omitempty" yaml:"existingParameter,omitempty"`
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
	format := assetdomain.NormalizeConfigFormat(typed.Format)
	switch format {
	case "text", "yaml", "json", "binary", "toml", "ini", "cfg":
	default:
		return fmt.Errorf("property format %q is not supported", typed.Format)
	}
	for key, value := range typed.Data {
		if err := requireString(key, "data key"); err != nil {
			return fmt.Errorf("property data contains invalid key: %w", err)
		}
		if err := requireString(value, fmt.Sprintf("data[%s]", key)); err != nil {
			return err
		}
		if format == "binary" {
			if _, err := decodeConfigBase64(value); err != nil {
				return fmt.Errorf("property data[%s] must be base64 when format is binary: %w", key, err)
			}
		}
	}
	if format == "binary" && strings.TrimSpace(typed.Content) != "" {
		if _, err := decodeConfigBase64(typed.Content); err != nil {
			return fmt.Errorf("property content must be base64 when format is binary: %w", err)
		}
	}
	return nil
}

func decodeConfigBase64(value string) ([]byte, error) {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
	return base64.StdEncoding.DecodeString(cleaned)
}
