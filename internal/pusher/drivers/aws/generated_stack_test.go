package awsdriver

import (
	"context"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/cdkgen"
	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	targetdomain "github.com/radryc/guardian/internal/domain/target"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/pusher/secrets"
	"github.com/radryc/guardian/internal/store/memory"
)

func generatedStackInput() cdkgen.Input {
	return cdkgen.Input{
		Partition: "demo",
		Intent:    "stack",
		Account:   "123456789012",
		Region:    "eu-west-1",
		SourceDir: paths.GeneratedAWSPayloadDir("demo", "stack"),
		Order:     []string{"data", "bucket", "web"},
		Assets: []assetdomain.Spec{
			{Type: "Volume", Name: "data", Properties: map[string]any{"size": "1Gi"}},
			{Type: "ObjectStore", Name: "bucket", Properties: map[string]any{"engine": "s3", "versioning": true}},
			{Type: "Compute", Name: "web", Properties: map[string]any{"image": "public.ecr.aws/nginx/nginx:latest"}},
		},
	}
}

func TestGeneratedStackDriverLifecycle(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	backend.SetBootstrapReady("123456789012", "eu-west-1", true)

	result, err := cdkgen.Generate(generatedStackInput())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !result.Generated {
		t.Fatal("expected generated payload")
	}
	for rel, content := range result.Files {
		writeFile(t, ctx, store, paths.GeneratedAWSPayloadDir("demo", "stack")+"/"+rel, content)
	}

	driver := &GeneratedStackDriver{baseDriver{backend: backend, resolver: secrets.NoopResolver{}}}
	in := registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "stack",
		Target:        targetdomain.Placement{Account: "123456789012", Region: "eu-west-1"},
		Asset: taskdomain.AbstractAsset{
			Type:    assetdomain.TypeGeneratedStack,
			Name:    "aws-stack",
			Payload: map[string]string{"generated": paths.GeneratedAWSPayloadDir("demo", "stack")},
		},
		Store: store,
	}

	drift, err := driver.Diff(ctx, in)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if drift.Status != "Changed" {
		t.Fatalf("diff status = %q, want Changed", drift.Status)
	}
	if len(drift.ChangedAssets) != 3 {
		t.Fatalf("changed assets = %v, want all generated assets", drift.ChangedAssets)
	}

	applied, err := driver.Apply(ctx, in)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.Outputs["stackName"] != result.Manifest.StackName {
		t.Fatalf("stackName output = %q", applied.Outputs["stackName"])
	}

	backend.SetStackOutputs("123456789012", "eu-west-1", result.Manifest.StackName, map[string]string{
		"OutWebService":   "aws-svc-demo-stack-web",
		"OutBucketBucket": "guardian-bucket",
	})
	applied, err = driver.Apply(ctx, in)
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if got := applied.Outputs["web.service"]; got != "aws-svc-demo-stack-web" {
		t.Fatalf("web.service = %q", got)
	}
	if got := applied.Outputs["bucket.bucket"]; got != "guardian-bucket" {
		t.Fatalf("bucket.bucket = %q", got)
	}

	drift, err = driver.Diff(ctx, in)
	if err != nil {
		t.Fatalf("diff after apply: %v", err)
	}
	if drift.Status != "InSync" {
		t.Fatalf("diff after apply = %+v, want InSync", drift)
	}

	backend.SetDriftStatus("123456789012", "eu-west-1", result.Manifest.StackName, StackDriftDrifted)
	drift, err = driver.Diff(ctx, in)
	if err != nil {
		t.Fatalf("drifted diff: %v", err)
	}
	if drift.Status != "Changed" || len(drift.ChangedAssets) == 0 {
		t.Fatalf("drifted diff = %+v", drift)
	}

	last := backend.LastRequest()
	if last.Manifest.StackName != result.Manifest.StackName {
		t.Fatalf("stack name request = %q", last.Manifest.StackName)
	}
	if last.DesiredHash == "" || last.Tags["guardian.hash"] == "" {
		t.Fatalf("expected desired hash and tags, got %+v", last.Tags)
	}

	if err := driver.Destroy(ctx, in); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, ok, err := backend.GetStack(ctx, StackRequest{
		Target:   targetdomain.Placement{Account: "123456789012", Region: "eu-west-1"},
		Manifest: stackPayload{StackName: result.Manifest.StackName},
	}); err != nil {
		t.Fatalf("get stack after destroy: %v", err)
	} else if ok {
		t.Fatal("expected stack deleted")
	}
}

// TestGeneratedStackTemplateChangeFlipsHash guards the contract that template
// files are staged and hashed: editing a template (for example through a store
// overlay) must change the desired hash so DIFF re-deploys the stack.
func TestGeneratedStackTemplateChangeFlipsHash(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	backend := NewBackend()
	backend.SetBootstrapReady("123456789012", "eu-west-1", true)

	result, err := cdkgen.Generate(generatedStackInput())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := paths.GeneratedAWSPayloadDir("demo", "stack")
	for rel, content := range result.Files {
		writeFile(t, ctx, store, dir+"/"+rel, content)
	}

	driver := &GeneratedStackDriver{baseDriver{backend: backend, resolver: secrets.NoopResolver{}}}
	in := registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "stack",
		Target:        targetdomain.Placement{Account: "123456789012", Region: "eu-west-1"},
		Asset: taskdomain.AbstractAsset{
			Type:    assetdomain.TypeGeneratedStack,
			Name:    "aws-stack",
			Payload: map[string]string{"generated": dir},
		},
		Store: store,
	}

	if _, err := driver.Apply(ctx, in); err != nil {
		t.Fatalf("apply: %v", err)
	}
	before := backend.LastRequest().DesiredHash
	if before == "" {
		t.Fatal("expected a desired hash")
	}
	if drift, err := driver.Diff(ctx, in); err != nil {
		t.Fatalf("diff after apply: %v", err)
	} else if drift.Status != "InSync" {
		t.Fatalf("diff after apply = %+v, want InSync", drift)
	}

	// Simulate an overlay template change in the payload.
	templatePath := dir + "/templates/compute.fargate/construct.ts"
	raw, err := store.ReadFile(ctx, templatePath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	writeFile(t, ctx, store, templatePath, append(raw, []byte("\n// overlay tweak\n")...))

	drift, err := driver.Diff(ctx, in)
	if err != nil {
		t.Fatalf("diff after template change: %v", err)
	}
	if drift.Status != "Changed" {
		t.Fatalf("template content change must flip the diff to Changed, got %+v", drift)
	}
	if after := backend.LastRequest().DesiredHash; after == before {
		t.Fatal("template content change must change the desired hash")
	}
}

// TestGeneratedPhysicalNameParity guards the contract that generated
// constructs reference the same physical names the native drivers create
// (config SSM parameters, ECS services, EFS filesystems, log groups, buckets).
func TestGeneratedPhysicalNameParity(t *testing.T) {
	target := targetdomain.Placement{Account: "975049940689", Region: "us-east-1"}
	computeIn := registry.AssetInput{
		PartitionName: "aws-demo",
		IntentName:    "demo-stack",
		Target:        target,
		Asset:         taskdomain.AbstractAsset{Type: "Compute", Name: "nginx"},
	}
	configIn := computeIn
	configIn.Asset = taskdomain.AbstractAsset{Type: "Config", Name: "nginx-page"}
	bucketIn := computeIn
	bucketIn.Asset = taskdomain.AbstractAsset{Type: "ObjectStore", Name: "demo-bucket"}

	wantService := awsServiceName(computeIn)
	wantTaskFamily := awsTaskFamily("task", computeIn)
	wantLogGroup := awsLogGroupName(computeIn)
	wantEFS := awsEFSName(computeIn, "nginx-data")
	wantSSM := awsSSMParameterName(configIn, "nginx-page")
	wantBucket := awsS3BucketName(bucketIn)

	result, err := cdkgen.Generate(cdkgen.Input{
		Partition: "aws-demo",
		Intent:    "demo-stack",
		Account:   target.Account,
		Region:    target.Region,
		SourceDir: paths.GeneratedAWSPayloadDir("aws-demo", "demo-stack"),
		Order:     []string{"nginx-data", "demo-bucket", "nginx"},
		Assets: []assetdomain.Spec{
			{Type: "Volume", Name: "nginx-data", Properties: map[string]any{"size": "1Gi"}},
			{Type: "ObjectStore", Name: "demo-bucket", Properties: map[string]any{"engine": "s3"}},
			{Type: "Compute", Name: "nginx", Properties: map[string]any{
				"image":        "public.ecr.aws/nginx/nginx:latest",
				"configMounts": []any{map[string]any{"config": "nginx-page", "path": "/usr/share/nginx/html"}},
			}},
		},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	stack := string(result.Files["lib/stack.ts"])
	for name, value := range map[string]string{
		"service name": wantService,
		"task family":  wantTaskFamily,
		"log group":    wantLogGroup,
		"efs name":     wantEFS,
		"ssm param":    wantSSM,
		"bucket name":  wantBucket,
	} {
		if !strings.Contains(stack, value) {
			t.Errorf("generated stack missing %s %q:\n%s", name, value, stack)
		}
	}
}

func TestGeneratedStackDriverRequiresManifest(t *testing.T) {
	driver := &GeneratedStackDriver{baseDriver{backend: NewBackend(), resolver: secrets.NoopResolver{}}}
	in := registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "stack",
		Target:        targetdomain.Placement{Account: "123456789012", Region: "eu-west-1"},
		Asset: taskdomain.AbstractAsset{
			Type:    assetdomain.TypeGeneratedStack,
			Name:    "aws-stack",
			Payload: map[string]string{"generated": "/partitions/demo/payloads/aws-generated/stack"},
		},
		Store: memory.New(),
	}
	if err := driver.Check(context.Background(), in); err == nil {
		t.Fatal("expected missing manifest error")
	}
}
