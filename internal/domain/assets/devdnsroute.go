package assets

import (
	"fmt"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type DevDNSRouteSpec struct {
	Hostname string `json:"hostname" yaml:"hostname"`
	Target   string `json:"target" yaml:"target"`
	PortName string `json:"portName,omitempty" yaml:"portName,omitempty"`
}

type devDNSRouteDefinition struct{}

func init() {
	Register(devDNSRouteDefinition{})
}

func (devDNSRouteDefinition) Type() string { return assetdomain.TypeDevDNSRoute }

func (devDNSRouteDefinition) NewSpec() any { return &DevDNSRouteSpec{} }

func (devDNSRouteDefinition) Validate(spec any, ctx ValidationContext) error {
	typed, ok := spec.(*DevDNSRouteSpec)
	if !ok {
		return fmt.Errorf("internal devdns route spec type mismatch")
	}
	if strings.TrimSpace(typed.Hostname) == "" {
		return fmt.Errorf("property hostname is required")
	}
	if err := validateAssetRef(ctx, typed.Target, assetdomain.TypeCompute, "target"); err != nil {
		return err
	}
	return nil
}
