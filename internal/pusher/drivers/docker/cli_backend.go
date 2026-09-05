package dockerdriver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	volumeSizeLabel       = "guardian.volume.size"
	volumeAccessModeLabel = "guardian.volume.accessMode"
	volumeEphemeralLabel  = "guardian.volume.ephemeral"
	inlineMountRoot       = "/guardian-inline"
)

type CLIBackend struct {
	docker   string
	stateDir string
}

type dockerConfigState struct {
	Name   string            `json:"name"`
	Hash   string            `json:"hash"`
	Labels map[string]string `json:"labels,omitempty"`
	Files  map[string]string `json:"files,omitempty"`
}

func NewCLIBackend(dockerBinary, stateDir string) (*CLIBackend, error) {
	if strings.TrimSpace(dockerBinary) == "" {
		dockerBinary = "docker"
	}
	if strings.TrimSpace(stateDir) == "" {
		stateDir = "/var/lib/guardian/pusher-docker"
	}
	resolved, err := exec.LookPath(dockerBinary)
	if err != nil {
		return nil, fmt.Errorf("locate docker binary: %w", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create docker state dir %q: %w", stateDir, err)
	}
	return &CLIBackend{docker: resolved, stateDir: stateDir}, nil
}

func (b *CLIBackend) EnsureNetwork(network Network) error {
	_, ok, err := b.GetNetwork(network.Name)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	driver := network.Driver
	if driver == "" {
		driver = "bridge"
	}
	args := []string{"network", "create", "--driver", driver}
	if network.Internal {
		args = append(args, "--internal")
	}
	args = append(args, labelArgs(network.Labels)...)
	args = append(args, network.Name)
	_, err = b.run(args...)
	return err
}

func (b *CLIBackend) GetNetwork(name string) (Network, bool, error) {
	out, ok, err := b.inspect("network", name)
	if err != nil || !ok {
		return Network{}, ok, err
	}
	var payload []struct {
		Name     string            `json:"Name"`
		Labels   map[string]string `json:"Labels"`
		Driver   string            `json:"Driver"`
		Internal bool              `json:"Internal"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return Network{}, false, fmt.Errorf("decode docker network inspect for %q: %w", name, err)
	}
	if len(payload) == 0 {
		return Network{}, false, nil
	}
	labels := cloneStringMap(payload[0].Labels)
	return Network{
		Name:     payload[0].Name,
		Hash:     labels["guardian.hash"],
		Labels:   labels,
		Driver:   payload[0].Driver,
		Internal: payload[0].Internal,
	}, true, nil
}

func (b *CLIBackend) DeleteNetwork(name string) error {
	_, err := b.runAllowNotFound("network", "rm", name)
	return err
}

func (b *CLIBackend) ConnectNetwork(networkName, containerName string, aliases []string) error {
	args := []string{"network", "connect"}
	for _, alias := range aliases {
		if alias == "" {
			continue
		}
		args = append(args, "--alias", alias)
	}
	args = append(args, networkName, containerName)
	_, err := b.run(args...)
	if dockerNetworkAlreadyConnected(err) {
		return nil
	}
	return err
}

func dockerNetworkAlreadyConnected(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists in network")
}

func (b *CLIBackend) UpsertVolume(volume Volume) error {
	current, ok, err := b.GetVolume(volume.Name)
	if err != nil {
		return err
	}
	if ok && current.Hash == volume.Hash && current.Size == volume.Size && current.AccessMode == volume.AccessMode && current.Ephemeral == volume.Ephemeral {
		return nil
	}
	if ok {
		if err := b.DeleteVolume(volume.Name); err != nil {
			return err
		}
	}
	labels := cloneStringMap(volume.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	labels[volumeSizeLabel] = volume.Size
	labels[volumeAccessModeLabel] = volume.AccessMode
	labels[volumeEphemeralLabel] = strconv.FormatBool(volume.Ephemeral)
	args := []string{"volume", "create"}
	args = append(args, labelArgs(labels)...)
	args = append(args, volume.Name)
	_, err = b.run(args...)
	return err
}

func (b *CLIBackend) GetVolume(name string) (Volume, bool, error) {
	out, ok, err := b.inspect("volume", name)
	if err != nil || !ok {
		return Volume{}, ok, err
	}
	var payload []struct {
		Name   string            `json:"Name"`
		Labels map[string]string `json:"Labels"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return Volume{}, false, fmt.Errorf("decode docker volume inspect for %q: %w", name, err)
	}
	if len(payload) == 0 {
		return Volume{}, false, nil
	}
	labels := cloneStringMap(payload[0].Labels)
	return Volume{
		Name:       payload[0].Name,
		Hash:       labels["guardian.hash"],
		Labels:     labels,
		Size:       labels[volumeSizeLabel],
		AccessMode: labels[volumeAccessModeLabel],
		Ephemeral:  strings.EqualFold(labels[volumeEphemeralLabel], "true"),
	}, true, nil
}

func (b *CLIBackend) DeleteVolume(name string) error {
	if err := b.detachManagedContainersFromVolume(name); err != nil {
		return err
	}
	_, err := b.runAllowNotFound("volume", "rm", "-f", name)
	return err
}

func (b *CLIBackend) detachManagedContainersFromVolume(volumeName string) error {
	out, err := b.run("ps", "-aq", "--filter", "volume="+volumeName)
	if err != nil {
		return err
	}
	ids := strings.Fields(strings.TrimSpace(string(out)))
	for _, id := range ids {
		container, ok, err := b.GetContainer(id)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if !strings.EqualFold(container.Labels["guardian.managed"], "true") {
			return fmt.Errorf("docker volume %q is in use by unmanaged container %q", volumeName, container.Name)
		}
		if err := b.DeleteContainer(container.Name); err != nil {
			return fmt.Errorf("remove container %q using volume %q: %w", container.Name, volumeName, err)
		}
	}
	return nil
}

func (b *CLIBackend) UpsertConfig(config Config) error {
	dir := b.configDir(config.Name)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove docker config dir %q: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create docker config dir %q: %w", dir, err)
	}
	if err := writeStateFiles(dir, config.Files); err != nil {
		return err
	}
	state := dockerConfigState{
		Name:   config.Name,
		Hash:   config.Hash,
		Labels: cloneStringMap(config.Labels),
		Files:  cloneStringMap(config.Files),
	}
	return writeJSON(filepath.Join(dir, "metadata.json"), state)
}

func (b *CLIBackend) GetConfig(name string) (Config, bool, error) {
	statePath := filepath.Join(b.configDir(name), "metadata.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, false, nil
		}
		return Config{}, false, fmt.Errorf("read docker config state %q: %w", statePath, err)
	}
	var state dockerConfigState
	if err := json.Unmarshal(data, &state); err != nil {
		return Config{}, false, fmt.Errorf("decode docker config state %q: %w", statePath, err)
	}
	return Config{
		Name:   state.Name,
		Hash:   state.Hash,
		Labels: cloneStringMap(state.Labels),
		Files:  cloneStringMap(state.Files),
	}, true, nil
}

func (b *CLIBackend) DeleteConfig(name string) error {
	if err := os.RemoveAll(b.configDir(name)); err != nil {
		return fmt.Errorf("remove docker config dir for %q: %w", name, err)
	}
	return nil
}

func (b *CLIBackend) UpsertContainer(container Container) error {
	current, ok, err := b.GetContainer(container.Name)
	if err != nil {
		return err
	}
	if ok && current.Hash == container.Hash && current.Running {
		return nil
	}
	if ok {
		if err := b.DeleteContainer(container.Name); err != nil {
			return err
		}
	}
	if err := b.checkPortConflicts(container); err != nil {
		return err
	}
	inlineDir, err := b.stageInlineFiles(container)
	if err != nil {
		return err
	}

	args := []string{"run", "-d", "--name", container.Name}
	if container.Network != "" {
		args = append(args, "--network", container.Network)
		for _, alias := range uniqueStrings(container.Aliases) {
			if alias == "" {
				continue
			}
			args = append(args, "--network-alias", alias)
		}
	}
	for _, host := range sortedStringKeys(container.ExtraHosts) {
		address := strings.TrimSpace(container.ExtraHosts[host])
		if strings.TrimSpace(host) == "" || address == "" {
			continue
		}
		args = append(args, "--add-host", fmt.Sprintf("%s:%s", host, address))
	}
	args = append(args, labelArgs(container.Labels)...)
	for _, binding := range container.Ports {
		if binding.HostPort <= 0 || binding.ContainerPort <= 0 {
			continue
		}
		mapping := fmt.Sprintf("%d:%d", binding.HostPort, binding.ContainerPort)
		if protocol := normalizeDockerProtocol(binding.Protocol); protocol != "tcp" {
			mapping += "/" + protocol
		}
		args = append(args, "-p", mapping)
	}
	for _, key := range sortedStringKeys(container.Env) {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, container.Env[key]))
	}
	if container.Privileged {
		args = append(args, "--privileged")
	}
	if container.ShmSize != "" {
		args = append(args, "--shm-size", container.ShmSize)
	}
	if container.GPUs != "" {
		args = append(args, "--gpus", container.GPUs)
	}
	for _, capability := range container.Capabilities {
		if capability == "" {
			continue
		}
		args = append(args, "--cap-add", capability)
	}
	for _, mount := range container.VolumeMounts {
		spec := []string{"type=volume", "source=" + mount.Source, "target=" + mount.Target}
		if mount.ReadOnly {
			spec = append(spec, "readonly")
		}
		args = append(args, "--mount", strings.Join(spec, ","))
	}
	for _, mount := range container.ConfigMounts {
		source, target, err := b.configMountPaths(mount)
		if err != nil {
			return err
		}
		spec := []string{"type=bind", "source=" + source, "target=" + target}
		if mount.ReadOnly {
			spec = append(spec, "readonly")
		}
		args = append(args, "--mount", strings.Join(spec, ","))
	}
	for _, mount := range container.HostBindMounts {
		if strings.TrimSpace(mount.Source) == "" || strings.TrimSpace(mount.Target) == "" {
			continue
		}
		spec := []string{"type=bind", "source=" + mount.Source, "target=" + mount.Target}
		if mount.ReadOnly {
			spec = append(spec, "readonly")
		}
		args = append(args, "--mount", strings.Join(spec, ","))
	}
	if inlineDir != "" {
		args = append(args, "--mount", strings.Join([]string{
			"type=bind",
			"source=" + inlineDir,
			"target=" + inlineMountRoot,
			"readonly",
		}, ","))
		for fileName, target := range defaultInlineMountTargets(container) {
			source, err := safeJoin(inlineDir, fileName)
			if err != nil {
				return err
			}
			spec := []string{"type=bind", "source=" + source, "target=" + target, "readonly"}
			args = append(args, "--mount", strings.Join(spec, ","))
		}
	}

	runArgs := append([]string(nil), container.Args...)
	if len(container.Command) > 0 {
		args = append(args, "--entrypoint", container.Command[0])
		runArgs = append(append([]string{}, container.Command[1:]...), runArgs...)
	}
	if cpu := k8sCPUToDocker(container.CPULimit); cpu != "" {
		args = append(args, "--cpus", cpu)
	}
	if mem := k8sMemoryToDocker(container.MemoryLimit); mem != "" {
		args = append(args, "--memory", mem)
	}
	if memRes := k8sMemoryToDocker(container.MemoryReservation); memRes != "" {
		args = append(args, "--memory-reservation", memRes)
	}
	args = append(args, container.Image)
	args = append(args, runArgs...)
	_, err = b.run(args...)
	return err
}

func (b *CLIBackend) GetContainer(name string) (Container, bool, error) {
	out, ok, err := b.inspect("container", name)
	if err != nil || !ok {
		return Container{}, ok, err
	}
	containers, err := parseContainerInspect(out)
	if err != nil {
		return Container{}, false, fmt.Errorf("docker container inspect for %q: %w", name, err)
	}
	if len(containers) == 0 {
		return Container{}, false, nil
	}
	return containers[0], true, nil
}

func (b *CLIBackend) DeleteContainer(name string) error {
	_, err := b.runAllowNotFound("container", "rm", "-f", name)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(b.inlineDir(name)); err != nil {
		return fmt.Errorf("remove docker inline dir for %q: %w", name, err)
	}
	return nil
}

func (b *CLIBackend) ListContainersByAsset(partition, intent, asset string) ([]Container, error) {
	args := []string{"ps", "-aq", "--filter", "label=guardian.partition=" + partition, "--filter", "label=guardian.intent=" + intent, "--filter", "label=guardian.asset=" + asset}
	out, err := b.run(args...)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(strings.TrimSpace(string(out)))
	if len(ids) == 0 {
		return nil, nil
	}
	containers := make([]Container, 0, len(ids))
	for _, id := range ids {
		container, ok, err := b.GetContainer(id)
		if err != nil {
			return nil, err
		}
		if ok {
			containers = append(containers, container)
		}
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })
	return containers, nil
}

func (b *CLIBackend) run(args ...string) ([]byte, error) {
	cmd := exec.Command(b.docker, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// checkPortConflicts verifies that none of the container's desired host ports
// are already bound by a different, running container. Guardian-unmanaged
// containers blocking a port are stopped automatically (with a log warning).
// Guardian-managed containers that still hold the port are a hard error.
func (b *CLIBackend) checkPortConflicts(container Container) error {
	for _, binding := range container.Ports {
		if binding.HostPort <= 0 {
			continue
		}
		port := strconv.Itoa(binding.HostPort)
		out, err := b.run("ps", "-q", "--filter", "publish="+port)
		if err != nil {
			// best-effort: skip if docker ps fails
			continue
		}
		ids := strings.Fields(strings.TrimSpace(string(out)))
		for _, id := range ids {
			c, ok, err := b.GetContainer(id)
			if err != nil || !ok {
				continue
			}
			if c.Name == container.Name {
				// same container (shouldn't happen after DeleteContainer, but safe)
				continue
			}
			// If the conflicting container is guardian-managed, that's a hard error —
			// it should have been deleted already.
			if c.Labels["guardian.managed"] == "true" {
				return fmt.Errorf("host port %s already bound by guardian-managed container %q; this is a bug", port, c.Name)
			}
			// Unmanaged container (e.g. bootstrap docker-compose service): stop it
			// so Guardian can take ownership of the port.
			fmt.Printf("guardian-pusher: stopping unmanaged container %q that is holding host port %s\n", c.Name, port)
			if _, err := b.run("stop", c.Name); err != nil {
				return fmt.Errorf("host port %s held by unmanaged container %q; failed to stop it: %w", port, c.Name, err)
			}
		}
	}
	return nil
}

func (b *CLIBackend) runAllowNotFound(args ...string) ([]byte, error) {
	cmd := exec.Command(b.docker, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if dockerNotFound(text) {
			return nil, nil
		}
		return nil, fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, text)
	}
	return out, nil
}

func (b *CLIBackend) inspect(kind, name string) ([]byte, bool, error) {
	cmd := exec.Command(b.docker, kind, "inspect", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if dockerNotFound(text) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("docker %s inspect %s: %w: %s", kind, name, err, text)
	}
	return out, true, nil
}

func (b *CLIBackend) configMountPaths(mount ConfigMount) (string, string, error) {
	root := b.configDir(mount.Config)
	if mount.SourcePath == "" {
		return root, mount.TargetPath, nil
	}
	source, err := safeJoin(root, mount.SourcePath)
	if err != nil {
		return "", "", err
	}
	return source, resolveFileTarget(mount.TargetPath, mount.SourcePath), nil
}

func (b *CLIBackend) stageInlineFiles(container Container) (string, error) {
	if len(container.InlineFiles) == 0 {
		if err := os.RemoveAll(b.inlineDir(container.Name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("remove docker inline dir for %q: %w", container.Name, err)
		}
		return "", nil
	}
	dir := b.inlineDir(container.Name)
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("reset docker inline dir %q: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create docker inline dir %q: %w", dir, err)
	}
	if err := writeStateFiles(dir, container.InlineFiles); err != nil {
		return "", err
	}
	return dir, nil
}

func (b *CLIBackend) configDir(name string) string {
	return filepath.Join(b.stateDir, "configs", name)
}

func (b *CLIBackend) inlineDir(name string) string {
	return filepath.Join(b.stateDir, "inline", name)
}

func writeStateFiles(root string, files map[string]string) error {
	for rel, content := range files {
		target, err := safeJoin(root, rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create state file dir for %q: %w", target, err)
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write state file %q: %w", target, err)
		}
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json for %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write json file %q: %w", path, err)
	}
	return nil
}

func safeJoin(root, rel string) (string, error) {
	clean := filepath.Clean(rel)
	if clean == "." || clean == string(filepath.Separator) {
		return root, nil
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid relative path %q", rel)
	}
	return filepath.Join(root, clean), nil
}

func resolveFileTarget(targetPath, sourcePath string) string {
	base := path.Base(sourcePath)
	cleanTarget := path.Clean(targetPath)
	if cleanTarget == "." || cleanTarget == "/" {
		return path.Join("/", base)
	}
	if path.Base(cleanTarget) == base || path.Ext(path.Base(cleanTarget)) != "" {
		return cleanTarget
	}
	return path.Join(cleanTarget, base)
}

func defaultInlineMountTargets(container Container) map[string]string {
	switch container.Kind {
	case "LoadBalancer":
		if _, ok := container.InlineFiles["haproxy.cfg"]; ok {
			return map[string]string{"haproxy.cfg": "/usr/local/etc/haproxy/haproxy.cfg"}
		}
	case "Observability":
		if _, ok := container.InlineFiles["config.yaml"]; ok {
			return map[string]string{"config.yaml": "/etc/otelcol/config.yaml"}
		}
	}
	return nil
}

func labelArgs(labels map[string]string) []string {
	if len(labels) == 0 {
		return nil
	}
	keys := sortedStringKeys(labels)
	args := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, labels[key]))
	}
	return args
}

func normalizeDockerProtocol(protocol string) string {
	if strings.EqualFold(strings.TrimSpace(protocol), "udp") {
		return "udp"
	}
	return "tcp"
}

func dockerNotFound(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "no such") || strings.Contains(message, "not found")
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// k8sCPUToDocker converts a k8s CPU string to Docker --cpus decimal format.
// "500m" → "0.5", "1000m" → "1", "2" → "2".
func k8sCPUToDocker(cpu string) string {
	cpu = strings.TrimSpace(cpu)
	if cpu == "" {
		return ""
	}
	if strings.HasSuffix(cpu, "m") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(cpu, "m"), 64)
		if err != nil {
			return cpu
		}
		return strconv.FormatFloat(n/1000.0, 'f', -1, 64)
	}
	return cpu
}

// k8sMemoryToDocker converts a k8s memory string to Docker --memory format (bytes).
// "512Mi" → "536870912", "1Gi" → "1073741824", "256M" → "256000000".
// Plain byte counts or docker-native values are passed through unchanged.
func k8sMemoryToDocker(mem string) string {
	mem = strings.TrimSpace(mem)
	if mem == "" {
		return ""
	}
	suffixMultipliers := []struct {
		suffix     string
		multiplier int64
	}{
		{"Ki", 1024},
		{"Mi", 1024 * 1024},
		{"Gi", 1024 * 1024 * 1024},
		{"K", 1000},
		{"M", 1000000},
		{"G", 1000000000},
	}
	for _, sm := range suffixMultipliers {
		if strings.HasSuffix(mem, sm.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSuffix(mem, sm.suffix), 64)
			if err != nil {
				return mem
			}
			return strconv.FormatInt(int64(n*float64(sm.multiplier)), 10)
		}
	}
	return mem
}
