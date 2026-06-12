package kubernetesdriver

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

type CLIBackend struct {
	kubectl     string
	kubeconfig  string
	contextName string
}

const fullHashAnnotation = "guardian.hash.full"

func NewCLIBackend(kubectlBinary, kubeconfig, contextName string) (*CLIBackend, error) {
	if strings.TrimSpace(kubectlBinary) == "" {
		kubectlBinary = "kubectl"
	}
	resolved, err := exec.LookPath(kubectlBinary)
	if err != nil {
		return nil, fmt.Errorf("locate kubectl binary: %w", err)
	}
	return &CLIBackend{kubectl: resolved, kubeconfig: kubeconfig, contextName: contextName}, nil
}

func (b *CLIBackend) UpsertConfigMap(cm ConfigMap) error {
	if err := b.ensureNamespace(cm.Namespace); err != nil {
		return err
	}
	return b.applyManifest(cm.Namespace, map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      cm.Name,
			"namespace": cm.Namespace,
			"labels":    cloneStringMap(cm.Labels),
			"annotations": map[string]string{
				fullHashAnnotation: cm.Hash,
			},
		},
		"data": cloneStringMap(cm.Data),
	})
}

func (b *CLIBackend) GetConfigMap(namespace, name string) (ConfigMap, bool, error) {
	raw, ok, err := b.getResource(namespace, "configmap", name)
	if err != nil || !ok {
		return ConfigMap{}, ok, err
	}
	var payload struct {
		Metadata struct {
			Name        string            `json:"name"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ConfigMap{}, false, fmt.Errorf("decode configmap %s/%s: %w", namespace, name, err)
	}
	hash := payload.Metadata.Annotations[fullHashAnnotation]
	if hash == "" {
		hash = payload.Metadata.Labels["guardian.hash"]
	}
	return ConfigMap{
		Namespace: namespace,
		Name:      payload.Metadata.Name,
		Hash:      hash,
		Labels:    cloneStringMap(payload.Metadata.Labels),
		Data:      cloneStringMap(payload.Data),
	}, true, nil
}

func (b *CLIBackend) DeleteConfigMap(namespace, name string) error {
	return b.deleteResource(namespace, "configmap", name)
}

func (b *CLIBackend) UpsertClaim(claim PersistentVolumeClaim) error {
	if err := b.ensureNamespace(claim.Namespace); err != nil {
		return err
	}
	spec := map[string]any{
		"accessModes": []string{firstNonEmpty(claim.AccessMode, "ReadWriteOnce")},
		"resources": map[string]any{
			"requests": map[string]string{
				"storage": firstNonEmpty(claim.Size, "1Gi"),
			},
		},
	}
	if claim.StorageClass != "" {
		spec["storageClassName"] = claim.StorageClass
	}
	return b.applyManifest(claim.Namespace, map[string]any{
		"apiVersion": "v1",
		"kind":       "PersistentVolumeClaim",
		"metadata": map[string]any{
			"name":      claim.Name,
			"namespace": claim.Namespace,
			"labels":    cloneStringMap(claim.Labels),
			"annotations": map[string]string{
				fullHashAnnotation: claim.Hash,
			},
		},
		"spec": spec,
	})
}

func (b *CLIBackend) GetClaim(namespace, name string) (PersistentVolumeClaim, bool, error) {
	raw, ok, err := b.getResource(namespace, "persistentvolumeclaim", name)
	if err != nil || !ok {
		return PersistentVolumeClaim{}, ok, err
	}
	var payload struct {
		Metadata struct {
			Name        string            `json:"name"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			AccessModes      []string `json:"accessModes"`
			StorageClassName string   `json:"storageClassName"`
			Resources        struct {
				Requests map[string]string `json:"requests"`
			} `json:"resources"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return PersistentVolumeClaim{}, false, fmt.Errorf("decode pvc %s/%s: %w", namespace, name, err)
	}
	accessMode := ""
	if len(payload.Spec.AccessModes) > 0 {
		accessMode = payload.Spec.AccessModes[0]
	}
	hash := payload.Metadata.Annotations[fullHashAnnotation]
	if hash == "" {
		hash = payload.Metadata.Labels["guardian.hash"]
	}
	return PersistentVolumeClaim{
		Namespace:    namespace,
		Name:         payload.Metadata.Name,
		Hash:         hash,
		Labels:       cloneStringMap(payload.Metadata.Labels),
		Size:         payload.Spec.Resources.Requests["storage"],
		AccessMode:   accessMode,
		StorageClass: payload.Spec.StorageClassName,
	}, true, nil
}

func (b *CLIBackend) DeleteClaim(namespace, name string) error {
	return b.deleteResource(namespace, "persistentvolumeclaim", name)
}

func (b *CLIBackend) UpsertDeployment(deployment Deployment) error {
	if err := b.ensureNamespace(deployment.Namespace); err != nil {
		return err
	}
	items := []map[string]any{}
	volumeItems, volumeMounts := b.containerVolumes(deployment)
	if len(deployment.Container.InlineFiles) > 0 {
		items = append(items, map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      inlineConfigMapName(deployment.Name),
				"namespace": deployment.Namespace,
				"labels":    cloneStringMap(deployment.Labels),
			},
			"data": cloneStringMap(deployment.Container.InlineFiles),
		})
	}
	selector := selectorForLabels(deployment.Labels)
	replicas := deployment.Replicas
	if replicas < 1 {
		replicas = 1
	}
	containerSpec := map[string]any{
		"name":            firstNonEmpty(deployment.Container.Name, deployment.Name),
		"image":           deployment.Container.Image,
		"env":             envList(deployment.Container.Env),
		"ports":           containerPorts(deployment.Container.Ports),
		"volumeMounts":    volumeMounts,
		"securityContext": containerSecurityContext(deployment.Container),
	}
	if len(deployment.Container.Command) > 0 {
		containerSpec["command"] = append([]string(nil), deployment.Container.Command...)
	}
	if len(deployment.Container.Args) > 0 {
		containerSpec["args"] = append([]string(nil), deployment.Container.Args...)
	}
	if deployment.Container.ImagePullPolicy != "" {
		containerSpec["imagePullPolicy"] = deployment.Container.ImagePullPolicy
	}
	if probe := deployment.Container.ReadinessProbe; probe != nil {
		containerSpec["readinessProbe"] = probeSpec(probe)
	}
	if r := deployment.Container.Resources; r.CPURequest != "" || r.CPULimit != "" || r.MemoryRequest != "" || r.MemoryLimit != "" {
		resources := map[string]any{}
		if req := map[string]string{}; r.CPURequest != "" || r.MemoryRequest != "" {
			if r.CPURequest != "" {
				req["cpu"] = r.CPURequest
			}
			if r.MemoryRequest != "" {
				req["memory"] = r.MemoryRequest
			}
			resources["requests"] = req
		}
		if lim := map[string]string{}; r.CPULimit != "" || r.MemoryLimit != "" {
			if r.CPULimit != "" {
				lim["cpu"] = r.CPULimit
			}
			if r.MemoryLimit != "" {
				lim["memory"] = r.MemoryLimit
			}
			resources["limits"] = lim
		}
		containerSpec["resources"] = resources
	}
	items = append(items, map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      deployment.Name,
			"namespace": deployment.Namespace,
			"labels":    cloneStringMap(deployment.Labels),
			"annotations": map[string]string{
				fullHashAnnotation: deployment.Hash,
			},
		},
		"spec": map[string]any{
			"replicas": replicas,
			"selector": map[string]any{
				"matchLabels": selector,
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": cloneStringMap(deployment.Labels),
				},
				"spec": func() map[string]any {
					podSpec := map[string]any{
						"containers": []map[string]any{containerSpec},
						"volumes":    volumeItems,
					}
					if deployment.ServiceAccountName != "" {
						podSpec["serviceAccountName"] = deployment.ServiceAccountName
					}
					return podSpec
				}(),
			},
		},
	})
	return b.applyManifest(deployment.Namespace, map[string]any{
		"apiVersion": "v1",
		"kind":       "List",
		"items":      items,
	})
}

func (b *CLIBackend) GetDeployment(namespace, name string) (Deployment, bool, error) {
	raw, ok, err := b.getResource(namespace, "deployment", name)
	if err != nil || !ok {
		return Deployment{}, ok, err
	}
	var payload struct {
		Metadata struct {
			Name        string            `json:"name"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			Replicas *int `json:"replicas"`
			Template struct {
				Spec struct {
					Containers []struct {
						Name            string `json:"name"`
						Image           string `json:"image"`
						ImagePullPolicy string `json:"imagePullPolicy"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
		Status struct {
			ReadyReplicas     int `json:"readyReplicas"`
			AvailableReplicas int `json:"availableReplicas"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Deployment{}, false, fmt.Errorf("decode deployment %s/%s: %w", namespace, name, err)
	}
	container := Container{}
	if len(payload.Spec.Template.Spec.Containers) > 0 {
		container.Name = payload.Spec.Template.Spec.Containers[0].Name
		container.Image = payload.Spec.Template.Spec.Containers[0].Image
		container.ImagePullPolicy = payload.Spec.Template.Spec.Containers[0].ImagePullPolicy
	}
	replicas := 1
	if payload.Spec.Replicas != nil && *payload.Spec.Replicas > 0 {
		replicas = *payload.Spec.Replicas
	}
	labels := cloneStringMap(payload.Metadata.Labels)
	var podFailureReason string
	if payload.Status.ReadyReplicas < replicas {
		podFailureReason = b.podsTerminalFailure(namespace, labels)
	}
	hash := payload.Metadata.Annotations[fullHashAnnotation]
	if hash == "" {
		hash = payload.Metadata.Labels["guardian.hash"]
	}
	return Deployment{
		Namespace:         namespace,
		Name:              payload.Metadata.Name,
		Hash:              hash,
		Labels:            labels,
		Replicas:          replicas,
		ReadyReplicas:     payload.Status.ReadyReplicas,
		AvailableReplicas: payload.Status.AvailableReplicas,
		Container:         container,
		CrashLoopBackOff:  podFailureReason != "",
		PodFailureReason:  podFailureReason,
	}, true, nil
}

// transientPodWaitingReasons are waiting states that represent normal startup
// progress and will resolve on their own. Any other non-empty waiting reason
// is treated as a terminal failure requiring user intervention.
var transientPodWaitingReasons = map[string]bool{
	"ContainerCreating": true,
	"PodInitializing":   true,
	"Init:0/1":          true,
	"Pending":           true,
	"Scheduled":         true,
}

// podsTerminalFailure returns the failure reason if any pod owned by a
// deployment (matched via guardian labels) has a container stuck in a
// non-transient waiting state. Any waiting reason that is not in the known
// transient allowlist is considered a terminal failure.
// Returns an empty string when all pods are healthy or transiently starting.
func (b *CLIBackend) podsTerminalFailure(namespace string, labels map[string]string) string {
	sel := selectorForLabels(labels)
	parts := make([]string, 0, len(sel))
	for k, v := range sel {
		parts = append(parts, k+"="+v)
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	selector := strings.Join(parts, ",")
	args := b.baseArgs()
	args = append(args, "-n", namespace, "get", "pods", "-l", selector, "-o", "json")
	raw, err := b.run(args...)
	if err != nil {
		return ""
	}
	var podList struct {
		Items []struct {
			Status struct {
				ContainerStatuses []struct {
					State struct {
						Waiting *struct {
							Reason string `json:"reason"`
						} `json:"waiting"`
						Running *struct{} `json:"running"`
					} `json:"state"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &podList); err != nil {
		return ""
	}
	for _, pod := range podList.Items {
		for _, cs := range pod.Status.ContainerStatuses {
			w := cs.State.Waiting
			if w == nil {
				// running or terminated — not a transient wait, not a failure
				continue
			}
			if w.Reason == "" || transientPodWaitingReasons[w.Reason] {
				// known-good transient state
				continue
			}
			// anything else (ImagePullBackOff, CrashLoopBackOff, CreateContainerConfigError, …)
			return w.Reason
		}
	}
	return ""
}

// podsCrashLoopBackOff is kept for backward compatibility; prefer podsTerminalFailure.
func (b *CLIBackend) podsCrashLoopBackOff(namespace string, labels map[string]string) bool {
	return b.podsTerminalFailure(namespace, labels) != ""
}

func (b *CLIBackend) DeleteDeployment(namespace, name string) error {
	if err := b.deleteResource(namespace, "deployment", name); err != nil {
		return err
	}
	return b.deleteResource(namespace, "configmap", inlineConfigMapName(name))
}

func (b *CLIBackend) UpsertService(service Service) error {
	if err := b.ensureNamespace(service.Namespace); err != nil {
		return err
	}
	annotations := cloneStringMap(service.Annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[fullHashAnnotation] = service.Hash
	meta := map[string]any{
		"name":        service.Name,
		"namespace":   service.Namespace,
		"labels":      cloneStringMap(service.Labels),
		"annotations": annotations,
	}
	return b.applyManifest(service.Namespace, map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   meta,
		"spec": map[string]any{
			"type":     firstNonEmpty(service.Type, "ClusterIP"),
			"selector": cloneStringMap(service.Selector),
			"ports":    servicePorts(service.Ports),
		},
	})
}

func (b *CLIBackend) GetService(namespace, name string) (Service, bool, error) {
	raw, ok, err := b.getResource(namespace, "service", name)
	if err != nil || !ok {
		return Service{}, ok, err
	}
	var payload struct {
		Metadata struct {
			Name        string            `json:"name"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			Type     string            `json:"type"`
			Selector map[string]string `json:"selector"`
			Ports    []struct {
				Name       string      `json:"name"`
				Protocol   string      `json:"protocol"`
				Port       int         `json:"port"`
				TargetPort interface{} `json:"targetPort"`
			} `json:"ports"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Service{}, false, fmt.Errorf("decode service %s/%s: %w", namespace, name, err)
	}
	ports := make([]ServicePort, 0, len(payload.Spec.Ports))
	for _, port := range payload.Spec.Ports {
		ports = append(ports, ServicePort{
			Name:       port.Name,
			Protocol:   port.Protocol,
			Port:       port.Port,
			TargetPort: parseTargetPort(port.TargetPort, port.Port),
		})
	}
	hash := payload.Metadata.Annotations[fullHashAnnotation]
	if hash == "" {
		hash = payload.Metadata.Labels["guardian.hash"]
	}
	return Service{
		Namespace:   namespace,
		Name:        payload.Metadata.Name,
		Hash:        hash,
		Type:        payload.Spec.Type,
		Labels:      cloneStringMap(payload.Metadata.Labels),
		Annotations: cloneStringMap(payload.Metadata.Annotations),
		Selector:    cloneStringMap(payload.Spec.Selector),
		Ports:       ports,
	}, true, nil
}

func (b *CLIBackend) DeleteService(namespace, name string) error {
	return b.deleteResource(namespace, "service", name)
}

func (b *CLIBackend) applyManifest(namespace string, manifest any) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode kubernetes manifest for namespace %q: %w", namespace, err)
	}
	_, err = b.runWithInput(data, append(b.baseArgs(), "apply", "-f", "-")...)
	return err
}

func (b *CLIBackend) getResource(namespace, resourceType, name string) ([]byte, bool, error) {
	args := b.baseArgs()
	args = append(args, "-n", namespace, "get", resourceType, name, "-o", "json")
	out, err := b.run(args...)
	if err != nil {
		if kubectlNotFound(err.Error()) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return out, true, nil
}

func (b *CLIBackend) deleteResource(namespace, resourceType, name string) error {
	args := b.baseArgs()
	args = append(args, "-n", namespace, "delete", resourceType, name, "--ignore-not-found=true")
	_, err := b.run(args...)
	return err
}

func (b *CLIBackend) ensureNamespace(namespace string) error {
	if namespace == "" || namespace == "default" {
		return nil
	}
	args := b.baseArgs()
	args = append(args, "get", "namespace", namespace, "-o", "json")
	if _, err := b.run(args...); err == nil {
		return nil
	} else if !kubectlNotFound(err.Error()) {
		return err
	}
	createArgs := b.baseArgs()
	createArgs = append(createArgs, "create", "namespace", namespace)
	_, err := b.run(createArgs...)
	if err != nil && !strings.Contains(err.Error(), "AlreadyExists") {
		return err
	}
	return nil
}

func (b *CLIBackend) run(args ...string) ([]byte, error) {
	cmd := exec.Command(b.kubectl, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (b *CLIBackend) runWithInput(input []byte, args ...string) ([]byte, error) {
	cmd := exec.Command(b.kubectl, args...)
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (b *CLIBackend) baseArgs() []string {
	args := make([]string, 0, 4)
	if strings.TrimSpace(b.kubeconfig) != "" {
		args = append(args, "--kubeconfig", b.kubeconfig)
	}
	if strings.TrimSpace(b.contextName) != "" {
		args = append(args, "--context", b.contextName)
	}
	return args
}

func (b *CLIBackend) containerVolumes(deployment Deployment) ([]map[string]any, []map[string]any) {
	volumes := make([]map[string]any, 0, len(deployment.Container.VolumeMounts)+1)
	mounts := make([]map[string]any, 0, len(deployment.Container.VolumeMounts))
	inlineName := inlineConfigMapName(deployment.Name)
	for idx, mount := range deployment.Container.VolumeMounts {
		name := fmt.Sprintf("vol-%d", idx)
		volume := map[string]any{"name": name}
		switch mount.SourceKind {
		case "ConfigMap":
			volume["configMap"] = map[string]any{"name": mount.SourceName}
		case "PersistentVolumeClaim":
			volume["persistentVolumeClaim"] = map[string]any{"claimName": mount.SourceName}
		case "HostPath":
			volume["hostPath"] = map[string]any{"path": mount.SourceName}
		case "EmptyDir":
			volume["emptyDir"] = map[string]any{}
		case "InlineFile":
			volume["configMap"] = map[string]any{"name": inlineName}
		default:
			continue
		}
		volumes = append(volumes, volume)
		mountSpec := map[string]any{
			"name":      name,
			"mountPath": mount.MountPath,
			"readOnly":  mount.ReadOnly,
		}
		if mount.SubPath != "" {
			mountSpec["subPath"] = mount.SubPath
		} else if mount.SourceKind == "InlineFile" && mount.SourceName != "" {
			mountSpec["subPath"] = mount.SourceName
		}
		mounts = append(mounts, mountSpec)
	}
	return volumes, mounts
}

func envList(env map[string]string) []map[string]string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, map[string]string{"name": key, "value": env[key]})
	}
	return out
}

func containerPorts(ports []ServicePort) []map[string]any {
	if len(ports) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(ports))
	for _, port := range ports {
		containerPort := port.TargetPort
		if containerPort == 0 {
			containerPort = port.Port
		}
		entry := map[string]any{
			"containerPort": containerPort,
			"protocol":      firstNonEmpty(port.Protocol, "TCP"),
		}
		if port.Name != "" {
			entry["name"] = port.Name
		}
		if port.HostPort > 0 {
			entry["hostPort"] = port.HostPort
		}
		out = append(out, entry)
	}
	return out
}

func servicePorts(ports []ServicePort) []map[string]any {
	if len(ports) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(ports))
	for _, port := range ports {
		targetPort := port.TargetPort
		if targetPort == 0 {
			targetPort = port.Port
		}
		entry := map[string]any{
			"port":       port.Port,
			"targetPort": targetPort,
			"protocol":   firstNonEmpty(port.Protocol, "TCP"),
		}
		if port.Name != "" {
			entry["name"] = port.Name
		}
		out = append(out, entry)
	}
	return out
}

func containerSecurityContext(container Container) map[string]any {
	if !container.Privileged && len(container.Capabilities) == 0 {
		return nil
	}
	ctx := map[string]any{}
	if container.Privileged {
		ctx["privileged"] = true
	}
	if len(container.Capabilities) > 0 {
		ctx["capabilities"] = map[string]any{"add": append([]string(nil), container.Capabilities...)}
	}
	return ctx
}

func probeSpec(probe *Probe) map[string]any {
	if probe == nil {
		return nil
	}
	out := map[string]any{}
	if probe.TCPSocket != nil && probe.TCPSocket.Port > 0 {
		out["tcpSocket"] = map[string]any{"port": probe.TCPSocket.Port}
	}
	if probe.HTTPGet != nil && probe.HTTPGet.Port > 0 {
		httpGet := map[string]any{"port": probe.HTTPGet.Port}
		if probe.HTTPGet.Path != "" {
			httpGet["path"] = probe.HTTPGet.Path
		}
		if probe.HTTPGet.Scheme != "" {
			httpGet["scheme"] = probe.HTTPGet.Scheme
		}
		out["httpGet"] = httpGet
	}
	if probe.InitialDelaySeconds > 0 {
		out["initialDelaySeconds"] = probe.InitialDelaySeconds
	}
	if probe.PeriodSeconds > 0 {
		out["periodSeconds"] = probe.PeriodSeconds
	}
	if probe.TimeoutSeconds > 0 {
		out["timeoutSeconds"] = probe.TimeoutSeconds
	}
	if probe.SuccessThreshold > 0 {
		out["successThreshold"] = probe.SuccessThreshold
	}
	if probe.FailureThreshold > 0 {
		out["failureThreshold"] = probe.FailureThreshold
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func selectorForLabels(labels map[string]string) map[string]string {
	selector := map[string]string{
		"guardian.managed": "true",
	}
	for _, key := range []string{"guardian.partition", "guardian.intent", "guardian.asset"} {
		if value := labels[key]; value != "" {
			selector[key] = value
		}
	}
	return selector
}

func parseTargetPort(value interface{}, fallback int) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case string:
		if parsed, err := strconv.Atoi(typed); err == nil {
			return parsed
		}
	}
	return fallback
}

func inlineConfigMapName(name string) string {
	const suffix = "-inline"
	if len(name)+len(suffix) <= 63 {
		return name + suffix
	}
	sum := fmt.Sprintf("%x", sha1.Sum([]byte(name)))[:8]
	keep := 63 - len(suffix) - 1 - len(sum)
	if keep < 1 {
		keep = 1
	}
	return strings.TrimRight(name[:keep], "-") + "-" + sum + suffix
}

func kubectlNotFound(message string) bool {
	return strings.Contains(message, "NotFound") || strings.Contains(message, "not found")
}
