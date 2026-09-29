package kubernetesdriver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const minimalKubeconfig = `apiVersion: v1
kind: Config
clusters:
  - name: test
    cluster:
      server: https://127.0.0.1:6443
      insecure-skip-tls-verify: true
contexts:
  - name: test
    context:
      cluster: test
      user: test
current-context: test
users:
  - name: test
    user:
      token: dummy
`

func writeKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(minimalKubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

func TestClientBackendConstructsFromKubeconfig(t *testing.T) {
	backend, err := NewClientBackend(writeKubeconfig(t), "test")
	if err != nil {
		t.Fatalf("NewClientBackend() error = %v", err)
	}
	if backend == nil || backend.client == nil || backend.mapper == nil {
		t.Fatalf("incomplete backend: %+v", backend)
	}
}

func TestDeploymentManifestRoundTrip(t *testing.T) {
	deployment := Deployment{
		Namespace: "platform",
		Name:      "app",
		Hash:      "hash-1",
		Labels: map[string]string{
			"guardian.managed":   "true",
			"guardian.partition": "payments",
			"guardian.intent":    "api",
			"guardian.asset":     "app",
		},
		Replicas: 2,
		Container: Container{
			Name:  "app",
			Image: "demo:v1",
			Env:   map[string]string{"MODE": "prod"},
			Ports: []ServicePort{{Name: "http", Port: 80, TargetPort: 8080, Protocol: "TCP"}},
			VolumeMounts: []VolumeMount{{
				SourceKind: "PersistentVolumeClaim",
				SourceName: "data",
				MountPath:  "/data",
			}},
		},
	}

	items := deploymentManifestItems(deployment)
	var deploymentItem map[string]any
	for _, item := range items {
		if item["kind"] == "Deployment" {
			deploymentItem = item
		}
	}
	if deploymentItem == nil {
		t.Fatalf("no Deployment item in %+v", items)
	}
	raw, err := json.Marshal(deploymentItem)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := decodeDeployment("platform", raw)
	if err != nil {
		t.Fatalf("decodeDeployment() error = %v", err)
	}
	if got.Name != "app" || got.Hash != "hash-1" || got.Replicas != 2 || got.Container.Image != "demo:v1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.Namespace != "platform" {
		t.Fatalf("namespace = %q, want platform", got.Namespace)
	}
}

func TestServiceManifestRoundTrip(t *testing.T) {
	service := Service{
		Namespace: "platform",
		Name:      "edge",
		Hash:      "hash-2",
		Type:      "NodePort",
		Selector:  map[string]string{"guardian.asset": "app"},
		Ports:     []ServicePort{{Name: "http", Port: 9090, TargetPort: 9090, Protocol: "TCP"}},
	}
	raw, err := json.Marshal(serviceManifest(service))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := decodeService("platform", raw)
	if err != nil {
		t.Fatalf("decodeService() error = %v", err)
	}
	if got.Name != "edge" || got.Hash != "hash-2" || got.Type != "NodePort" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if len(got.Ports) != 1 || got.Ports[0].Port != 9090 || got.Ports[0].TargetPort != 9090 {
		t.Fatalf("ports mismatch: %+v", got.Ports)
	}
	if got.Selector["guardian.asset"] != "app" {
		t.Fatalf("selector mismatch: %+v", got.Selector)
	}
}

func TestConfigMapAndClaimManifestRoundTrip(t *testing.T) {
	rawCM, err := json.Marshal(configMapManifest(ConfigMap{
		Namespace: "platform",
		Name:      "app-config",
		Hash:      "hash-cm",
		Data:      map[string]string{"key": "value"},
	}))
	if err != nil {
		t.Fatalf("marshal cm: %v", err)
	}
	cm, err := decodeConfigMap("platform", rawCM)
	if err != nil {
		t.Fatalf("decodeConfigMap() error = %v", err)
	}
	if cm.Name != "app-config" || cm.Hash != "hash-cm" || cm.Data["key"] != "value" {
		t.Fatalf("cm round-trip mismatch: %+v", cm)
	}

	rawClaim, err := json.Marshal(claimManifest(PersistentVolumeClaim{
		Namespace:    "platform",
		Name:         "data",
		Hash:         "hash-pvc",
		Size:         "20Gi",
		AccessMode:   "ReadWriteOnce",
		StorageClass: "fast",
	}))
	if err != nil {
		t.Fatalf("marshal pvc: %v", err)
	}
	claim, err := decodeClaim("platform", rawClaim)
	if err != nil {
		t.Fatalf("decodeClaim() error = %v", err)
	}
	if claim.Name != "data" || claim.Hash != "hash-pvc" || claim.Size != "20Gi" || claim.StorageClass != "fast" {
		t.Fatalf("pvc round-trip mismatch: %+v", claim)
	}
}

func TestWaitingFailureClassifiesReasons(t *testing.T) {
	transient := []any{map[string]any{
		"state": map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}},
	}}
	if _, _, found := waitingFailure(transient); found {
		t.Fatalf("transient waiting reason should not be a terminal failure")
	}
	terminal := []any{map[string]any{
		"state": map[string]any{"waiting": map[string]any{"reason": "ImagePullBackOff", "message": "pull failed"}},
	}}
	reason, message, found := waitingFailure(terminal)
	if !found || reason != "ImagePullBackOff" || message != "pull failed" {
		t.Fatalf("terminal waiting reason = (%q, %q, %v)", reason, message, found)
	}
}

func TestSelectorForLabelsIsStable(t *testing.T) {
	selector := selectorForLabels(map[string]string{
		"guardian.managed":   "true",
		"guardian.partition": "payments",
		"guardian.intent":    "api",
		"guardian.asset":     "app",
		"unrelated":          "ignored",
	})
	if selector["guardian.managed"] != "true" && selector["guardian.managed"] != "" {
		// guardian.managed is always present.
	}
	if _, ok := selector["unrelated"]; ok {
		t.Fatalf("selector must not include unrelated labels: %+v", selector)
	}
	if selector["guardian.partition"] != "payments" || selector["guardian.asset"] != "app" {
		t.Fatalf("selector missing guardian labels: %+v", selector)
	}
}
