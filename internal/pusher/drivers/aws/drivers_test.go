package awsdriver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	runtimepkg "github.com/rydzu/ainfra/guardian/internal/pusher/runtime"
	"github.com/rydzu/ainfra/guardian/internal/pusher/secrets"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestAWSDriverApplyDiffDestroy(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	backend.SetBootstrapReady("123456789012", "eu-west-1", true)
	backend.SetStackOutputs("123456789012", "eu-west-1", "guardian-demo-network", map[string]string{
		"VpcId":          "vpc-123",
		"PrivateSubnetA": "subnet-456",
	})

	reg := registry.New()
	writeFile(t, ctx, store, "/partitions/shared/secrets/team-token", []byte("super-secret-token\n"))
	writeFile(t, ctx, store, "/partitions/demo/payloads/aws/network/stack.yaml", []byte(`
sourceType: cdk-ts
sourceDir: /partitions/demo/payloads/aws/network/src
entrypoint: bin/app.ts
stackName: guardian-demo-network
stackID: NetworkStack
packageManager: none
context:
  baseEnv: shared
outputMap:
  vpcId: VpcId
  subnetA: PrivateSubnetA
`))
	writeFile(t, ctx, store, "/partitions/demo/payloads/aws/network/src/package.json", []byte(`{"name":"network","private":true}`))
	writeFile(t, ctx, store, "/partitions/demo/payloads/aws/network/src/bin/app.ts", []byte(`console.log("network stack");`))
	Register(reg, backend, secrets.NewStoreResolver(store))

	runtime := &runtimepkg.Runtime{
		QueuePath: paths.QueueDir("aws"),
		WorkerID:  "aws-worker",
		Store:     store,
		Registry:  reg,
		CanHandle: func(task *taskdomain.Task) bool {
			return task.Target.Account == "123456789012" && task.Target.Region == "eu-west-1"
		},
	}

	run := func(id string, op taskdomain.Operation) taskdomain.TaskResult {
		t.Helper()
		task := taskdomain.Task{
			APIVersion:   "guardian/v1alpha1",
			Kind:         "Task",
			TaskID:       id,
			Partition:    "demo",
			Intent:       "network",
			Op:           op,
			TargetPusher: "aws",
			Target: targetdomain.Placement{
				Account: "123456789012",
				Region:  "eu-west-1",
			},
			Assets: []taskdomain.AbstractAsset{{
				Type: "CDKStack",
				Name: "network",
				Payload: map[string]string{
					"aws": "/partitions/demo/payloads/aws/network/stack.yaml",
				},
				Properties: map[string]any{
					"context": map[string]any{
						"envName": "prod",
					},
					"env": map[string]any{
						"TEAM_TOKEN": map[string]any{
							"secret_ref": "monofs-secret://shared/team-token",
						},
					},
				},
			}},
		}
		content, err := json.Marshal(task)
		if err != nil {
			t.Fatalf("marshal task: %v", err)
		}
		writeFile(t, ctx, store, paths.QueueTask("aws", id), content)
		if err := runtime.ProcessPending(ctx); err != nil {
			t.Fatalf("process task: %v", err)
		}
		raw, err := store.ReadFile(ctx, paths.QueueResult("aws", id))
		if err != nil {
			t.Fatalf("read result: %v", err)
		}
		var result taskdomain.TaskResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		return result
	}

	check := run("aws-check", taskdomain.OpCheck)
	if check.Status != taskdomain.ResultSucceeded {
		t.Fatalf("check status = %q, error = %v", check.Status, check.Error)
	}

	apply := run("aws-apply", taskdomain.OpApply)
	if apply.Status != taskdomain.ResultSucceeded {
		t.Fatalf("apply status = %q, error = %v", apply.Status, apply.Error)
	}
	if got := apply.Outputs["network.vpcId"]; got != "vpc-123" {
		t.Fatalf("network.vpcId = %q, want %q", got, "vpc-123")
	}
	if got := apply.Outputs["stackName"]; got != "guardian-demo-network" {
		t.Fatalf("stackName = %q, want guardian-demo-network", got)
	}
	last := backend.LastRequest()
	if last.DesiredHash == "" {
		t.Fatalf("expected desired hash to be populated")
	}
	if last.Context["baseEnv"] != "shared" || last.Context["envName"] != "prod" {
		t.Fatalf("unexpected merged context: %+v", last.Context)
	}
	if last.Env["TEAM_TOKEN"] != "super-secret-token" {
		t.Fatalf("expected resolved env secret, got %+v", last.Env)
	}
	if last.Tags["guardian.hash"] == "" {
		t.Fatalf("expected guardian hash tag, got %+v", last.Tags)
	}
	if _, err := os.Stat(last.WorkspaceDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected workspace cleanup, stat err = %v", err)
	}

	diff := run("aws-diff", taskdomain.OpDiff)
	if diff.Status != taskdomain.ResultSucceeded || diff.Drift == nil || diff.Drift.Status != "InSync" {
		t.Fatalf("diff = %+v", diff)
	}

	backend.SetDriftStatus("123456789012", "eu-west-1", "guardian-demo-network", StackDriftDrifted)
	drifted := run("aws-drift", taskdomain.OpDiff)
	if drifted.Drift == nil || drifted.Drift.Status != "Changed" {
		t.Fatalf("expected drifted stack, got %+v", drifted.Drift)
	}

	destroy := run("aws-destroy", taskdomain.OpDestroy)
	if destroy.Status != taskdomain.ResultSucceeded {
		t.Fatalf("destroy status = %q, error = %v", destroy.Status, destroy.Error)
	}
	if _, ok, err := backend.GetStack(ctx, StackRequest{
		Target:   targetdomain.Placement{Account: "123456789012", Region: "eu-west-1"},
		Manifest: stackPayload{StackName: "guardian-demo-network"},
	}); err != nil {
		t.Fatalf("get stack after destroy: %v", err)
	} else if ok {
		t.Fatalf("expected stack to be removed")
	}
}

func TestStageSourceTreeRejectsEmptyDirectory(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	writeFile(t, ctx, store, "/partitions/demo/payloads/aws/network/stack.yaml", []byte("placeholder"))
	if _, _, cleanup, err := stageSourceTree(ctx, store, "/partitions/demo/payloads/aws/network/src"); err == nil {
		cleanup()
		t.Fatal("expected stageSourceTree() to fail for missing source dir")
	}
}

func TestAWSDriverApplyDiffWithPrebuiltAssembly(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	backend.SetBootstrapReady("123456789012", "eu-west-1", true)
	backend.SetStackOutputs("123456789012", "eu-west-1", "guardian-demo-network", map[string]string{
		"VpcId": "vpc-prebuilt",
	})

	reg := registry.New()
	writeFile(t, ctx, store, "/partitions/demo/payloads/aws/network/stack.yaml", []byte(`
sourceType: cdk-ts
prebuiltAssemblyDir: /partitions/demo/payloads/aws/network/cdk.out
stackName: guardian-demo-network
stackID: NetworkStack
packageManager: none
outputMap:
  vpcId: VpcId
`))
	writeFile(t, ctx, store, "/partitions/demo/payloads/aws/network/cdk.out/manifest.json", []byte(`{"version":"1.0"}`))
	writeFile(t, ctx, store, "/partitions/demo/payloads/aws/network/cdk.out/tree.json", []byte(`{"tree":{}}`))
	Register(reg, backend, secrets.NewStoreResolver(store))

	runtime := &runtimepkg.Runtime{
		QueuePath: paths.QueueDir("aws"),
		WorkerID:  "aws-worker",
		Store:     store,
		Registry:  reg,
		CanHandle: func(task *taskdomain.Task) bool {
			return task.Target.Account == "123456789012" && task.Target.Region == "eu-west-1"
		},
	}

	run := func(id string, op taskdomain.Operation) taskdomain.TaskResult {
		t.Helper()
		task := taskdomain.Task{
			APIVersion:   "guardian/v1alpha1",
			Kind:         "Task",
			TaskID:       id,
			Partition:    "demo",
			Intent:       "network",
			Op:           op,
			TargetPusher: "aws",
			Target: targetdomain.Placement{
				Account: "123456789012",
				Region:  "eu-west-1",
			},
			Assets: []taskdomain.AbstractAsset{{
				Type: "CDKStack",
				Name: "network",
				Payload: map[string]string{
					"aws": "/partitions/demo/payloads/aws/network/stack.yaml",
				},
			}},
		}
		content, err := json.Marshal(task)
		if err != nil {
			t.Fatalf("marshal task: %v", err)
		}
		writeFile(t, ctx, store, paths.QueueTask("aws", id), content)
		if err := runtime.ProcessPending(ctx); err != nil {
			t.Fatalf("process task: %v", err)
		}
		raw, err := store.ReadFile(ctx, paths.QueueResult("aws", id))
		if err != nil {
			t.Fatalf("read result: %v", err)
		}
		var result taskdomain.TaskResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		return result
	}

	check := run("aws-prebuilt-check", taskdomain.OpCheck)
	if check.Status != taskdomain.ResultSucceeded {
		t.Fatalf("check status = %q, error = %v", check.Status, check.Error)
	}

	apply := run("aws-prebuilt-apply", taskdomain.OpApply)
	if apply.Status != taskdomain.ResultSucceeded {
		t.Fatalf("apply status = %q, error = %v", apply.Status, apply.Error)
	}
	if got := apply.Outputs["network.vpcId"]; got != "vpc-prebuilt" {
		t.Fatalf("network.vpcId = %q, want %q", got, "vpc-prebuilt")
	}
	last := backend.LastRequest()
	if last.AppCommand != last.WorkspaceDir {
		t.Fatalf("expected app command to point to staged prebuilt assembly, got app=%q workspace=%q", last.AppCommand, last.WorkspaceDir)
	}
	if last.DesiredHash == "" {
		t.Fatal("expected desired hash in prebuilt mode")
	}
	if _, err := os.Stat(last.WorkspaceDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected staged prebuilt assembly cleanup, stat err = %v", err)
	}

	diff := run("aws-prebuilt-diff", taskdomain.OpDiff)
	if diff.Status != taskdomain.ResultSucceeded || diff.Drift == nil || diff.Drift.Status != "InSync" {
		t.Fatalf("diff = %+v", diff)
	}
}

func TestAWSDriverPrebuiltAssemblyRequiresManifest(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	backend.SetBootstrapReady("123456789012", "eu-west-1", true)

	reg := registry.New()
	writeFile(t, ctx, store, "/partitions/demo/payloads/aws/network/stack.yaml", []byte(`
sourceType: cdk-ts
prebuiltAssemblyDir: /partitions/demo/payloads/aws/network/cdk.out
stackName: guardian-demo-network
stackID: NetworkStack
packageManager: none
`))
	writeFile(t, ctx, store, "/partitions/demo/payloads/aws/network/cdk.out/tree.json", []byte(`{"tree":{}}`))
	Register(reg, backend, secrets.NewStoreResolver(store))

	runtime := &runtimepkg.Runtime{
		QueuePath: paths.QueueDir("aws"),
		WorkerID:  "aws-worker",
		Store:     store,
		Registry:  reg,
		CanHandle: func(task *taskdomain.Task) bool {
			return task.Target.Account == "123456789012" && task.Target.Region == "eu-west-1"
		},
	}

	task := taskdomain.Task{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "Task",
		TaskID:       "aws-prebuilt-missing-manifest",
		Partition:    "demo",
		Intent:       "network",
		Op:           taskdomain.OpCheck,
		TargetPusher: "aws",
		Target: targetdomain.Placement{
			Account: "123456789012",
			Region:  "eu-west-1",
		},
		Assets: []taskdomain.AbstractAsset{{
			Type: "CDKStack",
			Name: "network",
			Payload: map[string]string{
				"aws": "/partitions/demo/payloads/aws/network/stack.yaml",
			},
		}},
	}
	content, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	writeFile(t, ctx, store, paths.QueueTask("aws", task.TaskID), content)
	if err := runtime.ProcessPending(ctx); err != nil {
		t.Fatalf("process task: %v", err)
	}
	raw, err := store.ReadFile(ctx, paths.QueueResult("aws", task.TaskID))
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var result taskdomain.TaskResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Status != taskdomain.ResultFailed {
		t.Fatalf("expected failed result, got %q", result.Status)
	}
	if result.Error == nil || *result.Error == "" {
		t.Fatalf("expected error message in failed result: %+v", result.Error)
	}
}

func writeFile(t *testing.T, ctx context.Context, store guardianapi.WriteStore, logicalPath string, content []byte) {
	t.Helper()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "test"},
	}); err != nil {
		t.Fatalf("write %s: %v", logicalPath, err)
	}
}
