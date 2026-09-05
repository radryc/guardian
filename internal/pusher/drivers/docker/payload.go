package dockerdriver

import (
	"context"

	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/versioning/digest"
)

type volumePayload struct {
	Size       string `yaml:"size,omitempty"`
	AccessMode string `yaml:"accessMode,omitempty"`
	Ephemeral  *bool  `yaml:"ephemeral,omitempty"`
}

type configPayload struct {
	Files map[string]string `yaml:"files,omitempty"`
}

type containerPayload struct {
	Image          string            `yaml:"image,omitempty"`
	Aliases        []string          `yaml:"aliases,omitempty"`
	Command        []string          `yaml:"command,omitempty"`
	Args           []string          `yaml:"args,omitempty"`
	Env            map[string]string `yaml:"env,omitempty"`
	Ports          []PortBinding     `yaml:"ports,omitempty"`
	VolumeMounts   []VolumeMount     `yaml:"volumeMounts,omitempty"`
	ConfigMounts   []ConfigMount     `yaml:"configMounts,omitempty"`
	HostBindMounts []HostBindMount   `yaml:"hostBindMounts,omitempty"`
	InlineFiles    map[string]string `yaml:"inlineFiles,omitempty"`
	Privileged     *bool             `yaml:"privileged,omitempty"`
	Capabilities   []string          `yaml:"capabilities,omitempty"`
}

func loadVolumePayload(ctx context.Context, in registry.AssetInput) (volumePayload, error) {
	var payload volumePayload
	_, err := driverutil.LoadPayload(ctx, in, []string{"docker"}, &payload)
	return payload, err
}

func loadConfigPayload(ctx context.Context, in registry.AssetInput) (configPayload, error) {
	var payload configPayload
	_, err := driverutil.LoadPayload(ctx, in, []string{"docker"}, &payload)
	return payload, err
}

func loadContainerPayload(ctx context.Context, in registry.AssetInput) (containerPayload, error) {
	var payload containerPayload
	_, err := driverutil.LoadPayload(ctx, in, []string{"docker"}, &payload)
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

func applyContainerPayload(container *Container, payload containerPayload) {
	if container == nil {
		return
	}
	if payload.Image != "" {
		container.Image = payload.Image
	}
	if len(payload.Aliases) > 0 {
		container.Aliases = append([]string(nil), payload.Aliases...)
	}
	if len(payload.Command) > 0 {
		container.Command = append([]string(nil), payload.Command...)
	}
	if len(payload.Args) > 0 {
		container.Args = append([]string(nil), payload.Args...)
	}
	if len(payload.Env) > 0 {
		if container.Env == nil {
			container.Env = map[string]string{}
		}
		for key, value := range payload.Env {
			container.Env[key] = value
		}
	}
	if len(payload.Ports) > 0 {
		container.Ports = append([]PortBinding(nil), payload.Ports...)
	}
	if len(payload.VolumeMounts) > 0 {
		container.VolumeMounts = append([]VolumeMount(nil), payload.VolumeMounts...)
	}
	if len(payload.ConfigMounts) > 0 {
		container.ConfigMounts = append([]ConfigMount(nil), payload.ConfigMounts...)
	}
	if len(payload.HostBindMounts) > 0 {
		container.HostBindMounts = append([]HostBindMount(nil), payload.HostBindMounts...)
	}
	if len(payload.InlineFiles) > 0 {
		container.InlineFiles = cloneStringMap(payload.InlineFiles)
	}
	if payload.Privileged != nil {
		container.Privileged = *payload.Privileged
	}
	if len(payload.Capabilities) > 0 {
		container.Capabilities = append([]string(nil), payload.Capabilities...)
	}
}
