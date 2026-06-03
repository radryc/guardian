package kubernetesdriver

import (
	"context"

	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/versioning/digest"
)

type volumePayload struct {
	Size         string `yaml:"size,omitempty"`
	AccessMode   string `yaml:"accessMode,omitempty"`
	StorageClass string `yaml:"storageClass,omitempty"`
	Ephemeral    *bool  `yaml:"ephemeral,omitempty"`
}

type configPayload struct {
	Data map[string]string `yaml:"data,omitempty"`
}

type workloadPayload struct {
	Image              string            `yaml:"image,omitempty"`
	Command            []string          `yaml:"command,omitempty"`
	Args               []string          `yaml:"args,omitempty"`
	Env                map[string]string `yaml:"env,omitempty"`
	Ports              []ServicePort     `yaml:"ports,omitempty"`
	VolumeMounts       []VolumeMount     `yaml:"volumeMounts,omitempty"`
	InlineFiles        map[string]string `yaml:"inlineFiles,omitempty"`
	ReadinessProbe     *Probe            `yaml:"readinessProbe,omitempty"`
	Replicas           *int              `yaml:"replicas,omitempty"`
	Privileged         *bool             `yaml:"privileged,omitempty"`
	Capabilities       []string          `yaml:"capabilities,omitempty"`
	ServiceType        string            `yaml:"serviceType,omitempty"`
	ServicePorts       []ServicePort     `yaml:"servicePorts,omitempty"`
	ServiceAnnotations map[string]string `yaml:"serviceAnnotations,omitempty"`
	ServiceAccountName string            `yaml:"serviceAccountName,omitempty"`
}

func loadVolumePayload(ctx context.Context, in registry.AssetInput) (volumePayload, error) {
	var payload volumePayload
	_, err := driverutil.LoadPayload(ctx, in, []string{"k8s", "kubernetes"}, &payload)
	return payload, err
}

func loadConfigPayload(ctx context.Context, in registry.AssetInput) (configPayload, error) {
	var payload configPayload
	_, err := driverutil.LoadPayload(ctx, in, []string{"k8s", "kubernetes"}, &payload)
	return payload, err
}

func loadWorkloadPayload(ctx context.Context, in registry.AssetInput) (workloadPayload, error) {
	var payload workloadPayload
	_, err := driverutil.LoadPayload(ctx, in, []string{"k8s", "kubernetes"}, &payload)
	return payload, err
}

func hashWithPayload(base string, payload any) string {
	return digest.MustNormalizedHash(struct {
		Base    string
		Payload any
	}{
		Base:    base,
		Payload: payload,
	})
}

func applyWorkloadPayload(deployment *Deployment, payload workloadPayload) {
	if deployment == nil {
		return
	}
	if payload.Image != "" {
		deployment.Container.Image = payload.Image
	}
	if len(payload.Command) > 0 {
		deployment.Container.Command = append([]string(nil), payload.Command...)
	}
	if len(payload.Args) > 0 {
		deployment.Container.Args = append([]string(nil), payload.Args...)
	}
	if len(payload.Env) > 0 {
		if deployment.Container.Env == nil {
			deployment.Container.Env = map[string]string{}
		}
		for key, value := range payload.Env {
			deployment.Container.Env[key] = value
		}
	}
	if len(payload.Ports) > 0 {
		deployment.Container.Ports = append([]ServicePort(nil), payload.Ports...)
	}
	if len(payload.VolumeMounts) > 0 {
		deployment.Container.VolumeMounts = append([]VolumeMount(nil), payload.VolumeMounts...)
	}
	if len(payload.InlineFiles) > 0 {
		deployment.Container.InlineFiles = cloneStringMap(payload.InlineFiles)
	}
	if payload.ReadinessProbe != nil {
		deployment.Container.ReadinessProbe = cloneProbe(payload.ReadinessProbe)
	}
	if payload.Privileged != nil {
		deployment.Container.Privileged = *payload.Privileged
	}
	if len(payload.Capabilities) > 0 {
		deployment.Container.Capabilities = append([]string(nil), payload.Capabilities...)
	}
	if payload.Replicas != nil && *payload.Replicas > 0 {
		deployment.Replicas = *payload.Replicas
	}
	if payload.ServiceAccountName != "" {
		deployment.ServiceAccountName = payload.ServiceAccountName
	}
}
