package dockerdriver

import (
	"sort"
	"strconv"
	"strings"

	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
)

// DesiredContainerForDiff builds a Container struct from a ComputeSpec for use
// in CLI diff comparisons. It mirrors the field-mapping that Apply/Diff perform
// but skips payload merging, config mount resolution, and secret resolution.
//
// Env values that are non-string (e.g. secret_ref maps) are replaced with the
// sentinel "<secret>" so DetailedContainerDiff skips them during comparison.
func DesiredContainerForDiff(
	partition, intentName, assetName string,
	target targetdomain.Placement,
	spec *assetdefs.ComputeSpec,
	idx int,
) Container {
	name := driverutil.ResourceName("docker-ct", target, partition, intentName, assetName, strconv.Itoa(idx))
	network := driverutil.ResourceName("docker-net", target, "", "", "")

	env := make(map[string]string, len(spec.Env))
	for k, v := range spec.Env {
		if sv, ok := v.(string); ok {
			env[k] = sv
		} else {
			env[k] = "<secret>"
		}
	}

	ports := make([]PortBinding, 0, len(spec.Ports))
	for _, p := range spec.Ports {
		containerPort := 0
		if p.ContainerPort != nil {
			containerPort = *p.ContainerPort
		} else if p.Port != nil {
			containerPort = *p.Port
		}
		hostPort := 0
		if strings.TrimSpace(p.DynamicHostname) == "" && p.HostPort != nil {
			hostPort = *p.HostPort
		}
		ports = append(ports, PortBinding{
			Name:          p.Name,
			Protocol:      firstNonEmpty(p.Protocol, "TCP"),
			ContainerPort: containerPort,
			HostPort:      hostPort,
		})
	}

	var volumeMounts []VolumeMount
	for _, m := range spec.VolumeMounts {
		volumeMounts = append(volumeMounts, VolumeMount{
			Source:   driverutil.ResourceName("docker-vol", target, partition, intentName, m.Volume),
			Target:   m.Path,
			ReadOnly: m.ReadOnly,
		})
	}

	var hostBindMounts []HostBindMount
	for _, m := range spec.HostBindMounts {
		hostBindMounts = append(hostBindMounts, HostBindMount{
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}

	caps := append([]string(nil), spec.Capabilities...)
	sort.Strings(caps)

	var cpuLimit, memLimit, memReservation string
	if r := spec.Resources; r != nil {
		cpuLimit = r.Limits.CPU
		memLimit = r.Limits.Memory
		memReservation = r.Requests.Memory
	}

	return Container{
		Name:              name,
		Image:             spec.Image,
		Network:           network,
		Env:               env,
		Ports:             ports,
		VolumeMounts:      volumeMounts,
		HostBindMounts:    hostBindMounts,
		Privileged:        driverutil.BoolValue(spec.Privileged),
		Capabilities:      caps,
		ShmSize:           spec.ShmSize,
		GPUs:              spec.GPUs,
		Running:           true,
		CPULimit:          cpuLimit,
		MemoryLimit:       memLimit,
		MemoryReservation: memReservation,
	}
}

// DesiredNetworkForDiff builds a Network struct from a NetworkSpec for CLI
// diff comparisons.
func DesiredNetworkForDiff(
	assetName string,
	target targetdomain.Placement,
	spec *assetdefs.NetworkSpec,
) Network {
	driver := spec.Driver
	if driver == "" {
		driver = "bridge"
	}
	return Network{
		Name:     driverutil.ResourceName("docker-net", target, "", "", assetName),
		Driver:   driver,
		Internal: spec.Internal,
	}
}
