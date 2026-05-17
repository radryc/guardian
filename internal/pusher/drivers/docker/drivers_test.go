package dockerdriver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	runtimepkg "github.com/rydzu/ainfra/guardian/internal/pusher/runtime"
	"github.com/rydzu/ainfra/guardian/internal/pusher/secrets"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestGenerateHAProxyConfigUsesDockerResolvers(t *testing.T) {
	in := registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "stack",
		Asset: taskdomain.AbstractAsset{
			Type: "LoadBalancer",
			Name: "edge",
		},
		Target: targetdomain.Placement{Cluster: "main"},
		Assets: map[string]taskdomain.AbstractAsset{
			"app": {
				Type: "Compute",
				Name: "app",
				Properties: map[string]any{
					"ports": []any{map[string]any{"containerPort": 4318}},
				},
			},
		},
	}
	spec := &assetdefs.LoadBalancerSpec{
		Targets: []string{"app"},
		Listeners: []assetdefs.ListenerSpec{{
			Name:     "http",
			Port:     intPtr(4318),
			Protocol: "TCP",
		}},
	}

	config, err := generateHAProxyConfig(in, spec)
	if err != nil {
		t.Fatalf("generateHAProxyConfig() error = %v", err)
	}
	if !strings.Contains(config, "resolvers docker\n  nameserver dns 127.0.0.11:53") {
		t.Fatalf("expected Docker DNS resolver config, got:\n%s", config)
	}
	if !strings.Contains(config, "default-server init-addr last,libc,none resolvers docker resolve-prefer ipv4") {
		t.Fatalf("expected backend default-server resolver config, got:\n%s", config)
	}
	if !strings.Contains(config, "server docker-ct-main-demo-stack-app-0 docker-ct-main-demo-stack-app-0:4318 check") {
		t.Fatalf("expected backend server entry, got:\n%s", config)
	}
}

func TestLoadBalancerContainerHashIncludesConfig(t *testing.T) {
	in := registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "stack",
		Asset: taskdomain.AbstractAsset{
			Type: "LoadBalancer",
			Name: "edge",
		},
		Target: targetdomain.Placement{Cluster: "main"},
		Assets: map[string]taskdomain.AbstractAsset{
			"app": {
				Type: "Compute",
				Name: "app",
			},
		},
	}
	spec := &assetdefs.LoadBalancerSpec{Targets: []string{"app"}}
	payload := containerPayload{}

	hashA := loadBalancerContainerHash(in, spec, "frontend a\n", payload)
	hashB := loadBalancerContainerHash(in, spec, "frontend b\n", payload)
	if hashA == hashB {
		t.Fatal("expected different HAProxy config content to change the load balancer hash")
	}
}

func TestDockerDriversApplyDiffDestroy(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	reg := registry.New()
	writeFile(t, ctx, store, "/partitions/shared/secrets/encryption-key", []byte("test-encryption-key\n"))
	Register(reg, backend, secrets.NewStoreResolver(store), WithDefaultExtraHosts(map[string]string{"host.docker.internal": "host-gateway"}))
	runtime := &runtimepkg.Runtime{
		QueuePath: paths.QueueDir("docker"),
		WorkerID:  "docker-worker",
		Store:     store,
		Registry:  reg,
		CanHandle: func(t *taskdomain.Task) bool { return t.Target.Cluster == "main" },
	}

	run := func(id string, op taskdomain.Operation) taskdomain.TaskResult {
		t.Helper()
		task := taskdomain.Task{
			APIVersion:   "guardian/v1alpha1",
			Kind:         "Task",
			TaskID:       id,
			Partition:    "demo",
			Intent:       "stack",
			Op:           op,
			TargetPusher: "docker",
			Target:       targetdomain.Placement{Cluster: "main"},
			Assets: []taskdomain.AbstractAsset{
				{Type: "Config", Name: "fetcher-config", Properties: map[string]any{"data": map[string]any{"fetcher.json": `{"port":9200}`}}},
				{Type: "Volume", Name: "data", Properties: map[string]any{"size": "20Gi", "accessMode": "ReadWriteOnce"}},
				{Type: "Compute", Name: "app", DependsOn: []string{"data", "fetcher-config"}, Properties: map[string]any{
					"image":    "demo:v1",
					"replicas": 2,
					"ports":    []any{map[string]any{"containerPort": 8080}},
					"env": map[string]any{
						"MONOFS_ENCRYPTION_KEY": map[string]any{"secret_ref": "monofs-secret://shared/encryption-key"},
					},
					"volumeMounts": []any{
						map[string]any{"volume": "data", "path": "/data"},
					},
					"configMounts": []any{
						map[string]any{"config": "fetcher-config", "path": "/etc/demo/fetcher.json", "readOnly": true},
					},
				}},
				{Type: "LoadBalancer", Name: "edge", DependsOn: []string{"app"}, Properties: map[string]any{
					"targets": []any{"app"},
					"listeners": []any{
						map[string]any{"name": "http", "port": 8080, "protocol": "TCP"},
					},
				}},
				{Type: "ObjectStore", Name: "blob", DependsOn: []string{"data"}, Properties: map[string]any{
					"engine": "minio",
					"volume": "data",
				}},
				{Type: "ObjectStore", Name: "external-blob", Properties: map[string]any{
					"engine":   "minio",
					"endpoint": "http://host.docker.internal:19000",
				}},
				{Type: "SQLDatabase", Name: "db", DependsOn: []string{"data"}, Properties: map[string]any{
					"engine": "postgres",
					"volume": "data",
				}},
				{Type: "Observability", Name: "otel", Properties: map[string]any{
					"endpoint":  "0.0.0.0:4317",
					"exporters": []any{"logging"},
				}},
			},
		}
		content, err := json.Marshal(task)
		if err != nil {
			t.Fatalf("marshal task: %v", err)
		}
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("docker", id), Content: content}},
			Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
		}); err != nil {
			t.Fatalf("seed task: %v", err)
		}
		if err := runtime.ProcessPending(ctx); err != nil {
			t.Fatalf("process task: %v", err)
		}
		raw, err := store.ReadFile(ctx, paths.QueueResult("docker", id))
		if err != nil {
			t.Fatalf("read result: %v", err)
		}
		var result taskdomain.TaskResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		return result
	}

	check := run("docker-check", taskdomain.OpCheck)
	if check.Status != taskdomain.ResultSucceeded {
		t.Fatalf("check status = %q, error = %v", check.Status, check.Error)
	}

	apply := run("docker-apply", taskdomain.OpApply)
	if apply.Status != taskdomain.ResultSucceeded {
		t.Fatalf("apply status = %q", apply.Status)
	}
	if apply.Outputs["app.address"] != "app:8080" {
		t.Fatalf("unexpected compute output: %v", apply.Outputs)
	}
	if apply.Outputs["external-blob.endpoint"] != "http://host.docker.internal:19000" {
		t.Fatalf("unexpected external object store output: %v", apply.Outputs)
	}
	if _, ok, err := backend.GetVolume("docker-vol-main-demo-stack-data"); err != nil {
		t.Fatalf("get volume: %v", err)
	} else if !ok {
		t.Fatalf("expected docker volume to be materialized")
	}
	if containers, err := backend.ListContainersByAsset("demo", "stack", "app"); err != nil {
		t.Fatalf("list containers: %v", err)
	} else if len(containers) != 2 {
		t.Fatalf("expected 2 app containers, got %d", len(containers))
	} else if containers[0].Env["MONOFS_ENCRYPTION_KEY"] != "test-encryption-key" {
		t.Fatalf("expected resolved secret env, got %+v", containers[0].Env)
	} else if containers[0].ExtraHosts["host.docker.internal"] != "host-gateway" {
		t.Fatalf("expected default extra host, got %+v", containers[0].ExtraHosts)
	}
	if containers, err := backend.ListContainersByAsset("demo", "stack", "external-blob"); err != nil {
		t.Fatalf("list external object store containers: %v", err)
	} else if len(containers) != 0 {
		t.Fatalf("expected external object store to avoid container provisioning, got %d containers", len(containers))
	}
	writeFile(t, ctx, store, paths.IntentState("demo", "stack"), mustJSON(t, statedomain.IntentState{
		APIVersion: "guardian/v1alpha1",
		Kind:       "IntentState",
		Partition:  "demo",
		Intent:     "stack",
		Outputs: map[string]string{
			"external-blob.endpoint": "http://host.docker.internal:19000",
		},
	}))

	diff := run("docker-diff", taskdomain.OpDiff)
	if diff.Status != taskdomain.ResultSucceeded || diff.Drift == nil || diff.Drift.Status != "InSync" {
		t.Fatalf("diff = %+v", diff)
	}
	if diff.AssetObservations == nil || diff.AssetObservations["app"] == nil {
		t.Fatalf("expected app asset observation, got %+v", diff.AssetObservations)
	}
	if got := diff.AssetObservations["app"].Health; got == nil || got.Status != taskdomain.HealthHealthy {
		t.Fatalf("app health = %+v, want healthy", got)
	}

	destroy := run("docker-destroy", taskdomain.OpDestroy)
	if destroy.Status != taskdomain.ResultSucceeded {
		t.Fatalf("destroy status = %q", destroy.Status)
	}
	if containers, err := backend.ListContainersByAsset("demo", "stack", "app"); err != nil {
		t.Fatalf("list containers after destroy: %v", err)
	} else if len(containers) != 0 {
		t.Fatalf("expected app containers to be removed")
	}
}

func TestDockerNotFoundCaseInsensitive(t *testing.T) {
	if !dockerNotFound("[]\nError response from daemon: get demo: no such volume") {
		t.Fatal("expected lowercase docker not-found message to be recognized")
	}
}

func TestDockerNetworkAlreadyConnectedCaseInsensitive(t *testing.T) {
	if !dockerNetworkAlreadyConnected(errors.New("Error response from daemon: endpoint with name demo already exists in network test-net")) {
		t.Fatal("expected already-connected docker network error to be recognized")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return content
}

func TestDockerComputePayloadOverridesAndDrift(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	reg := registry.New()
	Register(reg, backend, secrets.NewStoreResolver(store))
	runtime := &runtimepkg.Runtime{
		QueuePath: paths.QueueDir("docker"),
		WorkerID:  "docker-worker",
		Store:     store,
		Registry:  reg,
		CanHandle: func(t *taskdomain.Task) bool { return t.Target.Cluster == "main" },
	}

	payloadPath := "/partitions/demo/payloads/stack/app/payload.docker.yaml"
	run := func(id string, op taskdomain.Operation) taskdomain.TaskResult {
		t.Helper()
		task := taskdomain.Task{
			APIVersion:   "guardian/v1alpha1",
			Kind:         "Task",
			TaskID:       id,
			Partition:    "demo",
			Intent:       "stack",
			Op:           op,
			TargetPusher: "docker",
			Target:       targetdomain.Placement{Cluster: "main"},
			Assets: []taskdomain.AbstractAsset{{
				Type:    "Compute",
				Name:    "app",
				Payload: map[string]string{"docker": payloadPath},
				Properties: map[string]any{
					"image": "demo:v1",
					"ports": []any{map[string]any{"containerPort": 8080}},
				},
			}},
		}
		content, err := json.Marshal(task)
		if err != nil {
			t.Fatalf("marshal task: %v", err)
		}
		writeFile(t, ctx, store, paths.QueueTask("docker", id), content)
		if err := runtime.ProcessPending(ctx); err != nil {
			t.Fatalf("process task: %v", err)
		}
		raw, err := store.ReadFile(ctx, paths.QueueResult("docker", id))
		if err != nil {
			t.Fatalf("read result: %v", err)
		}
		var result taskdomain.TaskResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		return result
	}

	writeFile(t, ctx, store, payloadPath, []byte(`
image: demo:v2
ports:
  - name: http
    protocol: TCP
    containerPort: 9090
privileged: true
capabilities:
  - NET_ADMIN
inlineFiles:
  payload.txt: docker
`))

	apply := run("docker-payload-apply", taskdomain.OpApply)
	if apply.Status != taskdomain.ResultSucceeded {
		t.Fatalf("apply status = %q", apply.Status)
	}
	if apply.Outputs["app.image"] != "demo:v2" {
		t.Fatalf("unexpected image output: %v", apply.Outputs)
	}
	if apply.Outputs["app.address"] != "app:9090" {
		t.Fatalf("unexpected address output: %v", apply.Outputs)
	}
	containers, err := backend.ListContainersByAsset("demo", "stack", "app")
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("expected 1 app container, got %d", len(containers))
	}
	if containers[0].Image != "demo:v2" || !containers[0].Privileged {
		t.Fatalf("unexpected payload overlay on container: %+v", containers[0])
	}
	if len(containers[0].Ports) != 1 || containers[0].Ports[0].ContainerPort != 9090 {
		t.Fatalf("unexpected port overlay on container: %+v", containers[0].Ports)
	}
	if containers[0].InlineFiles["payload.txt"] != "docker" {
		t.Fatalf("expected inline file from payload, got %+v", containers[0].InlineFiles)
	}

	diffInSync := run("docker-payload-diff-insync", taskdomain.OpDiff)
	if diffInSync.Status != taskdomain.ResultSucceeded || diffInSync.Drift == nil || diffInSync.Drift.Status != "InSync" {
		t.Fatalf("expected payload-backed resource to be in sync, got %+v", diffInSync)
	}

	writeFile(t, ctx, store, payloadPath, []byte(`
image: demo:v3
ports:
  - name: http
    protocol: TCP
    containerPort: 9191
`))

	diffChanged := run("docker-payload-diff-changed", taskdomain.OpDiff)
	if diffChanged.Status != taskdomain.ResultSucceeded || diffChanged.Drift == nil || diffChanged.Drift.Status != "Changed" {
		t.Fatalf("expected payload change to trigger drift, got %+v", diffChanged)
	}
}

func writeFile(t *testing.T, ctx context.Context, store *memory.Store, path string, content []byte) {
	t.Helper()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: path, Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
	}); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
