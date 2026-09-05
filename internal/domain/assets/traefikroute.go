package assets

import (
	"fmt"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type TraefikRouteSpec struct {
	Hostname string `json:"hostname" yaml:"hostname"`
	Target   string `json:"target" yaml:"target"`
	PortName string `json:"portName,omitempty" yaml:"portName,omitempty"`
}

type traefikRouteDefinition struct{}

func init() {
	Register(traefikRouteDefinition{})
}

func (traefikRouteDefinition) Type() string { return assetdomain.TypeTraefikRoute }

func (traefikRouteDefinition) NewSpec() any { return &TraefikRouteSpec{} }

func (traefikRouteDefinition) Validate(spec any, ctx ValidationContext) error {
	return validateTraefikRouteSpec(spec, ctx)
}

func validateTraefikRouteSpec(spec any, ctx ValidationContext) error {
	typed, ok := spec.(*TraefikRouteSpec)
	if !ok {
		return fmt.Errorf("internal Traefik route spec type mismatch")
	}
	if strings.TrimSpace(typed.Hostname) == "" {
		return fmt.Errorf("property hostname is required")
	}
	if err := validateAssetRef(ctx, typed.Target, assetdomain.TypeCompute, "target"); err != nil {
		return err
	}
	return nil
}
