package dockerdriver

import (
	"context"
	"sort"
	"sync"
)

type Backend struct {
	mu         sync.Mutex
	networks   map[string]Network
	volumes    map[string]Volume
	configs    map[string]Config
	containers map[string]Container
}

func NewBackend() *Backend {
	return &Backend{
		networks:   map[string]Network{},
		volumes:    map[string]Volume{},
		configs:    map[string]Config{},
		containers: map[string]Container{},
	}
}

func (b *Backend) EnsureNetwork(ctx context.Context, network Network) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.networks[network.Name] = cloneNetwork(network)
	return nil
}

func (b *Backend) GetNetwork(ctx context.Context, name string) (Network, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	network, ok := b.networks[name]
	return cloneNetwork(network), ok, nil
}

func (b *Backend) DeleteNetwork(ctx context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.networks, name)
	return nil
}

func (b *Backend) ConnectNetwork(ctx context.Context, networkName, containerName string, aliases []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	container, ok := b.containers[containerName]
	if !ok {
		return nil
	}
	for _, en := range container.ExtraNetworks {
		if en.Name == networkName {
			return nil
		}
	}
	container.ExtraNetworks = append(container.ExtraNetworks, ExtraNetwork{Name: networkName, Aliases: append([]string(nil), aliases...)})
	b.containers[containerName] = container
	return nil
}

func (b *Backend) UpsertVolume(ctx context.Context, volume Volume) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.volumes[volume.Name] = cloneVolume(volume)
	return nil
}

func (b *Backend) GetVolume(ctx context.Context, name string) (Volume, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	volume, ok := b.volumes[name]
	return cloneVolume(volume), ok, nil
}

func (b *Backend) DeleteVolume(ctx context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.volumes, name)
	return nil
}

func (b *Backend) UpsertConfig(ctx context.Context, config Config) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.configs[config.Name] = cloneConfig(config)
	return nil
}

func (b *Backend) GetConfig(ctx context.Context, name string) (Config, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	config, ok := b.configs[name]
	return cloneConfig(config), ok, nil
}

func (b *Backend) DeleteConfig(ctx context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.configs, name)
	return nil
}

func (b *Backend) UpsertContainer(ctx context.Context, container Container) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.containers[container.Name] = cloneContainer(container)
	return nil
}

func (b *Backend) GetContainer(ctx context.Context, name string) (Container, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	container, ok := b.containers[name]
	return cloneContainer(container), ok, nil
}

func (b *Backend) DeleteContainer(ctx context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.containers, name)
	return nil
}

func (b *Backend) ListContainersByAsset(ctx context.Context, partition, intent, asset string) ([]Container, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Container, 0)
	for _, container := range b.containers {
		if container.Labels["guardian.partition"] != partition ||
			container.Labels["guardian.intent"] != intent ||
			container.Labels["guardian.asset"] != asset {
			continue
		}
		out = append(out, cloneContainer(container))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func cloneNetwork(in Network) Network {
	return Network{Name: in.Name, Hash: in.Hash, Labels: cloneStringMap(in.Labels), Driver: in.Driver, Internal: in.Internal}
}

func cloneVolume(in Volume) Volume {
	return Volume{
		Name:       in.Name,
		Hash:       in.Hash,
		Labels:     cloneStringMap(in.Labels),
		Size:       in.Size,
		AccessMode: in.AccessMode,
		Ephemeral:  in.Ephemeral,
	}
}

func cloneConfig(in Config) Config {
	return Config{Name: in.Name, Hash: in.Hash, Labels: cloneStringMap(in.Labels), Files: cloneStringMap(in.Files), Binary: in.Binary}
}

func cloneExtraNetworks(in []ExtraNetwork) []ExtraNetwork {
	if len(in) == 0 {
		return nil
	}
	out := make([]ExtraNetwork, len(in))
	for i, en := range in {
		out[i] = ExtraNetwork{Name: en.Name, Aliases: append([]string(nil), en.Aliases...)}
	}
	return out
}

func cloneContainer(in Container) Container {
	return Container{
		Name:              in.Name,
		Kind:              in.Kind,
		Image:             in.Image,
		Hash:              in.Hash,
		Labels:            cloneStringMap(in.Labels),
		Network:           in.Network,
		Aliases:           append([]string(nil), in.Aliases...),
		ExtraNetworks:     cloneExtraNetworks(in.ExtraNetworks),
		ExtraHosts:        cloneStringMap(in.ExtraHosts),
		Command:           append([]string(nil), in.Command...),
		Args:              append([]string(nil), in.Args...),
		Env:               cloneStringMap(in.Env),
		Ports:             append([]PortBinding(nil), in.Ports...),
		VolumeMounts:      append([]VolumeMount(nil), in.VolumeMounts...),
		ConfigMounts:      append([]ConfigMount(nil), in.ConfigMounts...),
		HostBindMounts:    append([]HostBindMount(nil), in.HostBindMounts...),
		InlineFiles:       cloneStringMap(in.InlineFiles),
		Privileged:        in.Privileged,
		Capabilities:      append([]string(nil), in.Capabilities...),
		Running:           in.Running,
		CPULimit:          in.CPULimit,
		MemoryLimit:       in.MemoryLimit,
		MemoryReservation: in.MemoryReservation,
	}
}
