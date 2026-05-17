package assets

import (
	"fmt"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type ListenerSpec struct {
	Name     string `json:"name,omitempty" yaml:"name,omitempty"`
	Port     *int   `json:"port,omitempty" yaml:"port,omitempty"`
	Protocol string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
}

type LoadBalancerSpec struct {
	Config      string         `json:"config,omitempty" yaml:"config,omitempty"`
	Targets     []string       `json:"targets" yaml:"targets"`
	Listeners   []ListenerSpec `json:"listeners" yaml:"listeners"`
	Networks    []string       `json:"networks,omitempty" yaml:"networks,omitempty"`
	ServiceType string         `json:"serviceType,omitempty" yaml:"serviceType,omitempty"`
}

type loadBalancerDefinition struct{}

func init() {
	Register(loadBalancerDefinition{})
}

func (loadBalancerDefinition) Type() string { return assetdomain.TypeLoadBalancer }

func (loadBalancerDefinition) NewSpec() any { return &LoadBalancerSpec{} }

func (loadBalancerDefinition) Validate(spec any, ctx ValidationContext) error {
	typed, ok := spec.(*LoadBalancerSpec)
	if !ok {
		return fmt.Errorf("internal load balancer spec type mismatch")
	}
	if len(typed.Targets) == 0 {
		return fmt.Errorf("property targets requires at least one referenced compute asset")
	}
	for idx, target := range typed.Targets {
		if err := validateAssetRef(ctx, target, assetdomain.TypeCompute, fmt.Sprintf("targets[%d]", idx)); err != nil {
			return err
		}
	}
	if len(typed.Listeners) == 0 {
		return fmt.Errorf("property listeners requires at least one listener definition")
	}
	for idx, listener := range typed.Listeners {
		if err := requirePositiveInt(listener.Port, fmt.Sprintf("listeners[%d].port", idx)); err != nil {
			return err
		}
	}
	if typed.Config != "" {
		if err := validateAssetRef(ctx, typed.Config, assetdomain.TypeConfig, "config"); err != nil {
			return err
		}
	}
	switch typed.ServiceType {
	case "", "LoadBalancer", "NodePort", "ClusterIP":
	default:
		return fmt.Errorf("property serviceType must be one of LoadBalancer, NodePort, ClusterIP")
	}
	return nil
}
