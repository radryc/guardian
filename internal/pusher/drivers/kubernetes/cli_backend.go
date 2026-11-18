package kubernetesdriver

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"log"
	"os"
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

func (b *CLIBackend) UpsertConfigMap(ctx context.Context, cm ConfigMap) error {
	if err := b.ensureNamespace(ctx, cm.Namespace); err != nil {
		return err
	}
	return b.applyManifest(ctx, cm.Namespace, configMapManifest(cm))
}

func (b *CLIBackend) GetConfigMap(ctx context.Context, namespace, name string) (ConfigMap, bool, error) {
	raw, ok, err := b.getResource(ctx, namespace, "configmap", name)
	if err != nil || !ok {
		return ConfigMap{}, ok, err
	}
	cm, err := decodeConfigMap(namespace, raw)
	if err != nil {
		return ConfigMap{}, false, err
	}
	return cm, true, nil
}

func (b *CLIBackend) DeleteConfigMap(ctx context.Context, namespace, name string) error {
	return b.deleteResource(ctx, namespace, "configmap", name)
}

func (b *CLIBackend) UpsertClaim(ctx context.Context, claim PersistentVolumeClaim) error {
	if err := b.ensureNamespace(ctx, claim.Namespace); err != nil {
		return err
	}
	return b.applyManifest(ctx, claim.Namespace, claimManifest(claim))
}

func (b *CLIBackend) GetClaim(ctx context.Context, namespace, name string) (PersistentVolumeClaim, bool, error) {
	raw, ok, err := b.getResource(ctx, namespace, "persistentvolumeclaim", name)
	if err != nil || !ok {
		return PersistentVolumeClaim{}, ok, err
	}
	claim, err := decodeClaim(namespace, raw)
	if err != nil {
		return PersistentVolumeClaim{}, false, err
	}
	return claim, true, nil
}

func (b *CLIBackend) DeleteClaim(ctx context.Context, namespace, name string) error {
	return b.deleteResource(ctx, namespace, "persistentvolumeclaim", name)
}

func (b *CLIBackend) UpsertDeployment(ctx context.Context, deployment Deployment) error {
	if err := b.ensureNamespace(ctx, deployment.Namespace); err != nil {
		return err
	}
	items := deploymentManifestItems(deployment)
	for _, item := range items {
		if kind, _ := item["kind"].(string); kind == "Deployment" {
			if err := b.clearDeploymentEnvConflicts(ctx, deployment.Namespace, item); err != nil {
				return err
			}
		}
	}
	return b.applyManifest(ctx, deployment.Namespace, listManifest(items))
}

// clearDeploymentEnvConflicts mirrors the client-go backend: stale valueFrom
// fields on live env vars are replaced with Guardian's literal value before
// apply, since kubectl apply also preserves fields it does not set.
func (b *CLIBackend) clearDeploymentEnvConflicts(ctx context.Context, namespace string, desired map[string]any) error {
	metadata, _ := desired["metadata"].(map[string]any)
	name, _ := metadata["name"].(string)
	if name == "" {
		return nil
	}
	raw, ok, err := b.getResource(ctx, namespace, "deployment", name)
	if err != nil || !ok {
		return err
	}
	var live map[string]any
	if err := json.Unmarshal(raw, &live); err != nil {
		return fmt.Errorf("decode live Deployment/%s: %w", name, err)
	}
	desiredNormalized, err := normalizeManifest(desired)
	if err != nil {
		return err
	}
	patch, err := deploymentEnvConflictPatch(desiredNormalized, live)
	if err != nil {
		return err
	}
	if len(patch) == 0 {
		return nil
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("encode env conflict patch for Deployment/%s: %w", name, err)
	}
	file, err := os.CreateTemp("", "guardian-env-patch-*.json")
	if err != nil {
		return fmt.Errorf("create env conflict patch for Deployment/%s: %w", name, err)
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("protect env conflict patch for Deployment/%s: %w", name, err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write env conflict patch for Deployment/%s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write env conflict patch for Deployment/%s: %w", name, err)
	}
	args := b.baseArgs()
	args = append(args, "-n", namespace, "patch", "deployment", name, "--type=json", "--patch-file", path)
	if _, err := b.run(ctx, args...); err != nil {
		return fmt.Errorf("clear env conflicts on Deployment/%s: %w", name, err)
	}
	log.Printf("[Kubernetes] cleared %d stale env valueFrom field(s) on Deployment %s/%s", len(patch), namespace, name)
	return nil
}

func (b *CLIBackend) GetDeployment(ctx context.Context, namespace, name string) (Deployment, bool, error) {
	raw, ok, err := b.getResource(ctx, namespace, "deployment", name)
	if err != nil || !ok {
		return Deployment{}, ok, err
	}
	deployment, err := decodeDeployment(namespace, raw)
	if err != nil {
		return Deployment{}, false, err
	}
	if deployment.ReadyReplicas < deployment.Replicas {
		reason, message, podName := b.podsTerminalFailure(ctx, namespace, deployment.Labels)
		deployment.CrashLoopBackOff = reason != ""
		deployment.PodFailureReason = reason
		deployment.PodFailureMessage = message
		deployment.PodFailurePodName = podName
	}
	return deployment, true, nil
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
func (b *CLIBackend) podsTerminalFailure(ctx context.Context, namespace string, labels map[string]string) (reason, message, podName string) {
	sel := selectorForLabels(labels)
	parts := make([]string, 0, len(sel))
	for k, v := range sel {
		parts = append(parts, k+"="+v)
	}
	if len(parts) == 0 {
		return "", "", ""
	}
	sort.Strings(parts)
	selector := strings.Join(parts, ",")
	args := b.baseArgs()
	args = append(args, "-n", namespace, "get", "pods", "-l", selector, "-o", "json")
	raw, err := b.run(ctx, args...)
	if err != nil {
		return "", "", ""
	}
	var podList struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
					Reason string `json:"reason"`
				} `json:"conditions"`
				ContainerStatuses []struct {
					State struct {
						Waiting *struct {
							Reason  string `json:"reason"`
							Message string `json:"message"`
						} `json:"waiting"`
						Running *struct{} `json:"running"`
					} `json:"state"`
				} `json:"containerStatuses"`
				InitContainerStatuses []struct {
					State struct {
						Waiting *struct {
							Reason  string `json:"reason"`
							Message string `json:"message"`
						} `json:"waiting"`
						Running *struct{} `json:"running"`
					} `json:"state"`
				} `json:"initContainerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &podList); err != nil {
		return "", "", ""
	}
	for _, pod := range podList.Items {
		r, m, found := b.containerWaitingFailure(pod.Status.ContainerStatuses, pod.Metadata.Name)
		if found {
			return r, m, pod.Metadata.Name
		}
		r, m, found = b.containerWaitingFailure(pod.Status.InitContainerStatuses, pod.Metadata.Name)
		if found {
			return r, m, pod.Metadata.Name
		}
	}
	return "", "", ""
}

func (b *CLIBackend) containerWaitingFailure(statuses []struct {
	State struct {
		Waiting *struct {
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"waiting"`
		Running *struct{} `json:"running"`
	} `json:"state"`
}, podName string) (reason, message string, found bool) {
	for _, cs := range statuses {
		w := cs.State.Waiting
		if w == nil {
			continue
		}
		if w.Reason == "" || transientPodWaitingReasons[w.Reason] {
			continue
		}
		return w.Reason, w.Message, true
	}
	return "", "", false
}

func (b *CLIBackend) GetPodEvents(ctx context.Context, namespace, podName string) ([]string, error) {
	if podName == "" {
		return nil, nil
	}
	args := b.baseArgs()
	args = append(args, "-n", namespace, "get", "events", "--field-selector", "involvedObject.name="+podName, "-o", "json")
	raw, err := b.run(ctx, args...)
	if err != nil {
		return nil, nil
	}
	var evList struct {
		Items []struct {
			Type          string `json:"type"`
			Reason        string `json:"reason"`
			Message       string `json:"message"`
			LastTimestamp string `json:"lastTimestamp"`
			Count         int    `json:"count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &evList); err != nil {
		return nil, nil
	}
	entries := make([]string, 0, len(evList.Items))
	for _, ev := range evList.Items {
		msg := strings.TrimSpace(ev.Message)
		if msg == "" {
			continue
		}
		line := ev.Reason
		if ev.Type != "" {
			line = ev.Type + " " + ev.Reason
		}
		if ev.Count > 1 {
			line += fmt.Sprintf(" (x%d)", ev.Count)
		}
		line += ": " + msg
		if ev.LastTimestamp != "" {
			line += " (" + ev.LastTimestamp + ")"
		}
		entries = append(entries, line)
	}
	return entries, nil
}

func (b *CLIBackend) DeleteDeployment(ctx context.Context, namespace, name string) error {
	if err := b.deleteResource(ctx, namespace, "deployment", name); err != nil {
		return err
	}
	return b.deleteResource(ctx, namespace, "configmap", inlineConfigMapName(name))
}

func (b *CLIBackend) UpsertService(ctx context.Context, service Service) error {
	if err := b.ensureNamespace(ctx, service.Namespace); err != nil {
		return err
	}
	return b.applyManifest(ctx, service.Namespace, serviceManifest(service))
}

func (b *CLIBackend) GetService(ctx context.Context, namespace, name string) (Service, bool, error) {
	raw, ok, err := b.getResource(ctx, namespace, "service", name)
	if err != nil || !ok {
		return Service{}, ok, err
	}
	service, err := decodeService(namespace, raw)
	if err != nil {
		return Service{}, false, err
	}
	return service, true, nil
}

func (b *CLIBackend) DeleteService(ctx context.Context, namespace, name string) error {
	return b.deleteResource(ctx, namespace, "service", name)
}

func (b *CLIBackend) UpsertNetworkPolicy(ctx context.Context, policy NetworkPolicy) error {
	if err := b.ensureNamespace(ctx, policy.Namespace); err != nil {
		return err
	}
	return b.applyManifest(ctx, policy.Namespace, networkPolicyManifest(policy))
}

func (b *CLIBackend) GetNetworkPolicy(ctx context.Context, namespace, name string) (NetworkPolicy, bool, error) {
	raw, ok, err := b.getResource(ctx, namespace, "networkpolicy", name)
	if err != nil || !ok {
		return NetworkPolicy{}, ok, err
	}
	policy, err := decodeNetworkPolicy(namespace, raw)
	if err != nil {
		return NetworkPolicy{}, false, err
	}
	return policy, true, nil
}

func (b *CLIBackend) DeleteNetworkPolicy(ctx context.Context, namespace, name string) error {
	return b.deleteResource(ctx, namespace, "networkpolicy", name)
}

// ApplyManifest applies a raw (possibly multi-document) Kubernetes manifest
// supplied as YAML bytes. When namespace is non-empty it is passed to kubectl,
// otherwise each document's own namespace is honoured.
func (b *CLIBackend) ApplyManifest(ctx context.Context, namespace string, manifest []byte) error {
	args := b.baseArgs()
	if strings.TrimSpace(namespace) != "" {
		args = append(args, "-n", namespace)
	}
	args = append(args, "apply", "-f", "-")
	_, err := b.runWithInput(ctx, manifest, args...)
	return err
}

func (b *CLIBackend) applyManifest(ctx context.Context, namespace string, manifest any) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode kubernetes manifest for namespace %q: %w", namespace, err)
	}
	_, err = b.runWithInput(ctx, data, append(b.baseArgs(), "apply", "-f", "-")...)
	return err
}

func (b *CLIBackend) getResource(ctx context.Context, namespace, resourceType, name string) ([]byte, bool, error) {
	args := b.baseArgs()
	args = append(args, "-n", namespace, "get", resourceType, name, "-o", "json")
	out, err := b.run(ctx, args...)
	if err != nil {
		if kubectlNotFound(err.Error()) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return out, true, nil
}

func (b *CLIBackend) deleteResource(ctx context.Context, namespace, resourceType, name string) error {
	args := b.baseArgs()
	args = append(args, "-n", namespace, "delete", resourceType, name, "--ignore-not-found=true")
	_, err := b.run(ctx, args...)
	return err
}

func (b *CLIBackend) ensureNamespace(ctx context.Context, namespace string) error {
	if namespace == "" || namespace == "default" {
		return nil
	}
	args := b.baseArgs()
	args = append(args, "get", "namespace", namespace, "-o", "json")
	if _, err := b.run(ctx, args...); err == nil {
		return nil
	} else if !kubectlNotFound(err.Error()) {
		return err
	}
	createArgs := b.baseArgs()
	createArgs = append(createArgs, "create", "namespace", namespace)
	_, err := b.run(ctx, createArgs...)
	if err != nil && !strings.Contains(err.Error(), "AlreadyExists") {
		return err
	}
	return nil
}

func (b *CLIBackend) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, b.kubectl, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (b *CLIBackend) runWithInput(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, b.kubectl, args...)
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
	if !container.Privileged && len(container.Capabilities) == 0 && container.RunAsUser == nil {
		return nil
	}
	ctx := map[string]any{}
	if container.Privileged {
		ctx["privileged"] = true
	}
	if len(container.Capabilities) > 0 {
		ctx["capabilities"] = map[string]any{"add": append([]string(nil), container.Capabilities...)}
	}
	if container.RunAsUser != nil {
		ctx["runAsUser"] = *container.RunAsUser
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

var _ BackendAPI = (*CLIBackend)(nil)
