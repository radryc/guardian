package assets

import (
	"fmt"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type PortSpec struct {
	Name            string `json:"name,omitempty" yaml:"name,omitempty"`
	Protocol        string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Port            *int   `json:"port,omitempty" yaml:"port,omitempty"`
	ContainerPort   *int   `json:"containerPort,omitempty" yaml:"containerPort,omitempty"`
	HostPort        *int   `json:"hostPort,omitempty" yaml:"hostPort,omitempty"`
	ServicePort     *int   `json:"servicePort,omitempty" yaml:"servicePort,omitempty"`
	DynamicHostname string `json:"dynamicHostname,omitempty" yaml:"dynamicHostname,omitempty"`
}

type VolumeMountSpec struct {
	Volume   string `json:"volume" yaml:"volume"`
	Path     string `json:"path" yaml:"path"`
	ReadOnly bool   `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
}

type ConfigMountSpec struct {
	Config   string `json:"config" yaml:"config"`
	Path     string `json:"path" yaml:"path"`
	ReadOnly bool   `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
}

type ResourceClass struct {
	CPU    string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	Memory string `json:"memory,omitempty" yaml:"memory,omitempty"`
}

type ResourcesSpec struct {
	Limits       ResourceClass `json:"limits,omitempty" yaml:"limits,omitempty"`
	Requests     ResourceClass `json:"requests,omitempty" yaml:"requests,omitempty"`
	Reservations ResourceClass `json:"reservations,omitempty" yaml:"reservations,omitempty"`
}

type HealthCheckSpec struct {
	Test     StringList `json:"test,omitempty" yaml:"test,omitempty"`
	Interval string     `json:"interval,omitempty" yaml:"interval,omitempty"`
	Timeout  string     `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retries  *int       `json:"retries,omitempty" yaml:"retries,omitempty"`
}

type HostBindMountSpec struct {
	Source   string `json:"source" yaml:"source"`
	Target   string `json:"target" yaml:"target"`
	ReadOnly bool   `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
}

type ComputeSpec struct {
	Image                  string              `json:"image" yaml:"image"`
	ImagePullPolicy        string              `json:"imagePullPolicy,omitempty" yaml:"imagePullPolicy,omitempty"`
	ObserveExisting        bool                `json:"observeExisting,omitempty" yaml:"observeExisting,omitempty"`
	ExistingDeploymentName string              `json:"existingDeploymentName,omitempty" yaml:"existingDeploymentName,omitempty"`
	ExistingServiceName    string              `json:"existingServiceName,omitempty" yaml:"existingServiceName,omitempty"`
	Replicas               *int                `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	Command                StringList          `json:"command,omitempty" yaml:"command,omitempty"`
	Args                   StringList          `json:"args,omitempty" yaml:"args,omitempty"`
	Env                    map[string]any      `json:"env,omitempty" yaml:"env,omitempty"`
	Resources              *ResourcesSpec      `json:"resources,omitempty" yaml:"resources,omitempty"`
	HealthCheck            *HealthCheckSpec    `json:"healthCheck,omitempty" yaml:"healthCheck,omitempty"`
	Privileged             *bool               `json:"privileged,omitempty" yaml:"privileged,omitempty"`
	Capabilities           []string            `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	ShmSize                string              `json:"shmSize,omitempty" yaml:"shmSize,omitempty"`
	GPUs                   string              `json:"gpus,omitempty" yaml:"gpus,omitempty"`
	Networks               []string            `json:"networks,omitempty" yaml:"networks,omitempty"`
	Ports                  []PortSpec          `json:"ports,omitempty" yaml:"ports,omitempty"`
	VolumeMounts           []VolumeMountSpec   `json:"volumeMounts,omitempty" yaml:"volumeMounts,omitempty"`
	ConfigMounts           []ConfigMountSpec   `json:"configMounts,omitempty" yaml:"configMounts,omitempty"`
	HostBindMounts         []HostBindMountSpec `json:"hostBindMounts,omitempty" yaml:"hostBindMounts,omitempty"`
}

type computeDefinition struct{}

func init() {
	Register(computeDefinition{})
}

func (computeDefinition) Type() string { return assetdomain.TypeCompute }

func (computeDefinition) NewSpec() any { return &ComputeSpec{} }

func (computeDefinition) Validate(spec any, ctx ValidationContext) error {
	typed, ok := spec.(*ComputeSpec)
	if !ok {
		return fmt.Errorf("internal compute spec type mismatch")
	}
	if err := requireString(typed.Image, "image"); err != nil {
		return err
	}
	if typed.ImagePullPolicy != "" {
		switch typed.ImagePullPolicy {
		case "Always", "IfNotPresent", "Never":
		default:
			return fmt.Errorf("property imagePullPolicy must be one of Always, IfNotPresent, Never")
		}
	}
	if strings.TrimSpace(typed.ExistingDeploymentName) != "" && !typed.ObserveExisting {
		return fmt.Errorf("property existingDeploymentName requires observeExisting=true")
	}
	if strings.TrimSpace(typed.ExistingServiceName) != "" && !typed.ObserveExisting {
		return fmt.Errorf("property existingServiceName requires observeExisting=true")
	}
	if err := optionalPositiveInt(typed.Replicas, "replicas"); err != nil {
		return err
	}
	if err := validateStringList(typed.Capabilities, "capabilities"); err != nil {
		return err
	}
	for idx, port := range typed.Ports {
		if port.Port == nil && port.ContainerPort == nil {
			return fmt.Errorf("property ports[%d] requires either port or containerPort", idx)
		}
		if strings.TrimSpace(port.DynamicHostname) != "" && port.HostPort != nil {
			return fmt.Errorf("property ports[%d].hostPort cannot be set when dynamicHostname is used", idx)
		}
		if err := optionalPositiveInt(port.Port, fmt.Sprintf("ports[%d].port", idx)); err != nil {
			return err
		}
		if err := optionalPositiveInt(port.ContainerPort, fmt.Sprintf("ports[%d].containerPort", idx)); err != nil {
			return err
		}
		if err := optionalPositiveInt(port.HostPort, fmt.Sprintf("ports[%d].hostPort", idx)); err != nil {
			return err
		}
		if err := optionalPositiveInt(port.ServicePort, fmt.Sprintf("ports[%d].servicePort", idx)); err != nil {
			return err
		}
	}
	for idx, mount := range typed.VolumeMounts {
		if err := validateAssetRef(ctx, mount.Volume, assetdomain.TypeVolume, fmt.Sprintf("volumeMounts[%d].volume", idx)); err != nil {
			return err
		}
		if err := requireAbsolutePath(mount.Path, fmt.Sprintf("volumeMounts[%d].path", idx)); err != nil {
			return err
		}
	}
	for idx, mount := range typed.ConfigMounts {
		if err := validateAssetRef(ctx, mount.Config, assetdomain.TypeConfig, fmt.Sprintf("configMounts[%d].config", idx)); err != nil {
			return err
		}
		if err := requireAbsolutePath(mount.Path, fmt.Sprintf("configMounts[%d].path", idx)); err != nil {
			return err
		}
	}
	for idx, mount := range typed.HostBindMounts {
		if err := requireAbsolutePath(mount.Source, fmt.Sprintf("hostBindMounts[%d].source", idx)); err != nil {
			return err
		}
		if err := requireAbsolutePath(mount.Target, fmt.Sprintf("hostBindMounts[%d].target", idx)); err != nil {
			return err
		}
	}
	if typed.HealthCheck != nil {
		if err := optionalPositiveInt(typed.HealthCheck.Retries, "healthCheck.retries"); err != nil {
			return err
		}
	}
	return nil
}
