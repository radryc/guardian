package kubernetesdriver

import (
	"context"
	"encoding/json"
	"testing"

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

func TestKubernetesDriversApplyDiffDestroy(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	reg := registry.New()
	writeFile(t, ctx, store, "/partitions/shared/secrets/encryption-key", []byte("test-encryption-key\n"))
	Register(reg, backend, secrets.NewStoreResolver(store))
	runtime := &runtimepkg.Runtime{
		QueuePath: paths.QueueDir("k8s"),
		WorkerID:  "k8s-worker",
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
			TargetPusher: "k8s",
			Target:       targetdomain.Placement{Cluster: "main", Namespace: "platform"},
			Assets: []taskdomain.AbstractAsset{
				{Type: "Config", Name: "app-config", Properties: map[string]any{"content": "key: value", "format": "yaml"}},
				{Type: "Config", Name: "dir-config", Properties: map[string]any{"content": "feature: enabled", "format": "yaml"}},
				{Type: "Volume", Name: "data", Properties: map[string]any{"size": "10Gi", "accessMode": "ReadWriteOnce"}},
				{Type: "Compute", Name: "app", DependsOn: []string{"data", "app-config", "dir-config"}, Properties: map[string]any{
					"image":    "demo:v1",
					"replicas": 2,
					"ports":    []any{map[string]any{"containerPort": 8080, "servicePort": 80}},
					"env": map[string]any{
						"MONOFS_ENCRYPTION_KEY": map[string]any{"secret_ref": "monofs-secret://shared/encryption-key"},
					},
					"volumeMounts": []any{
						map[string]any{"volume": "data", "path": "/data"},
					},
					"configMounts": []any{
						map[string]any{"config": "app-config", "path": "/etc/app/config.yaml", "readOnly": true},
						map[string]any{"config": "dir-config", "path": "/etc/app/config.d", "readOnly": true},
					},
					"hostBindMounts": []any{
						map[string]any{"source": "/dev/fuse", "target": "/dev/fuse"},
					},
				}},
				{Type: "LoadBalancer", Name: "edge", DependsOn: []string{"app"}, Properties: map[string]any{
					"targets": []any{"app"},
					"listeners": []any{
						map[string]any{"name": "http", "port": 80, "protocol": "TCP"},
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
			Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("k8s", id), Content: content}},
			Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
		}); err != nil {
			t.Fatalf("seed task: %v", err)
		}
		if err := runtime.ProcessPending(ctx); err != nil {
			t.Fatalf("process task: %v", err)
		}
		raw, err := store.ReadFile(ctx, paths.QueueResult("k8s", id))
		if err != nil {
			t.Fatalf("read result: %v", err)
		}
		var result taskdomain.TaskResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		return result
	}

	check := run("k8s-check", taskdomain.OpCheck)
	if check.Status != taskdomain.ResultSucceeded {
		t.Fatalf("check status = %q, error = %v", check.Status, check.Error)
	}

	apply := run("k8s-apply", taskdomain.OpApply)
	if apply.Status != taskdomain.ResultSucceeded {
		t.Fatalf("apply status = %q", apply.Status)
	}
	if apply.Outputs["app.address"] != "k8s-svc-compute-main-platform-demo-stack-app.platform.svc.cluster.local:80" {
		t.Fatalf("unexpected compute output: %v", apply.Outputs)
	}
	if apply.Outputs["external-blob.endpoint"] != "http://host.docker.internal:19000" {
		t.Fatalf("unexpected external object store output: %v", apply.Outputs)
	}
	deployment, ok, err := backend.GetDeployment("platform", "k8s-compute-main-platform-demo-stack-app")
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if !ok {
		t.Fatalf("expected compute deployment")
	}
	fileMountFound := false
	dirMountFound := false
	for _, mount := range deployment.Container.VolumeMounts {
		switch mount.MountPath {
		case "/etc/app/config.yaml":
			fileMountFound = mount.SubPath == "config.yaml"
		case "/etc/app/config.d":
			dirMountFound = mount.SubPath == ""
		}
	}
	if !fileMountFound {
		t.Fatalf("expected single-file config mount to preserve subPath, got %+v", deployment.Container.VolumeMounts)
	}
	if !dirMountFound {
		t.Fatalf("expected directory config mount to avoid subPath, got %+v", deployment.Container.VolumeMounts)
	}
	hostBindMountFound := false
	for _, mount := range deployment.Container.VolumeMounts {
		if mount.SourceKind == "HostPath" && mount.SourceName == "/dev/fuse" && mount.MountPath == "/dev/fuse" {
			hostBindMountFound = true
			break
		}
	}
	if !hostBindMountFound {
		t.Fatalf("expected host bind mount to be preserved, got %+v", deployment.Container.VolumeMounts)
	}
	if _, ok, err := backend.GetDeployment("platform", "k8s-compute-main-platform-demo-stack-app"); err != nil {
		t.Fatalf("get deployment: %v", err)
	} else if !ok {
		t.Fatalf("expected compute deployment")
	} else if deployment, ok, err := backend.GetDeployment("platform", "k8s-compute-main-platform-demo-stack-app"); err != nil {
		t.Fatalf("get deployment env: %v", err)
	} else if !ok || deployment.Container.Env["MONOFS_ENCRYPTION_KEY"] != "test-encryption-key" {
		t.Fatalf("expected resolved secret env, got %+v", deployment.Container.Env)
	}
	if _, ok, err := backend.GetService("platform", "k8s-svc-lb-main-platform-demo-stack-edge"); err != nil {
		t.Fatalf("get service: %v", err)
	} else if !ok {
		t.Fatalf("expected load balancer service")
	}
	if obs, ok, err := backend.GetDeployment("platform", "k8s-obs-main-platform-demo-stack-otel"); err != nil {
		t.Fatalf("get observability deployment: %v", err)
	} else if !ok {
		t.Fatalf("expected observability deployment")
	} else {
		foundInlineConfig := false
		for _, mount := range obs.Container.VolumeMounts {
			if mount.SourceKind == "InlineFile" && mount.MountPath == "/etc/otelcol/config.yaml" {
				foundInlineConfig = true
				break
			}
		}
		if !foundInlineConfig {
			t.Fatalf("expected observability deployment to mount inline collector config: %+v", obs.Container.VolumeMounts)
		}
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

	diff := run("k8s-diff", taskdomain.OpDiff)
	if diff.Status != taskdomain.ResultSucceeded || diff.Drift == nil || diff.Drift.Status != "InSync" {
		t.Fatalf("diff = %+v", diff)
	}

	destroy := run("k8s-destroy", taskdomain.OpDestroy)
	if destroy.Status != taskdomain.ResultSucceeded {
		t.Fatalf("destroy status = %q", destroy.Status)
	}
	if _, ok, err := backend.GetDeployment("platform", "k8s-compute-main-platform-demo-stack-app"); err != nil {
		t.Fatalf("get deployment after destroy: %v", err)
	} else if ok {
		t.Fatalf("expected compute deployment removed")
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

func TestKubernetesComputeObserveExistingDiffInSync(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	reg := registry.New()
	Register(reg, backend, secrets.NewStoreResolver(store))
	runtime := &runtimepkg.Runtime{
		QueuePath: paths.QueueDir("k8s"),
		WorkerID:  "k8s-worker",
		Store:     store,
		Registry:  reg,
		CanHandle: func(t *taskdomain.Task) bool { return t.Target.Cluster == "k8s-main" },
	}

	if err := backend.UpsertDeployment(Deployment{
		Namespace: "monofs",
		Name:      "node-a",
		Hash:      "bootstrap-hash",
		Replicas:  1,
		Container: Container{Image: "monofs-server:latest"},
	}); err != nil {
		t.Fatalf("seed deployment: %v", err)
	}

	task := taskdomain.Task{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "Task",
		TaskID:       "k8s-observe-existing-diff",
		Partition:    "monofs",
		Intent:       "monofs-storage",
		Op:           taskdomain.OpDiff,
		TargetPusher: "k8s",
		Target:       targetdomain.Placement{Cluster: "k8s-main", Namespace: "monofs"},
		Assets: []taskdomain.AbstractAsset{{
			Type: "Compute",
			Name: "node-a",
			Properties: map[string]any{
				"image":           "monofs-server:latest",
				"observeExisting": true,
			},
		}},
	}
	content, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.QueueTask("k8s", task.TaskID), Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand"},
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	if err := runtime.ProcessPending(ctx); err != nil {
		t.Fatalf("process task: %v", err)
	}
	raw, err := store.ReadFile(ctx, paths.QueueResult("k8s", task.TaskID))
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var result taskdomain.TaskResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Status != taskdomain.ResultSucceeded || result.Drift == nil || result.Drift.Status != "InSync" {
		t.Fatalf("diff result = %+v", result)
	}
}

func TestKubernetesComputePayloadOverridesAndDrift(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	reg := registry.New()
	Register(reg, backend, secrets.NewStoreResolver(store))
	runtime := &runtimepkg.Runtime{
		QueuePath: paths.QueueDir("k8s"),
		WorkerID:  "k8s-worker",
		Store:     store,
		Registry:  reg,
		CanHandle: func(t *taskdomain.Task) bool { return t.Target.Cluster == "main" },
	}

	payloadPath := "/partitions/demo/payloads/stack/app/payload.k8s.yaml"
	run := func(id string, op taskdomain.Operation) taskdomain.TaskResult {
		t.Helper()
		task := taskdomain.Task{
			APIVersion:   "guardian/v1alpha1",
			Kind:         "Task",
			TaskID:       id,
			Partition:    "demo",
			Intent:       "stack",
			Op:           op,
			TargetPusher: "k8s",
			Target:       targetdomain.Placement{Cluster: "main", Namespace: "platform"},
			Assets: []taskdomain.AbstractAsset{{
				Type:    "Compute",
				Name:    "app",
				Payload: map[string]string{"k8s": payloadPath},
				Properties: map[string]any{
					"image":    "demo:v1",
					"replicas": 1,
					"ports":    []any{map[string]any{"containerPort": 8080, "servicePort": 80}},
				},
			}},
		}
		content, err := json.Marshal(task)
		if err != nil {
			t.Fatalf("marshal task: %v", err)
		}
		writeFile(t, ctx, store, paths.QueueTask("k8s", id), content)
		if err := runtime.ProcessPending(ctx); err != nil {
			t.Fatalf("process task: %v", err)
		}
		raw, err := store.ReadFile(ctx, paths.QueueResult("k8s", id))
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
replicas: 3
serviceType: NodePort
servicePorts:
  - name: http
    protocol: TCP
    port: 9090
    targetPort: 9090
readinessProbe:
  tcpSocket:
    port: 8080
  initialDelaySeconds: 600
  periodSeconds: 10
privileged: true
capabilities:
  - NET_ADMIN
`))

	apply := run("k8s-payload-apply", taskdomain.OpApply)
	if apply.Status != taskdomain.ResultSucceeded {
		t.Fatalf("apply status = %q", apply.Status)
	}
	if apply.Outputs["app.image"] != "demo:v2" {
		t.Fatalf("unexpected image output: %v", apply.Outputs)
	}
	if apply.Outputs["app.address"] != "k8s-svc-compute-main-platform-demo-stack-app.platform.svc.cluster.local:9090" {
		t.Fatalf("unexpected address output: %v", apply.Outputs)
	}
	deployment, ok, err := backend.GetDeployment("platform", "k8s-compute-main-platform-demo-stack-app")
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if !ok {
		t.Fatalf("expected compute deployment")
	}
	if deployment.Container.Image != "demo:v2" || deployment.Replicas != 3 || !deployment.Container.Privileged {
		t.Fatalf("unexpected payload overlay on deployment: %+v", deployment)
	}
	if deployment.Container.ReadinessProbe == nil || deployment.Container.ReadinessProbe.InitialDelaySeconds != 600 {
		t.Fatalf("expected readiness probe initial delay override, got %+v", deployment.Container.ReadinessProbe)
	}
	if deployment.Container.ReadinessProbe.TCPSocket == nil || deployment.Container.ReadinessProbe.TCPSocket.Port != 8080 {
		t.Fatalf("expected readiness probe tcp socket override, got %+v", deployment.Container.ReadinessProbe)
	}
	service, ok, err := backend.GetService("platform", "k8s-svc-compute-main-platform-demo-stack-app")
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if !ok {
		t.Fatalf("expected compute service")
	}
	if service.Type != "NodePort" || len(service.Ports) != 1 || service.Ports[0].Port != 9090 {
		t.Fatalf("unexpected payload overlay on service: %+v", service)
	}

	diffInSync := run("k8s-payload-diff-insync", taskdomain.OpDiff)
	if diffInSync.Status != taskdomain.ResultSucceeded || diffInSync.Drift == nil || diffInSync.Drift.Status != "InSync" {
		t.Fatalf("expected payload-backed resource to be in sync, got %+v", diffInSync)
	}

	writeFile(t, ctx, store, payloadPath, []byte(`
image: demo:v3
serviceType: ClusterIP
servicePorts:
  - name: http
    protocol: TCP
    port: 9191
    targetPort: 9191
`))

	diffChanged := run("k8s-payload-diff-changed", taskdomain.OpDiff)
	if diffChanged.Status != taskdomain.ResultSucceeded || diffChanged.Drift == nil || diffChanged.Drift.Status != "Changed" {
		t.Fatalf("expected payload change to trigger drift, got %+v", diffChanged)
	}
}

func TestCLIBackendContainerVolumesRendersHostPathMounts(t *testing.T) {
	backend := &CLIBackend{}
	deployment := Deployment{
		Name: "workspace",
		Container: Container{
			VolumeMounts: []VolumeMount{
				{SourceKind: "HostPath", SourceName: "/dev/fuse", MountPath: "/dev/fuse"},
			},
		},
	}

	volumes, mounts := backend.containerVolumes(deployment)
	if len(volumes) != 1 {
		t.Fatalf("expected one volume, got %+v", volumes)
	}
	if got := volumes[0]["hostPath"]; got == nil {
		t.Fatalf("expected hostPath volume, got %+v", volumes[0])
	}
	hostPath, ok := volumes[0]["hostPath"].(map[string]any)
	if !ok || hostPath["path"] != "/dev/fuse" {
		t.Fatalf("expected hostPath /dev/fuse, got %+v", volumes[0]["hostPath"])
	}
	if len(mounts) != 1 || mounts[0]["mountPath"] != "/dev/fuse" {
		t.Fatalf("expected hostPath mount at /dev/fuse, got %+v", mounts)
	}
}

func TestKubernetesLoadBalancerServiceTypeFromSpec(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	reg := registry.New()
	Register(reg, backend, secrets.NewStoreResolver(store))
	runtime := &runtimepkg.Runtime{
		QueuePath: paths.QueueDir("k8s"),
		WorkerID:  "k8s-worker",
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
			TargetPusher: "k8s",
			Target:       targetdomain.Placement{Cluster: "main", Namespace: "platform"},
			Assets: []taskdomain.AbstractAsset{
				{
					Type: "Compute",
					Name: "app",
					Properties: map[string]any{
						"image": "demo:v1",
						"ports": []any{map[string]any{"containerPort": 8080, "servicePort": 8080}},
					},
				},
				{
					Type:      "LoadBalancer",
					Name:      "edge",
					DependsOn: []string{"app"},
					Properties: map[string]any{
						"serviceType": "NodePort",
						"targets":     []any{"app"},
						"listeners": []any{
							map[string]any{"name": "http", "port": 18080, "protocol": "TCP"},
						},
					},
				},
			},
		}
		content, err := json.Marshal(task)
		if err != nil {
			t.Fatalf("marshal task: %v", err)
		}
		writeFile(t, ctx, store, paths.QueueTask("k8s", id), content)
		if err := runtime.ProcessPending(ctx); err != nil {
			t.Fatalf("process task: %v", err)
		}
		raw, err := store.ReadFile(ctx, paths.QueueResult("k8s", id))
		if err != nil {
			t.Fatalf("read result: %v", err)
		}
		var result taskdomain.TaskResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		return result
	}

	apply := run("k8s-lb-spec-apply", taskdomain.OpApply)
	if apply.Status != taskdomain.ResultSucceeded {
		t.Fatalf("apply status = %q", apply.Status)
	}
	service, ok, err := backend.GetService("platform", "k8s-svc-lb-main-platform-demo-stack-edge")
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if !ok {
		t.Fatalf("expected load balancer service")
	}
	if service.Type != "NodePort" {
		t.Fatalf("expected service type NodePort, got %+v", service)
	}

	diff := run("k8s-lb-spec-diff", taskdomain.OpDiff)
	if diff.Status != taskdomain.ResultSucceeded || diff.Drift == nil || diff.Drift.Status != "InSync" {
		t.Fatalf("expected NodePort load balancer to be in sync, got %+v", diff)
	}
}

func TestKubernetesDiffIgnoresUnreadyReplicas(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	reg := registry.New()
	Register(reg, backend, secrets.NewStoreResolver(store))
	runtime := &runtimepkg.Runtime{
		QueuePath: paths.QueueDir("k8s"),
		WorkerID:  "k8s-worker",
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
			TargetPusher: "k8s",
			Target:       targetdomain.Placement{Cluster: "main", Namespace: "platform"},
			Assets: []taskdomain.AbstractAsset{{
				Type: "Compute",
				Name: "app",
				Properties: map[string]any{
					"image":    "demo:v1",
					"replicas": 1,
					"ports":    []any{map[string]any{"containerPort": 8080, "servicePort": 80}},
				},
			}},
		}
		content, err := json.Marshal(task)
		if err != nil {
			t.Fatalf("marshal task: %v", err)
		}
		writeFile(t, ctx, store, paths.QueueTask("k8s", id), content)
		if err := runtime.ProcessPending(ctx); err != nil {
			t.Fatalf("process task: %v", err)
		}
		raw, err := store.ReadFile(ctx, paths.QueueResult("k8s", id))
		if err != nil {
			t.Fatalf("read result: %v", err)
		}
		var result taskdomain.TaskResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		return result
	}

	apply := run("k8s-unready-apply", taskdomain.OpApply)
	if apply.Status != taskdomain.ResultSucceeded {
		t.Fatalf("apply status = %q", apply.Status)
	}

	deployment, ok, err := backend.GetDeployment("platform", "k8s-compute-main-platform-demo-stack-app")
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if !ok {
		t.Fatalf("expected compute deployment")
	}
	deployment.ReadyReplicas = 0
	deployment.AvailableReplicas = 0
	if err := backend.UpsertDeployment(deployment); err != nil {
		t.Fatalf("update deployment readiness: %v", err)
	}

	diff := run("k8s-unready-diff", taskdomain.OpDiff)
	// Transient pod unreadiness must NOT trigger drift — only hash/replica-count
	// changes in the spec constitute configuration drift.
	if diff.Status != taskdomain.ResultSucceeded || diff.Drift == nil || diff.Drift.Status != "InSync" {
		t.Fatalf("expected unready replicas to be ignored (InSync), got %+v", diff)
	}
	if diff.Health == nil || diff.Health.Status != taskdomain.HealthDegraded {
		t.Fatalf("expected degraded health observation, got %+v", diff.Health)
	}
	if diff.ApplyReadiness == nil || diff.ApplyReadiness.Status != taskdomain.ApplyReadinessReady {
		t.Fatalf("expected ready apply-readiness observation, got %+v", diff.ApplyReadiness)
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
