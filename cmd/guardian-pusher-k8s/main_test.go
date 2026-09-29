package main

import (
	"os"
	"path/filepath"
	"testing"

	kubernetesdriver "github.com/rydzu/ainfra/guardian/internal/pusher/drivers/kubernetes"
)

const testKubeconfig = `apiVersion: v1
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

func writeTestKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

func TestNewKubeBackendClientMode(t *testing.T) {
	backend, err := newKubeBackend("client", "kubectl", writeTestKubeconfig(t), "")
	if err != nil {
		t.Fatalf("newKubeBackend(client) error = %v", err)
	}
	if _, ok := backend.(*kubernetesdriver.ClientBackend); !ok {
		t.Fatalf("client mode returned %T, want *kubernetesdriver.ClientBackend", backend)
	}
}

func TestNewKubeBackendKubectlMode(t *testing.T) {
	backend, err := newKubeBackend("kubectl", "kubectl", writeTestKubeconfig(t), "")
	if err != nil {
		t.Fatalf("newKubeBackend(kubectl) error = %v", err)
	}
	if _, ok := backend.(*kubernetesdriver.CLIBackend); !ok {
		t.Fatalf("kubectl mode returned %T, want *kubernetesdriver.CLIBackend", backend)
	}
}

func TestNewKubeBackendAutoFallsBackToKubectl(t *testing.T) {
	// An explicit, non-existent kubeconfig makes client-go config loading fail,
	// so auto mode must fall back to the kubectl backend (which only resolves
	// the binary at construction time).
	backend, err := newKubeBackend("auto", "kubectl", filepath.Join(t.TempDir(), "missing"), "")
	if err != nil {
		t.Fatalf("newKubeBackend(auto) error = %v", err)
	}
	if _, ok := backend.(*kubernetesdriver.CLIBackend); !ok {
		t.Fatalf("auto fallback returned %T, want *kubernetesdriver.CLIBackend", backend)
	}
}
