package assets

import (
	"fmt"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type CDKStackSpec struct {
	Context map[string]string `json:"context,omitempty" yaml:"context,omitempty"`
	Env     map[string]any    `json:"env,omitempty" yaml:"env,omitempty"`
}

type cdkStackDefinition struct{}

func init() {
	Register(cdkStackDefinition{})
}

func (cdkStackDefinition) Type() string { return assetdomain.TypeCDKStack }

func (cdkStackDefinition) NewSpec() any { return &CDKStackSpec{} }

func (cdkStackDefinition) Validate(spec any, _ ValidationContext) error {
	typed, ok := spec.(*CDKStackSpec)
	if !ok {
		return fmt.Errorf("internal CDK stack spec type mismatch")
	}
	for key, value := range typed.Context {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("property context must not contain empty keys")
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("property context[%q] must not be empty", key)
		}
	}
	for key := range typed.Env {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("property env must not contain empty keys")
		}
	}
	return nil
}
