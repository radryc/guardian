package dockerdriver

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// GuardianSnapshot holds all guardian-managed Docker resources discovered via
// docker inspect, with full field data parsed from the inspect output.
type GuardianSnapshot struct {
	Containers []Container
	Volumes    []Volume
	Networks   []Network
}

// SnapshotGuardianResources discovers all Docker resources labelled
// guardian.managed=true and returns a snapshot with full inspect data.
// Resources are identified exclusively by label, not by name convention.
func (b *CLIBackend) SnapshotGuardianResources() (*GuardianSnapshot, error) {
	containers, err := b.snapshotContainers()
	if err != nil {
		return nil, fmt.Errorf("snapshot containers: %w", err)
	}
	volumes, err := b.snapshotVolumes()
	if err != nil {
		return nil, fmt.Errorf("snapshot volumes: %w", err)
	}
	networks, err := b.snapshotNetworks()
	if err != nil {
		return nil, fmt.Errorf("snapshot networks: %w", err)
	}
	return &GuardianSnapshot{
		Containers: containers,
		Volumes:    volumes,
		Networks:   networks,
	}, nil
}

func (b *CLIBackend) snapshotContainers() ([]Container, error) {
	out, err := exec.Command(b.docker, "ps", "-aq", "--filter", "label=guardian.managed=true").Output() //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("list guardian containers: %w", err)
	}
	ids := strings.Fields(strings.TrimSpace(string(out)))
	if len(ids) == 0 {
		return nil, nil
	}
	args := append([]string{"container", "inspect"}, ids...)
	inspectOut, err := exec.Command(b.docker, args...).Output() //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("inspect guardian containers: %w", err)
	}
	return parseContainerInspect(inspectOut)
}

func (b *CLIBackend) snapshotVolumes() ([]Volume, error) {
	out, err := exec.Command(b.docker, "volume", "ls", "-q", "--filter", "label=guardian.managed=true").Output() //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("list guardian volumes: %w", err)
	}
	names := strings.Fields(strings.TrimSpace(string(out)))
	if len(names) == 0 {
		return nil, nil
	}
	args := append([]string{"volume", "inspect"}, names...)
	inspectOut, err := exec.Command(b.docker, args...).Output() //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("inspect guardian volumes: %w", err)
	}
	return parseVolumeInspect(inspectOut)
}

func (b *CLIBackend) snapshotNetworks() ([]Network, error) {
	out, err := exec.Command(b.docker, "network", "ls", "-q", "--filter", "label=guardian.managed=true").Output() //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("list guardian networks: %w", err)
	}
	ids := strings.Fields(strings.TrimSpace(string(out)))
	if len(ids) == 0 {
		return nil, nil
	}
	args := append([]string{"network", "inspect"}, ids...)
	inspectOut, err := exec.Command(b.docker, args...).Output() //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("inspect guardian networks: %w", err)
	}
	return parseNetworkInspect(inspectOut)
}

// rawContainerInspect is the subset of `docker container inspect` JSON we
// decode. Only fields needed for structural drift detection and asset
// reconstruction are included.
type rawContainerInspect struct {
	Name   string `json:"Name"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
		Env    []string          `json:"Env"` // "KEY=VALUE" pairs
	} `json:"Config"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
	HostConfig struct {
		NetworkMode    string   `json:"NetworkMode"`
		CapAdd         []string `json:"CapAdd"`
		Privileged     bool     `json:"Privileged"`
		ShmSize        int64    `json:"ShmSize"`
		DeviceRequests []struct {
			Driver       string   `json:"Driver"`
			Count        int      `json:"Count"`
			DeviceIDs    []string `json:"DeviceIDs"`
			Capabilities [][]string `json:"Capabilities"`
		} `json:"DeviceRequests"`
		PortBindings map[string][]struct {
			HostPort string `json:"HostPort"`
		} `json:"PortBindings"`
	} `json:"HostConfig"`
	Mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`        // volume name (type=volume)
		Source      string `json:"Source"`      // host path (type=bind)
		Destination string `json:"Destination"` // container path
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
	NetworkSettings struct {
		Networks map[string]struct {
			Aliases []string `json:"Aliases"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

// parseContainerInspect decodes `docker container inspect` JSON output into
// Container structs with all available structural fields populated.
func parseContainerInspect(data []byte) ([]Container, error) {
	var raw []rawContainerInspect
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode container inspect: %w", err)
	}
	out := make([]Container, 0, len(raw))
	for _, r := range raw {
		out = append(out, containerFromRawInspect(r))
	}
	return out, nil
}

func containerFromRawInspect(r rawContainerInspect) Container {
	labels := cloneStringMap(r.Config.Labels)

	// Env: "KEY=VALUE" list → map
	env := make(map[string]string, len(r.Config.Env))
	for _, e := range r.Config.Env {
		k, v, ok := strings.Cut(e, "=")
		if ok {
			env[k] = v
		}
	}

	// Ports: "8080/tcp" → PortBinding
	var ports []PortBinding
	for portProto, bindings := range r.HostConfig.PortBindings {
		portStr, proto, _ := strings.Cut(portProto, "/")
		containerPort, _ := strconv.Atoi(portStr)
		hostPort := 0
		if len(bindings) > 0 {
			hostPort, _ = strconv.Atoi(bindings[0].HostPort)
		}
		ports = append(ports, PortBinding{
			Protocol:      strings.ToUpper(proto),
			ContainerPort: containerPort,
			HostPort:      hostPort,
		})
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].ContainerPort < ports[j].ContainerPort })

	// Mounts: split by type
	var volumeMounts []VolumeMount
	var hostBindMounts []HostBindMount
	for _, m := range r.Mounts {
		switch m.Type {
		case "volume":
			volumeMounts = append(volumeMounts, VolumeMount{
				Source:   m.Name,
				Target:   m.Destination,
				ReadOnly: !m.RW,
			})
		case "bind":
			hostBindMounts = append(hostBindMounts, HostBindMount{
				Source:   m.Source,
				Target:   m.Destination,
				ReadOnly: !m.RW,
			})
		}
	}

	// Primary network and aliases
	network := r.HostConfig.NetworkMode
	var aliases []string
	if netInfo, ok := r.NetworkSettings.Networks[network]; ok {
		aliases = netInfo.Aliases
	}

	// Extra networks (all but primary)
	var extraNetworks []ExtraNetwork
	for netName, netInfo := range r.NetworkSettings.Networks {
		if netName == network {
			continue
		}
		extraNetworks = append(extraNetworks, ExtraNetwork{
			Name:    netName,
			Aliases: append([]string(nil), netInfo.Aliases...),
		})
	}
	sort.Slice(extraNetworks, func(i, j int) bool { return extraNetworks[i].Name < extraNetworks[j].Name })

	caps := append([]string(nil), r.HostConfig.CapAdd...)
	sort.Strings(caps)

	return Container{
		Name:           strings.TrimPrefix(r.Name, "/"),
		Image:          r.Config.Image,
		Hash:           labels["guardian.hash"],
		Labels:         labels,
		Running:        r.State.Running,
		Network:        network,
		Aliases:        aliases,
		ExtraNetworks:  extraNetworks,
		Env:            env,
		Ports:          ports,
		VolumeMounts:   volumeMounts,
		HostBindMounts: hostBindMounts,
		Privileged:     r.HostConfig.Privileged,
		Capabilities:   caps,
		ShmSize:        formatShmSize(r.HostConfig.ShmSize),
		GPUs:           parseDeviceRequests(r.HostConfig.DeviceRequests),
	}
}

// formatShmSize converts a ShmSize in bytes (from docker inspect) to the same
// human-readable string that docker accepts (e.g. 17179869184 → "16g").
// Returns empty string for 0 or the default 64 MiB shm that docker always allocates.
func formatShmSize(bytes int64) string {
	const dockerDefaultShm = 67108864 // 64 MiB
	if bytes <= 0 || bytes == dockerDefaultShm {
		return ""
	}
	gb := bytes / (1024 * 1024 * 1024)
	if gb*1024*1024*1024 == bytes {
		return fmt.Sprintf("%dg", gb)
	}
	mb := bytes / (1024 * 1024)
	if mb*1024*1024 == bytes {
		return fmt.Sprintf("%dm", mb)
	}
	return fmt.Sprintf("%d", bytes)
}

// parseDeviceRequests converts the DeviceRequests from docker inspect back to
// the --gpus flag string (e.g. "all" or "device=0,1").
func parseDeviceRequests(reqs []struct {
	Driver       string     `json:"Driver"`
	Count        int        `json:"Count"`
	DeviceIDs    []string   `json:"DeviceIDs"`
	Capabilities [][]string `json:"Capabilities"`
}) string {
	if len(reqs) == 0 {
		return ""
	}
	for _, req := range reqs {
		if req.Count == -1 {
			return "all"
		}
		if len(req.DeviceIDs) > 0 {
			return "device=" + strings.Join(req.DeviceIDs, ",")
		}
	}
	return ""
}

// parseVolumeInspect decodes `docker volume inspect` JSON into Volume structs.
// Volume hash and metadata are read from guardian.* labels.
func parseVolumeInspect(data []byte) ([]Volume, error) {
	var raw []struct {
		Name   string            `json:"Name"`
		Labels map[string]string `json:"Labels"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode volume inspect: %w", err)
	}
	out := make([]Volume, 0, len(raw))
	for _, r := range raw {
		labels := cloneStringMap(r.Labels)
		out = append(out, Volume{
			Name:       r.Name,
			Hash:       labels["guardian.hash"],
			Labels:     labels,
			Size:       labels[volumeSizeLabel],
			AccessMode: labels[volumeAccessModeLabel],
			Ephemeral:  strings.EqualFold(labels[volumeEphemeralLabel], "true"),
		})
	}
	return out, nil
}

// parseNetworkInspect decodes `docker network inspect` JSON into Network structs.
func parseNetworkInspect(data []byte) ([]Network, error) {
	var raw []struct {
		Name     string            `json:"Name"`
		Labels   map[string]string `json:"Labels"`
		Driver   string            `json:"Driver"`
		Internal bool              `json:"Internal"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode network inspect: %w", err)
	}
	out := make([]Network, 0, len(raw))
	for _, r := range raw {
		labels := cloneStringMap(r.Labels)
		out = append(out, Network{
			Name:     r.Name,
			Hash:     labels["guardian.hash"],
			Labels:   labels,
			Driver:   r.Driver,
			Internal: r.Internal,
		})
	}
	return out, nil
}

// ContainerToProperties converts a Container (read from docker inspect) into a
// Guardian asset properties map. The result is suitable for embedding in an
// Intent manifest as the properties of a Compute asset.
//
// Note: volume mount sources are Docker volume names (e.g. docker-vol-...), not
// guardian asset names. The caller may want to reverse-map them via labels.
func ContainerToProperties(c Container) map[string]any {
	props := map[string]any{"image": c.Image}
	if c.Privileged {
		props["privileged"] = true
	}
	if len(c.Capabilities) > 0 {
		props["capabilities"] = append([]string(nil), c.Capabilities...)
	}
	if len(c.Ports) > 0 {
		ports := make([]map[string]any, 0, len(c.Ports))
		for _, p := range c.Ports {
			entry := map[string]any{"containerPort": p.ContainerPort}
			if p.HostPort > 0 {
				entry["hostPort"] = p.HostPort
			}
			if p.Protocol != "" && strings.ToUpper(p.Protocol) != "TCP" {
				entry["protocol"] = strings.ToUpper(p.Protocol)
			}
			if p.Name != "" {
				entry["name"] = p.Name
			}
			ports = append(ports, entry)
		}
		props["ports"] = ports
	}
	if len(c.Env) > 0 {
		env := make(map[string]any, len(c.Env))
		for k, v := range c.Env {
			env[k] = v
		}
		props["env"] = env
	}
	if len(c.VolumeMounts) > 0 {
		mounts := make([]map[string]any, 0, len(c.VolumeMounts))
		for _, m := range c.VolumeMounts {
			entry := map[string]any{"volume": m.Source, "path": m.Target}
			if m.ReadOnly {
				entry["readOnly"] = true
			}
			mounts = append(mounts, entry)
		}
		props["volumeMounts"] = mounts
	}
	if len(c.HostBindMounts) > 0 {
		mounts := make([]map[string]any, 0, len(c.HostBindMounts))
		for _, m := range c.HostBindMounts {
			entry := map[string]any{"source": m.Source, "target": m.Target}
			if m.ReadOnly {
				entry["readOnly"] = true
			}
			mounts = append(mounts, entry)
		}
		props["hostBindMounts"] = mounts
	}
	return props
}

// VolumeToProperties converts a Volume (from docker inspect) into a Guardian
// asset properties map for a Volume asset.
func VolumeToProperties(v Volume) map[string]any {
	props := map[string]any{}
	if v.Size != "" {
		props["size"] = v.Size
	}
	if v.AccessMode != "" {
		props["accessMode"] = v.AccessMode
	}
	if v.Ephemeral {
		props["ephemeral"] = true
	}
	return props
}

// NetworkToProperties converts a Network (from docker inspect) into a Guardian
// asset properties map for a Network asset.
func NetworkToProperties(n Network) map[string]any {
	props := map[string]any{}
	if n.Driver != "" && n.Driver != "bridge" {
		props["driver"] = n.Driver
	}
	if n.Internal {
		props["internal"] = true
	}
	return props
}
