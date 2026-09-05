package dockerdriver

import (
	"testing"

	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
)

func TestToComputePortsIgnoresHostPortWhenDynamicHostnameIsSet(t *testing.T) {
	containerPort := 8080
	hostPort := 18080
	ports := toComputePorts([]assetdefs.PortSpec{{
		Name:            "http",
		ContainerPort:   &containerPort,
		HostPort:        &hostPort,
		DynamicHostname: "app.home.arpa",
	}})

	if len(ports) != 1 {
		t.Fatalf("len(ports) = %d, want 1", len(ports))
	}
	if ports[0].ContainerPort != 8080 {
		t.Fatalf("containerPort = %d, want 8080", ports[0].ContainerPort)
	}
	if ports[0].HostPort != 0 {
		t.Fatalf("hostPort = %d, want 0", ports[0].HostPort)
	}
}

func TestDesiredContainerForDiffIgnoresHostPortWhenDynamicHostnameIsSet(t *testing.T) {
	containerPort := 8080
	hostPort := 18080
	spec := &assetdefs.ComputeSpec{
		Image: "demo:v1",
		Ports: []assetdefs.PortSpec{{
			Name:            "http",
			ContainerPort:   &containerPort,
			HostPort:        &hostPort,
			DynamicHostname: "app.home.arpa",
		}},
	}

	container := DesiredContainerForDiff("demo", "stack", "app", targetdomain.Placement{Cluster: "local"}, spec, 0)
	if len(container.Ports) != 1 {
		t.Fatalf("len(container.Ports) = %d, want 1", len(container.Ports))
	}
	if container.Ports[0].HostPort != 0 {
		t.Fatalf("hostPort = %d, want 0", container.Ports[0].HostPort)
	}
}
