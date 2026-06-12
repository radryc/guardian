package dockerdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	runtimepkg "github.com/rydzu/ainfra/guardian/internal/pusher/runtime"
	"github.com/rydzu/ainfra/guardian/internal/pusher/secrets"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

// ----------------------------------------------------------------------------
// Helpers shared by tests in this file
// ----------------------------------------------------------------------------

// buildTestRuntime constructs an in-memory store + backend + runtime for use
// in structural-diff tests.
func buildTestRuntime(t *testing.T) (context.Context, *memory.Store, *Backend, *runtimepkg.Runtime) {
	t.Helper()
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	reg := registry.New()
	Register(reg, backend, secrets.NewStoreResolver(store))
	rt := &runtimepkg.Runtime{
		QueuePath: paths.QueueDir("docker"),
		WorkerID:  "docker-worker",
		Store:     store,
		Registry:  reg,
		CanHandle: func(t *taskdomain.Task) bool { return t.Target.Cluster == "local" },
	}
	return ctx, store, backend, rt
}

// runOp writes a task to the queue and processes it, returning the result.
func runOp(t *testing.T, ctx context.Context, store *memory.Store, rt *runtimepkg.Runtime, taskID string, op taskdomain.Operation, assets []taskdomain.AbstractAsset) taskdomain.TaskResult {
	t.Helper()
	task := taskdomain.Task{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "Task",
		TaskID:       taskID,
		Partition:    "p",
		Intent:       "i",
		Op:           op,
		TargetPusher: "docker",
		Target:       targetdomain.Placement{Cluster: "local"},
		Assets:       assets,
	}
	content, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("docker", taskID), Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "test"},
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if err := rt.ProcessPending(ctx); err != nil {
		t.Fatalf("process task: %v", err)
	}
	raw, err := store.ReadFile(ctx, paths.QueueResult("docker", taskID))
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var result taskdomain.TaskResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return result
}

// baseComputeAssets returns a minimal single-container Compute asset list.
func baseComputeAssets(image string) []taskdomain.AbstractAsset {
	return []taskdomain.AbstractAsset{{
		Type: "Compute",
		Name: "svc",
		Properties: map[string]any{
			"image": image,
			"ports": []any{map[string]any{"containerPort": 8080}},
			"env":   map[string]any{"APP_MODE": "prod"},
		},
	}}
}

// ----------------------------------------------------------------------------
// parseContainerInspect
// ----------------------------------------------------------------------------

func TestParseContainerInspect_AllFields(t *testing.T) {
	raw := `[{
		"Name": "/myapp",
		"Config": {
			"Image": "nginx:alpine",
			"Labels": {"guardian.managed":"true","guardian.hash":"abc123"},
			"Env": ["PORT=8080","DEBUG=false","EMPTY="]
		},
		"State": {"Running": true},
		"HostConfig": {
			"NetworkMode": "my-net",
			"CapAdd": ["NET_ADMIN","SYS_PTRACE"],
			"Privileged": true,
			"PortBindings": {
				"8080/tcp": [{"HostPort":"80"}],
				"443/tcp":  [{"HostPort":"443"}]
			}
		},
		"Mounts": [
			{"Type":"volume","Name":"my-vol","Destination":"/data","RW":true},
			{"Type":"bind","Source":"/host/cfg","Destination":"/etc/app","RW":false}
		],
		"NetworkSettings": {
			"Networks": {
				"my-net":    {"Aliases":["myapp"]},
				"extra-net": {"Aliases":["svc"]}
			}
		}
	}]`

	containers, err := parseContainerInspect([]byte(raw))
	if err != nil {
		t.Fatalf("parseContainerInspect: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
	c := containers[0]

	if c.Name != "myapp" {
		t.Errorf("Name = %q, want %q", c.Name, "myapp")
	}
	if c.Image != "nginx:alpine" {
		t.Errorf("Image = %q", c.Image)
	}
	if !c.Running {
		t.Errorf("expected Running=true")
	}
	if !c.Privileged {
		t.Errorf("expected Privileged=true")
	}
	if c.Network != "my-net" {
		t.Errorf("Network = %q", c.Network)
	}
	if c.Hash != "abc123" {
		t.Errorf("Hash = %q", c.Hash)
	}

	// Env
	if c.Env["PORT"] != "8080" || c.Env["DEBUG"] != "false" || c.Env["EMPTY"] != "" {
		t.Errorf("Env = %v", c.Env)
	}

	// Ports (sorted by containerPort)
	if len(c.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(c.Ports))
	}
	if c.Ports[0].ContainerPort != 443 || c.Ports[0].HostPort != 443 {
		t.Errorf("port[0] = %+v", c.Ports[0])
	}
	if c.Ports[1].ContainerPort != 8080 || c.Ports[1].HostPort != 80 {
		t.Errorf("port[1] = %+v", c.Ports[1])
	}

	// Caps (sorted)
	if len(c.Capabilities) != 2 || c.Capabilities[0] != "NET_ADMIN" || c.Capabilities[1] != "SYS_PTRACE" {
		t.Errorf("Capabilities = %v", c.Capabilities)
	}

	// Mounts
	if len(c.VolumeMounts) != 1 || c.VolumeMounts[0].Source != "my-vol" || c.VolumeMounts[0].Target != "/data" {
		t.Errorf("VolumeMounts = %+v", c.VolumeMounts)
	}
	if len(c.HostBindMounts) != 1 || c.HostBindMounts[0].Source != "/host/cfg" || c.HostBindMounts[0].ReadOnly != true {
		t.Errorf("HostBindMounts = %+v", c.HostBindMounts)
	}

	// Extra networks
	if len(c.ExtraNetworks) != 1 || c.ExtraNetworks[0].Name != "extra-net" {
		t.Errorf("ExtraNetworks = %+v", c.ExtraNetworks)
	}
}

func TestParseContainerInspect_LeadingSlashStripped(t *testing.T) {
	raw := `[{"Name":"/svc","Config":{"Image":"img","Labels":{},"Env":[]},"State":{"Running":false},
		"HostConfig":{"NetworkMode":"bridge","CapAdd":null,"Privileged":false,"PortBindings":{}},
		"Mounts":[],"NetworkSettings":{"Networks":{}}}]`
	cs, err := parseContainerInspect([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if cs[0].Name != "svc" {
		t.Errorf("Name not stripped: %q", cs[0].Name)
	}
}

func TestParseVolumeInspect_Labels(t *testing.T) {
	raw := fmt.Sprintf(`[{"Name":"my-vol","Labels":{%q:%q,%q:%q,%q:%q}}]`,
		"guardian.hash", "h1",
		volumeSizeLabel, "10Gi",
		volumeAccessModeLabel, "ReadWriteOnce",
	)
	vols, err := parseVolumeInspect([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	v := vols[0]
	if v.Hash != "h1" || v.Size != "10Gi" || v.AccessMode != "ReadWriteOnce" {
		t.Errorf("volume = %+v", v)
	}
}

func TestParseNetworkInspect(t *testing.T) {
	raw := `[{"Name":"my-net","Labels":{"guardian.hash":"nh1"},"Driver":"overlay","Internal":true}]`
	nets, err := parseNetworkInspect([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 1 {
		t.Fatalf("expected 1 network")
	}
	n := nets[0]
	if n.Hash != "nh1" || n.Driver != "overlay" || !n.Internal {
		t.Errorf("network = %+v", n)
	}
}

// ----------------------------------------------------------------------------
// ContainerToProperties
// ----------------------------------------------------------------------------

func TestContainerToProperties_Full(t *testing.T) {
	c := Container{
		Image:          "nginx:alpine",
		Privileged:     true,
		Capabilities:   []string{"NET_ADMIN"},
		Ports:          []PortBinding{{ContainerPort: 8080, HostPort: 80, Protocol: "TCP"}},
		Env:            map[string]string{"FOO": "bar"},
		VolumeMounts:   []VolumeMount{{Source: "my-vol", Target: "/data"}},
		HostBindMounts: []HostBindMount{{Source: "/etc/ssl", Target: "/ssl", ReadOnly: true}},
	}
	props := ContainerToProperties(c)
	if props["image"] != "nginx:alpine" {
		t.Errorf("image = %v", props["image"])
	}
	if props["privileged"] != true {
		t.Errorf("privileged = %v", props["privileged"])
	}
	ports, ok := props["ports"].([]map[string]any)
	if !ok || len(ports) != 1 {
		t.Errorf("ports = %v", props["ports"])
	}
	if ports[0]["containerPort"] != 8080 {
		t.Errorf("containerPort = %v", ports[0])
	}
	// TCP is default, should not be emitted
	if _, hasProto := ports[0]["protocol"]; hasProto {
		t.Errorf("protocol should be omitted for TCP")
	}
}

func TestContainerToProperties_UDPProtocolEmitted(t *testing.T) {
	c := Container{
		Image: "dns",
		Ports: []PortBinding{{ContainerPort: 53, Protocol: "udp"}},
	}
	props := ContainerToProperties(c)
	ports := props["ports"].([]map[string]any)
	if ports[0]["protocol"] != "UDP" {
		t.Errorf("protocol = %v", ports[0]["protocol"])
	}
}

// ----------------------------------------------------------------------------
// StructuralContainerDrift
// ----------------------------------------------------------------------------

func TestStructuralContainerDrift_InSync(t *testing.T) {
	c := Container{
		Image:        "nginx:alpine",
		Running:      true,
		Network:      "my-net",
		Ports:        []PortBinding{{ContainerPort: 8080, Protocol: "TCP"}},
		Env:          map[string]string{"K": "V"},
		VolumeMounts: []VolumeMount{{Source: "vol", Target: "/data"}},
		Capabilities: []string{"NET_ADMIN"},
		Privileged:   false,
	}
	drifted, reason := StructuralContainerDrift(c, c)
	if drifted {
		t.Errorf("expected in-sync, got drift: %s", reason)
	}
}

func TestStructuralContainerDrift_Stopped(t *testing.T) {
	desired := Container{Image: "nginx", Running: true}
	actual := Container{Image: "nginx", Running: false}
	drifted, reason := StructuralContainerDrift(desired, actual)
	if drifted {
		t.Errorf("expected stopped container to remain structurally in sync, got drift: %q", reason)
	}
}

func TestStructuralContainerDrift_ImageChanged(t *testing.T) {
	desired := Container{Image: "nginx:1.25", Running: true}
	actual := Container{Image: "nginx:1.26", Running: true}
	drifted, _ := StructuralContainerDrift(desired, actual)
	if !drifted {
		t.Errorf("expected drift for changed image")
	}
}

func TestStructuralContainerDrift_NetworkChanged(t *testing.T) {
	desired := Container{Image: "img", Running: true, Network: "net-a"}
	actual := Container{Image: "img", Running: true, Network: "net-b"}
	drifted, _ := StructuralContainerDrift(desired, actual)
	if !drifted {
		t.Errorf("expected drift for changed network")
	}
}

func TestStructuralContainerDrift_PortAdded(t *testing.T) {
	desired := Container{Image: "img", Running: true, Ports: []PortBinding{{ContainerPort: 8080, HostPort: 80, Protocol: "TCP"}, {ContainerPort: 9090, HostPort: 90, Protocol: "TCP"}}}
	actual := Container{Image: "img", Running: true, Ports: []PortBinding{{ContainerPort: 8080, HostPort: 80, Protocol: "TCP"}}}
	drifted, _ := StructuralContainerDrift(desired, actual)
	if !drifted {
		t.Errorf("expected drift for added port")
	}
}

func TestStructuralContainerDrift_HostlessPortsIgnored(t *testing.T) {
	desired := Container{Image: "img", Running: true, Ports: []PortBinding{{ContainerPort: 7860, Protocol: "TCP"}, {ContainerPort: 7861, Protocol: "TCP"}}}
	actual := Container{Image: "img", Running: true}
	drifted, _ := StructuralContainerDrift(desired, actual)
	if drifted {
		t.Errorf("hostless ports should not be structural drift")
	}
}

func TestStructuralContainerDrift_HostPortChange_NotDrift(t *testing.T) {
	// Host port is allocation detail; changing it is NOT structural drift.
	desired := Container{Image: "img", Running: true, Ports: []PortBinding{{ContainerPort: 8080, HostPort: 80, Protocol: "TCP"}}}
	actual := Container{Image: "img", Running: true, Ports: []PortBinding{{ContainerPort: 8080, HostPort: 9999, Protocol: "TCP"}}}
	drifted, _ := StructuralContainerDrift(desired, actual)
	if drifted {
		t.Errorf("host-port change should not be structural drift")
	}
}

func TestStructuralContainerDrift_EnvChanged(t *testing.T) {
	desired := Container{Image: "img", Running: true, Env: map[string]string{"MODE": "prod"}}
	actual := Container{Image: "img", Running: true, Env: map[string]string{"MODE": "dev"}}
	drifted, _ := StructuralContainerDrift(desired, actual)
	if !drifted {
		t.Errorf("expected drift for env value change")
	}
}

func TestStructuralContainerDrift_EnvKeyAdded(t *testing.T) {
	desired := Container{Image: "img", Running: true, Env: map[string]string{"A": "1", "B": "2"}}
	actual := Container{Image: "img", Running: true, Env: map[string]string{"A": "1"}}
	drifted, _ := StructuralContainerDrift(desired, actual)
	if !drifted {
		t.Errorf("expected drift for env key added")
	}
}

func TestStructuralContainerDrift_EnvExtraActualKey_NotDrift(t *testing.T) {
	desired := Container{Image: "img", Running: true, Env: map[string]string{"A": "1"}}
	actual := Container{Image: "img", Running: true, Env: map[string]string{"A": "1", "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}}
	drifted, _ := StructuralContainerDrift(desired, actual)
	if drifted {
		t.Errorf("extra runtime env keys should not be structural drift")
	}
}

func TestStructuralContainerDrift_VolumeMountChanged(t *testing.T) {
	desired := Container{Image: "img", Running: true, VolumeMounts: []VolumeMount{{Source: "vol-a", Target: "/data"}}}
	actual := Container{Image: "img", Running: true, VolumeMounts: []VolumeMount{{Source: "vol-b", Target: "/data"}}}
	drifted, _ := StructuralContainerDrift(desired, actual)
	if !drifted {
		t.Errorf("expected drift for volume source change")
	}
}

func TestStructuralContainerDrift_HostBindMountReadOnlyChanged(t *testing.T) {
	desired := Container{Image: "img", Running: true, HostBindMounts: []HostBindMount{{Source: "/h", Target: "/c", ReadOnly: true}}}
	actual := Container{Image: "img", Running: true, HostBindMounts: []HostBindMount{{Source: "/h", Target: "/c", ReadOnly: false}}}
	drifted, _ := StructuralContainerDrift(desired, actual)
	if !drifted {
		t.Errorf("expected drift for readOnly change")
	}
}

func TestStructuralContainerDrift_CapabilitiesOrderIndependent(t *testing.T) {
	desired := Container{Image: "img", Running: true, Capabilities: []string{"NET_ADMIN", "SYS_PTRACE"}}
	actual := Container{Image: "img", Running: true, Capabilities: []string{"SYS_PTRACE", "NET_ADMIN"}}
	drifted, _ := StructuralContainerDrift(desired, actual)
	if drifted {
		t.Errorf("capability order difference should not be drift")
	}
}

func TestStructuralContainerDrift_PrivilegedChanged(t *testing.T) {
	desired := Container{Image: "img", Running: true, Privileged: true}
	actual := Container{Image: "img", Running: true, Privileged: false}
	drifted, _ := StructuralContainerDrift(desired, actual)
	if !drifted {
		t.Errorf("expected drift for privileged change")
	}
}

// ----------------------------------------------------------------------------
// ComputeDriver.Diff — end-to-end with in-memory backend
// ----------------------------------------------------------------------------

// applyThenCorrupt runs Apply then manually mutates the backend container to
// simulate out-of-band drift, then returns the Diff result.
func applyThenDiff(t *testing.T, assets []taskdomain.AbstractAsset, corrupt func(b *Backend)) taskdomain.TaskResult {
	t.Helper()
	ctx, store, backend, rt := buildTestRuntime(t)
	apply := runOp(t, ctx, store, rt, "apply", taskdomain.OpApply, assets)
	if apply.Status != taskdomain.ResultSucceeded {
		t.Fatalf("Apply failed: %v", apply.Error)
	}
	if corrupt != nil {
		corrupt(backend)
	}
	return runOp(t, ctx, store, rt, "diff", taskdomain.OpDiff, assets)
}

func TestComputeDiff_InSyncAfterApply(t *testing.T) {
	diff := applyThenDiff(t, baseComputeAssets("demo:v1"), nil)
	if diff.Status != taskdomain.ResultSucceeded {
		t.Fatalf("Diff failed: %v", diff.Error)
	}
	if diff.Drift == nil || diff.Drift.Status != "InSync" {
		t.Errorf("expected InSync after Apply, got %+v", diff.Drift)
	}
}

func TestComputeDiff_DetectsImageDrift(t *testing.T) {
	assets := baseComputeAssets("demo:v1")
	diff := applyThenDiff(t, assets, func(b *Backend) {
		// Simulate someone running the container with a different image.
		name := computeContainerNameForTest("local", "p", "i", "svc", 0)
		c, ok, _ := b.GetContainer(name)
		if !ok {
			return
		}
		c.Image = "demo:v2-manual"
		_ = b.UpsertContainer(c)
	})
	if diff.Drift == nil || diff.Drift.Status == "InSync" {
		t.Errorf("expected Drifted for image change, got %+v", diff.Drift)
	}
}

func TestComputeDiff_DetectsStoppedContainer(t *testing.T) {
	assets := baseComputeAssets("demo:v1")
	diff := applyThenDiff(t, assets, func(b *Backend) {
		name := computeContainerNameForTest("local", "p", "i", "svc", 0)
		c, ok, _ := b.GetContainer(name)
		if !ok {
			return
		}
		c.Running = false
		_ = b.UpsertContainer(c)
	})
	if diff.Drift == nil || diff.Drift.Status == "InSync" {
		t.Errorf("expected Drifted for stopped container, got %+v", diff.Drift)
	}
	if diff.AssetObservations == nil || diff.AssetObservations["svc"] == nil {
		t.Fatalf("expected svc asset observation, got %+v", diff.AssetObservations)
	}
	if got := diff.AssetObservations["svc"].Health; got == nil || got.Status != taskdomain.HealthUnhealthy {
		t.Fatalf("svc health = %+v, want unhealthy", got)
	}
}

func TestComputeDiff_DetectsEnvDrift(t *testing.T) {
	assets := baseComputeAssets("demo:v1")
	diff := applyThenDiff(t, assets, func(b *Backend) {
		name := computeContainerNameForTest("local", "p", "i", "svc", 0)
		c, ok, _ := b.GetContainer(name)
		if !ok {
			return
		}
		// Change the env value out-of-band.
		c.Env["APP_MODE"] = "staging"
		_ = b.UpsertContainer(c)
	})
	if diff.Drift == nil || diff.Drift.Status == "InSync" {
		t.Errorf("expected Drifted for env change, got %+v", diff.Drift)
	}
}

func TestComputeDiff_DetectsPortDrift(t *testing.T) {
	assets := []taskdomain.AbstractAsset{{
		Type: "Compute",
		Name: "svc",
		Properties: map[string]any{
			"image": "demo:v1",
			"ports": []any{map[string]any{"containerPort": 8080, "hostPort": 18080}},
		},
	}}
	diff := applyThenDiff(t, assets, func(b *Backend) {
		name := computeContainerNameForTest("local", "p", "i", "svc", 0)
		c, ok, _ := b.GetContainer(name)
		if !ok {
			return
		}
		// Remove all ports.
		c.Ports = nil
		_ = b.UpsertContainer(c)
	})
	if diff.Drift == nil || diff.Drift.Status == "InSync" {
		t.Errorf("expected Drifted for port removal, got %+v", diff.Drift)
	}
}

func TestComputeDiff_DetectsCapabilityDrift(t *testing.T) {
	assets := []taskdomain.AbstractAsset{{
		Type: "Compute",
		Name: "svc",
		Properties: map[string]any{
			"image":        "demo:v1",
			"capabilities": []any{"NET_ADMIN"},
		},
	}}
	diff := applyThenDiff(t, assets, func(b *Backend) {
		name := computeContainerNameForTest("local", "p", "i", "svc", 0)
		c, ok, _ := b.GetContainer(name)
		if !ok {
			return
		}
		c.Capabilities = nil // removed out-of-band
		_ = b.UpsertContainer(c)
	})
	if diff.Drift == nil || diff.Drift.Status == "InSync" {
		t.Errorf("expected Drifted for capability removal, got %+v", diff.Drift)
	}
}

func TestVolumeDiff_HashOnlyChange_NotDrift(t *testing.T) {
	assets := []taskdomain.AbstractAsset{{
		Type: "Volume",
		Name: "vol",
		Properties: map[string]any{
			"size":       "10Gi",
			"accessMode": "ReadWriteOnce",
			"ephemeral":  false,
		},
	}}
	diff := applyThenDiff(t, assets, func(b *Backend) {
		name := driverutil.ResourceName("docker-vol", targetdomain.Placement{Cluster: "local"}, "p", "i", "vol")
		v, ok, _ := b.GetVolume(name)
		if !ok {
			return
		}
		v.Hash = "manually-changed"
		if v.Labels == nil {
			v.Labels = map[string]string{}
		}
		v.Labels["guardian.hash"] = "manually-changed"
		_ = b.UpsertVolume(v)
	})
	if diff.Drift == nil || diff.Drift.Status != "InSync" {
		t.Errorf("expected InSync when only volume hash changed, got %+v", diff.Drift)
	}
}

func TestLoadBalancerDiff_HashOnlyChange_NotDrift(t *testing.T) {
	assets := []taskdomain.AbstractAsset{
		{
			Type: "Compute",
			Name: "svc",
			Properties: map[string]any{
				"image": "demo:v1",
				"ports": []any{map[string]any{"containerPort": 8080}},
			},
		},
		{
			Type: "LoadBalancer",
			Name: "edge",
			Properties: map[string]any{
				"listeners": []any{map[string]any{"name": "http", "port": 3400, "protocol": "TCP"}},
				"targets":   []any{"svc"},
			},
		},
	}
	diff := applyThenDiff(t, assets, func(b *Backend) {
		name := driverutil.ResourceName("docker-lb", targetdomain.Placement{Cluster: "local"}, "p", "i", "edge")
		c, ok, _ := b.GetContainer(name)
		if !ok {
			return
		}
		c.Hash = "manually-changed"
		if c.Labels == nil {
			c.Labels = map[string]string{}
		}
		c.Labels["guardian.hash"] = "manually-changed"
		_ = b.UpsertContainer(c)
	})
	if diff.Drift == nil || diff.Drift.Status != "InSync" {
		t.Errorf("expected InSync when only load balancer hash changed, got %+v", diff.Drift)
	}
}

// computeContainerNameForTest mirrors the naming logic used by ComputeDriver
// so tests can locate containers in the backend without going through the driver.
func computeContainerNameForTest(cluster, partition, intent, asset string, idx int) string {
	return driverutil.ResourceName("docker-ct",
		targetdomain.Placement{Cluster: cluster},
		partition, intent, asset,
		strconv.Itoa(idx),
	)
}

// ----------------------------------------------------------------------------
// StructuralNetworkDrift
// ----------------------------------------------------------------------------

func TestStructuralNetworkDrift_InSync(t *testing.T) {
	n := Network{Driver: "bridge", Internal: false}
	drifted, reason := StructuralNetworkDrift(n, n)
	if drifted {
		t.Errorf("expected in-sync, got drift: %s", reason)
	}
}

func TestStructuralNetworkDrift_DefaultDriverBridge(t *testing.T) {
	// Empty driver in desired should normalise to "bridge".
	desired := Network{Driver: ""}
	actual := Network{Driver: "bridge"}
	drifted, _ := StructuralNetworkDrift(desired, actual)
	if drifted {
		t.Errorf("empty desired driver should match bridge")
	}
}

func TestStructuralNetworkDrift_DriverChanged(t *testing.T) {
	desired := Network{Driver: "overlay"}
	actual := Network{Driver: "bridge"}
	drifted, reason := StructuralNetworkDrift(desired, actual)
	if !drifted {
		t.Errorf("expected drift for driver change")
	}
	if !containsString(reason, "driver") {
		t.Errorf("reason should mention 'driver', got %q", reason)
	}
}

func TestStructuralNetworkDrift_InternalChanged(t *testing.T) {
	desired := Network{Driver: "bridge", Internal: true}
	actual := Network{Driver: "bridge", Internal: false}
	drifted, reason := StructuralNetworkDrift(desired, actual)
	if !drifted {
		t.Errorf("expected drift for internal change")
	}
	if !containsString(reason, "internal") {
		t.Errorf("reason should mention 'internal', got %q", reason)
	}
}

func TestStructuralNetworkDrift_HashIgnored(t *testing.T) {
	// Hash changes should NOT trigger structural drift.
	desired := Network{Driver: "bridge", Internal: false, Hash: "aaa"}
	actual := Network{Driver: "bridge", Internal: false, Hash: "bbb"}
	drifted, _ := StructuralNetworkDrift(desired, actual)
	if drifted {
		t.Errorf("hash change should not be structural drift")
	}
}

// ----------------------------------------------------------------------------
// DetailedContainerDiff
// ----------------------------------------------------------------------------

func TestDetailedContainerDiff_NoFields_InSync(t *testing.T) {
	c := Container{
		Image:   "nginx:1.25",
		Network: "my-net",
		Running: true,
		Ports:   []PortBinding{{ContainerPort: 8080, Protocol: "TCP"}},
		Env:     map[string]string{"MODE": "prod"},
	}
	diffs := DetailedContainerDiff(c, c)
	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical containers, got %v", diffs)
	}
}

func TestDetailedContainerDiff_StoppedContainer(t *testing.T) {
	desired := Container{Image: "nginx", Running: true}
	actual := Container{Image: "nginx", Running: false}
	diffs := DetailedContainerDiff(desired, actual)
	if len(diffs) != 0 {
		t.Errorf("expected stopped container to be omitted from structural diff, got %v", diffs)
	}
}

func TestDetailedContainerDiff_ImageField(t *testing.T) {
	desired := Container{Image: "nginx:1.25", Running: true}
	actual := Container{Image: "nginx:1.24", Running: true}
	diffs := DetailedContainerDiff(desired, actual)
	if !hasDiffField(diffs, "image") {
		t.Errorf("expected diff on 'image' field, got %v", diffs)
	}
	f := findDiffField(diffs, "image")
	if f.Desired != "nginx:1.25" || f.Actual != "nginx:1.24" {
		t.Errorf("image diff values wrong: %+v", f)
	}
}

func TestDetailedContainerDiff_EnvDrift(t *testing.T) {
	desired := Container{Image: "img", Running: true, Env: map[string]string{"VERSION": "2.0"}}
	actual := Container{Image: "img", Running: true, Env: map[string]string{"VERSION": "1.0"}}
	diffs := DetailedContainerDiff(desired, actual)
	if !hasDiffField(diffs, "env[VERSION]") {
		t.Errorf("expected env[VERSION] diff, got %v", diffs)
	}
}

func TestDetailedContainerDiff_SecretEnvSkipped(t *testing.T) {
	// Env vars with "<secret>" sentinel should be skipped even if actual differs.
	desired := Container{Image: "img", Running: true, Env: map[string]string{"DB_PASS": "<secret>"}}
	actual := Container{Image: "img", Running: true, Env: map[string]string{"DB_PASS": "hunter2"}}
	diffs := DetailedContainerDiff(desired, actual)
	if hasDiffField(diffs, "env[DB_PASS]") {
		t.Errorf("secret env var should be skipped, got diff %v", diffs)
	}
}

func TestDetailedContainerDiff_PortsDrift(t *testing.T) {
	desired := Container{Image: "img", Running: true,
		Ports: []PortBinding{{ContainerPort: 8080, Protocol: "TCP"}, {ContainerPort: 9090, Protocol: "TCP"}}}
	actual := Container{Image: "img", Running: true,
		Ports: []PortBinding{{ContainerPort: 8080, Protocol: "TCP"}}}
	diffs := DetailedContainerDiff(desired, actual)
	if !hasDiffField(diffs, "ports") {
		t.Errorf("expected ports diff, got %v", diffs)
	}
}

func TestDetailedContainerDiff_CapabilitiesDrift(t *testing.T) {
	desired := Container{Image: "img", Running: true, Capabilities: []string{"NET_ADMIN", "SYS_PTRACE"}}
	actual := Container{Image: "img", Running: true, Capabilities: []string{"NET_ADMIN"}}
	diffs := DetailedContainerDiff(desired, actual)
	if !hasDiffField(diffs, "capabilities") {
		t.Errorf("expected capabilities diff, got %v", diffs)
	}
}

// ----------------------------------------------------------------------------
// DetailedNetworkDiff
// ----------------------------------------------------------------------------

func TestDetailedNetworkDiff_NoFields_InSync(t *testing.T) {
	n := Network{Driver: "bridge", Internal: false}
	diffs := DetailedNetworkDiff(n, n)
	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical networks, got %v", diffs)
	}
}

func TestDetailedNetworkDiff_DriverField(t *testing.T) {
	desired := Network{Driver: "overlay"}
	actual := Network{Driver: "bridge"}
	diffs := DetailedNetworkDiff(desired, actual)
	if !hasDiffField(diffs, "driver") {
		t.Errorf("expected driver diff, got %v", diffs)
	}
	f := findDiffField(diffs, "driver")
	if f.Desired != "overlay" || f.Actual != "bridge" {
		t.Errorf("driver diff values wrong: %+v", f)
	}
}

func TestDetailedNetworkDiff_InternalField(t *testing.T) {
	desired := Network{Driver: "bridge", Internal: true}
	actual := Network{Driver: "bridge", Internal: false}
	diffs := DetailedNetworkDiff(desired, actual)
	if !hasDiffField(diffs, "internal") {
		t.Errorf("expected internal diff, got %v", diffs)
	}
}

// ----------------------------------------------------------------------------
// NetworkDriver.Diff — end-to-end with in-memory backend
// ----------------------------------------------------------------------------

func TestNetworkDiff_InSyncAfterApply(t *testing.T) {
	assets := []taskdomain.AbstractAsset{{
		Type:       "Network",
		Name:       "frontend",
		Properties: map[string]any{"driver": "bridge"},
	}}
	ctx, store, _, rt := buildTestRuntime(t)
	apply := runOp(t, ctx, store, rt, "net-apply", taskdomain.OpApply, assets)
	if apply.Status != taskdomain.ResultSucceeded {
		t.Fatalf("Apply failed: %v", apply.Error)
	}
	diff := runOp(t, ctx, store, rt, "net-diff", taskdomain.OpDiff, assets)
	if diff.Drift == nil || diff.Drift.Status != "InSync" {
		t.Errorf("expected InSync after Apply, got %+v", diff.Drift)
	}
}

func TestNetworkDiff_NotFound(t *testing.T) {
	assets := []taskdomain.AbstractAsset{{
		Type:       "Network",
		Name:       "missing-net",
		Properties: map[string]any{},
	}}
	ctx, store, _, rt := buildTestRuntime(t)
	// Skip Apply — network doesn't exist.
	diff := runOp(t, ctx, store, rt, "net-diff-missing", taskdomain.OpDiff, assets)
	if diff.Drift == nil || diff.Drift.Status == "InSync" {
		t.Errorf("expected Drifted for missing network, got %+v", diff.Drift)
	}
}

func TestNetworkDiff_DriverDrift(t *testing.T) {
	assets := []taskdomain.AbstractAsset{{
		Type:       "Network",
		Name:       "mynet",
		Properties: map[string]any{"driver": "bridge"},
	}}
	ctx, store, backend, rt := buildTestRuntime(t)
	apply := runOp(t, ctx, store, rt, "net-apply2", taskdomain.OpApply, assets)
	if apply.Status != taskdomain.ResultSucceeded {
		t.Fatalf("Apply failed: %v", apply.Error)
	}
	// Mutate the network driver out-of-band.
	netName := driverutil.ResourceName("docker-net", targetdomain.Placement{Cluster: "local"}, "", "", "mynet")
	n, ok, _ := backend.GetNetwork(netName)
	if !ok {
		t.Fatalf("network not found after apply")
	}
	n.Driver = "overlay"
	backend.networks[netName] = n

	diff := runOp(t, ctx, store, rt, "net-diff2", taskdomain.OpDiff, assets)
	if diff.Drift == nil || diff.Drift.Status == "InSync" {
		t.Errorf("expected Drifted for driver change, got %+v", diff.Drift)
	}
}

// ----------------------------------------------------------------------------
// DesiredContainerForDiff
// ----------------------------------------------------------------------------

func TestDesiredContainerForDiff_Fields(t *testing.T) {
	replicas := 2
	spec := &assetdefs.ComputeSpec{
		Image:        "nginx:1.25",
		Replicas:     &replicas,
		Ports:        []assetdefs.PortSpec{{ContainerPort: intPtr(8080)}},
		Env:          map[string]any{"MODE": "prod", "SECRET": map[string]any{"secret": "path"}},
		Capabilities: []string{"NET_ADMIN"},
	}
	c := DesiredContainerForDiff("myptn", "myintent", "web", targetdomain.Placement{Cluster: "local"}, spec, 0)

	if c.Image != "nginx:1.25" {
		t.Errorf("image = %q", c.Image)
	}
	if c.Network == "" {
		t.Errorf("network should be set")
	}
	if c.Env["MODE"] != "prod" {
		t.Errorf("env[MODE] = %q", c.Env["MODE"])
	}
	if c.Env["SECRET"] != "<secret>" {
		t.Errorf("env[SECRET] should be <secret>, got %q", c.Env["SECRET"])
	}
	if len(c.Capabilities) != 1 || c.Capabilities[0] != "NET_ADMIN" {
		t.Errorf("capabilities = %v", c.Capabilities)
	}
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != 8080 {
		t.Errorf("ports = %v", c.Ports)
	}
}

func intPtr(v int) *int { return &v }

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

func containsString(s, sub string) bool {
	return strings.Contains(s, sub)
}

func hasDiffField(diffs []DiffField, field string) bool {
	for _, d := range diffs {
		if d.Field == field {
			return true
		}
	}
	return false
}

func findDiffField(diffs []DiffField, field string) DiffField {
	for _, d := range diffs {
		if d.Field == field {
			return d
		}
	}
	return DiffField{}
}
