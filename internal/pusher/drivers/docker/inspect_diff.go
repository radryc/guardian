package dockerdriver

import (
	"fmt"
	"sort"
	"strings"
)

// DiffField is a single field-level difference between desired and actual state.
// Used by DetailedContainerDiff and DetailedNetworkDiff for CLI diff output.
type DiffField struct {
	Field   string
	Desired string
	Actual  string
}

// StructuralNetworkDrift compares desired vs actual Network structs using
// field-level equality for structural (non-ephemeral) properties.
//
// Intentionally NOT compared:
//   - Network ID: ephemeral, changes on every recreation
//   - guardian.hash label: Guardian bookkeeping, not live state
//
// Returns (drifted bool, human-readable reason).
func StructuralNetworkDrift(desired, actual Network) (bool, string) {
	fields := DetailedNetworkDiff(desired, actual)
	if len(fields) == 0 {
		return false, ""
	}
	f := fields[0]
	return true, fmt.Sprintf("%s changed: want %q got %q", f.Field, f.Desired, f.Actual)
}

// DetailedNetworkDiff returns per-field differences between desired and actual
// Network structs. An empty slice means no structural drift.
func DetailedNetworkDiff(desired, actual Network) []DiffField {
	var diffs []DiffField
	desiredDriver := desired.Driver
	if desiredDriver == "" {
		desiredDriver = "bridge"
	}
	if desiredDriver != actual.Driver {
		diffs = append(diffs, DiffField{"driver", desiredDriver, actual.Driver})
	}
	if desired.Internal != actual.Internal {
		diffs = append(diffs, DiffField{
			"internal",
			fmt.Sprintf("%v", desired.Internal),
			fmt.Sprintf("%v", actual.Internal),
		})
	}
	return diffs
}

// DetailedContainerDiff returns per-field differences between desired and actual
// Container structs. Returns nil if there are no structural differences.
//
// Env vars where desired is the sentinel "<secret>" are skipped — those values
// cannot be compared without secret resolution (e.g. from the CLI diff tool).
// Extra actual env keys not present in desired are also skipped (system-injected
// env set by the Docker daemon).
func DetailedContainerDiff(desired, actual Container) []DiffField {
	var diffs []DiffField
	if desired.Image != actual.Image {
		diffs = append(diffs, DiffField{"image", desired.Image, actual.Image})
	}
	if desired.Network != "" && desired.Network != actual.Network {
		diffs = append(diffs, DiffField{"network", desired.Network, actual.Network})
	}
	if !containerPortsMatch(desired.Ports, actual.Ports) {
		diffs = append(diffs, DiffField{"ports", portsString(desired.Ports), portsString(actual.Ports)})
	}
	for k, dv := range desired.Env {
		if dv == "<secret>" {
			continue
		}
		if av, ok := actual.Env[k]; !ok {
			diffs = append(diffs, DiffField{"env[" + k + "]", dv, "<absent>"})
		} else if dv != av {
			diffs = append(diffs, DiffField{"env[" + k + "]", dv, av})
		}
	}
	if !volumeMountsSliceMatch(desired.VolumeMounts, actual.VolumeMounts) {
		diffs = append(diffs, DiffField{"volumeMounts",
			volumeMountsString(desired.VolumeMounts),
			volumeMountsString(actual.VolumeMounts),
		})
	}
	if !hostBindMountsSliceMatch(desired.HostBindMounts, actual.HostBindMounts) {
		diffs = append(diffs, DiffField{"hostBindMounts",
			hostBindMountsString(desired.HostBindMounts),
			hostBindMountsString(actual.HostBindMounts),
		})
	}
	if !sortedStringSlicesMatch(desired.Capabilities, actual.Capabilities) {
		diffs = append(diffs, DiffField{
			"capabilities",
			strings.Join(sortedCopy(desired.Capabilities), ","),
			strings.Join(sortedCopy(actual.Capabilities), ","),
		})
	}
	if desired.Privileged != actual.Privileged {
		diffs = append(diffs, DiffField{
			"privileged",
			fmt.Sprintf("%v", desired.Privileged),
			fmt.Sprintf("%v", actual.Privileged),
		})
	}
	if desired.ShmSize != actual.ShmSize {
		diffs = append(diffs, DiffField{"shmSize", desired.ShmSize, actual.ShmSize})
	}
	if desired.GPUs != actual.GPUs {
		diffs = append(diffs, DiffField{"gpus", desired.GPUs, actual.GPUs})
	}
	return diffs
}

// portsString formats a []PortBinding as a compact display string.
func portsString(ports []PortBinding) string {
	cp := make([]PortBinding, len(ports))
	copy(cp, ports)
	sort.Slice(cp, func(i, j int) bool { return cp[i].ContainerPort < cp[j].ContainerPort })
	parts := make([]string, 0, len(cp))
	for _, p := range cp {
		parts = append(parts, fmt.Sprintf("%d/%s", p.ContainerPort, normalizeProto(p.Protocol)))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// volumeMountsString formats []VolumeMount as a compact display string.
func volumeMountsString(mounts []VolumeMount) string {
	cp := make([]VolumeMount, len(mounts))
	copy(cp, mounts)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Target < cp[j].Target })
	parts := make([]string, 0, len(cp))
	for _, m := range cp {
		s := m.Source + ":" + m.Target
		if m.ReadOnly {
			s += ":ro"
		}
		parts = append(parts, s)
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// hostBindMountsString formats []HostBindMount as a compact display string.
func hostBindMountsString(mounts []HostBindMount) string {
	cp := make([]HostBindMount, len(mounts))
	copy(cp, mounts)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Target < cp[j].Target })
	parts := make([]string, 0, len(cp))
	for _, m := range cp {
		s := m.Source + ":" + m.Target
		if m.ReadOnly {
			s += ":ro"
		}
		parts = append(parts, s)
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// StructuralContainerDrift compares desired vs actual Container structs using
// field-level equality for structural (immutable-intent) properties.
//
// Intentionally NOT compared:
//   - Container ID: ephemeral, changes on every recreation
//   - guardian.hash label: that is Guardian bookkeeping, not live state
//   - Aliases, ExtraHosts, ExtraNetworks: injected defaults / secondary config
//   - Labels: Guardian metadata, not user-controlled live state
//   - ConfigMounts: represented as bind-mounts pointing to guardian state dirs;
//     the hash covers them indirectly via the guardian.hash label fallback
//
// Returns (drifted bool, human-readable reason).
func StructuralContainerDrift(desired, actual Container) (bool, string) {
	if desired.Image != actual.Image {
		return true, fmt.Sprintf("image changed: want %q got %q", desired.Image, actual.Image)
	}
	if desired.Network != "" && desired.Network != actual.Network {
		return true, fmt.Sprintf("network changed: want %q got %q", desired.Network, actual.Network)
	}
	if !containerPortsMatch(desired.Ports, actual.Ports) {
		return true, "container ports differ"
	}
	if !envMapsMatch(desired.Env, actual.Env) {
		return true, "container env differs"
	}
	if !volumeMountsSliceMatch(desired.VolumeMounts, actual.VolumeMounts) {
		return true, "volume mounts differ"
	}
	if !hostBindMountsSliceMatch(desired.HostBindMounts, actual.HostBindMounts) {
		return true, "host bind mounts differ"
	}
	if !sortedStringSlicesMatch(desired.Capabilities, actual.Capabilities) {
		return true, "capabilities differ"
	}
	if desired.Privileged != actual.Privileged {
		return true, fmt.Sprintf("privileged changed: want %v got %v", desired.Privileged, actual.Privileged)
	}
	if desired.ShmSize != actual.ShmSize {
		return true, fmt.Sprintf("shmSize changed: want %q got %q", desired.ShmSize, actual.ShmSize)
	}
	if desired.GPUs != actual.GPUs {
		return true, fmt.Sprintf("gpus changed: want %q got %q", desired.GPUs, actual.GPUs)
	}
	return false, ""
}

// containerPortsMatch compares only ports that are actually published.
// Hostless ports are internal-only and should not trigger drift.
func containerPortsMatch(desired, actual []PortBinding) bool {
	type portKey struct {
		port  int
		proto string
	}
	toSet := func(ports []PortBinding) map[portKey]struct{} {
		s := make(map[portKey]struct{}, len(ports))
		for _, p := range ports {
			if p.ContainerPort > 0 && p.HostPort > 0 {
				s[portKey{p.ContainerPort, normalizeProto(p.Protocol)}] = struct{}{}
			}
		}
		return s
	}
	ds := toSet(desired)
	as := toSet(actual)
	if len(ds) != len(as) {
		return false
	}
	for k := range ds {
		if _, ok := as[k]; !ok {
			return false
		}
	}
	return true
}

// envMapsMatch compares desired env keys/values against actual.
// Extra actual env keys are ignored because runtimes often inject defaults
// like PATH, HOSTNAME, and HOME.
func envMapsMatch(desired, actual map[string]string) bool {
	for k, dv := range desired {
		av, ok := actual[k]
		if !ok || dv != av {
			return false
		}
	}
	return true
}

// volumeMountsSliceMatch compares volume mounts by source name and target path.
// ReadOnly flag is also compared.
func volumeMountsSliceMatch(desired, actual []VolumeMount) bool {
	type key struct {
		src, tgt string
		ro       bool
	}
	toSet := func(mounts []VolumeMount) map[key]struct{} {
		s := make(map[key]struct{}, len(mounts))
		for _, m := range mounts {
			s[key{m.Source, m.Target, m.ReadOnly}] = struct{}{}
		}
		return s
	}
	ds := toSet(desired)
	as := toSet(actual)
	if len(ds) != len(as) {
		return false
	}
	for k := range ds {
		if _, ok := as[k]; !ok {
			return false
		}
	}
	return true
}

// hostBindMountsSliceMatch compares host bind mounts by source path, target
// path, and read-only flag.
func hostBindMountsSliceMatch(desired, actual []HostBindMount) bool {
	type key struct {
		src, tgt string
		ro       bool
	}
	toSet := func(mounts []HostBindMount) map[key]struct{} {
		s := make(map[key]struct{}, len(mounts))
		for _, m := range mounts {
			s[key{m.Source, m.Target, m.ReadOnly}] = struct{}{}
		}
		return s
	}
	ds := toSet(desired)
	as := toSet(actual)
	if len(ds) != len(as) {
		return false
	}
	for k := range ds {
		if _, ok := as[k]; !ok {
			return false
		}
	}
	return true
}

// sortedStringSlicesMatch compares two string slices after sorting, so order
// does not matter (e.g. linux capabilities may appear in different order).
func sortedStringSlicesMatch(a, b []string) bool {
	ca := sortedCopy(a)
	cb := sortedCopy(b)
	if len(ca) != len(cb) {
		return false
	}
	for i := range ca {
		if ca[i] != cb[i] {
			return false
		}
	}
	return true
}

func sortedCopy(s []string) []string {
	c := append([]string(nil), s...)
	sort.Strings(c)
	return c
}

func normalizeProto(p string) string {
	if p == "" {
		return "TCP"
	}
	return strings.ToUpper(p)
}
