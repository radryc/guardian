package kubernetesdriver

import (
	"testing"

	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
)

func TestToServicePortsIgnoresHostPortWhenDynamicHostnameIsSet(t *testing.T) {
	containerPort := 8080
	hostPort := 30080
	ports := toServicePorts([]assetdefs.PortSpec{{
		Name:            "http",
		ContainerPort:   &containerPort,
		HostPort:        &hostPort,
		DynamicHostname: "app.home.arpa",
	}})

	if len(ports) != 1 {
		t.Fatalf("len(ports) = %d, want 1", len(ports))
	}
	if ports[0].Port != 8080 {
		t.Fatalf("service port = %d, want 8080", ports[0].Port)
	}
	if ports[0].TargetPort != 8080 {
		t.Fatalf("targetPort = %d, want 8080", ports[0].TargetPort)
	}
	if ports[0].HostPort != 0 {
		t.Fatalf("hostPort = %d, want 0", ports[0].HostPort)
	}
}
