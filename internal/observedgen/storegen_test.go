package observedgen

import (
	"context"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/compiler/manifest"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func seedStore(t *testing.T, ctx context.Context, store guardianapi.Store, logicalPath, content string) {
	t.Helper()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: []byte(content)}},
	}); err != nil {
		t.Fatalf("seed %s: %v", logicalPath, err)
	}
}

func TestGenerateToStore(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	assemblyDir := "/partitions/strata-aws/payloads/aws/cdk.out"
	seedStore(t, ctx, store, assemblyDir+"/manifest.json", `{
	  "version": "39.0.0",
	  "artifacts": {
	    "StrataStack": {
	      "type": "aws:cloudformation:stack",
	      "properties": {"templateFile": "StrataStack.template.json"}
	    },
	    "DemoStack": {
	      "type": "aws:cloudformation:stack",
	      "properties": {"templateFile": "DemoStack.template.json", "stackName": "DemoStack"}
	    }
	  }
	}`)
	seedStore(t, ctx, store, assemblyDir+"/StrataStack.template.json", `{"Resources":{"Vpc":{"Type":"AWS::EC2::VPC","Properties":{}}}}`)
	seedStore(t, ctx, store, assemblyDir+"/DemoStack.template.json", `{"Resources":{}}`)
	seedStore(t, ctx, store, assemblyDir+"/StrataStack.assets.json", `{"version":"39.0.0"}`)

	created, err := GenerateToStore(ctx, store, StoreOptions{
		AssemblyDir: assemblyDir,
		Partition:   "strata-aws",
		Account:     "123456789012",
		Region:      "eu-west-1",
		Pusher:      "aws-123456789012",
	})
	if err != nil {
		t.Fatalf("GenerateToStore() error = %v", err)
	}
	if len(created) != 2 || created[0] != "demo-stack" || created[1] != "strata-stack" {
		t.Fatalf("created = %v", created)
	}

	configContent, err := store.ReadFile(ctx, paths.PartitionConfig("strata-aws"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(configContent), "name: strata-aws") {
		t.Fatalf("config = %s", configContent)
	}

	intentContent, err := store.ReadFile(ctx, paths.IntentManifest("strata-aws", "strata-stack"))
	if err != nil {
		t.Fatalf("read intent: %v", err)
	}
	parsed, err := manifest.ParseIntent(intentContent)
	if err != nil {
		t.Fatalf("parse intent: %v", err)
	}
	if parsed.Spec.IntentType != "observed" {
		t.Fatalf("intentType = %q, want observed", parsed.Spec.IntentType)
	}
	if parsed.Spec.TargetPusher != "aws-123456789012" {
		t.Fatalf("targetPusher = %q", parsed.Spec.TargetPusher)
	}
	if parsed.Spec.Assets[0].Payload["aws"] != "/partitions/strata-aws/payloads/aws/strata-stack.yaml" {
		t.Fatalf("payload = %v", parsed.Spec.Assets[0].Payload)
	}

	payloadContent, err := store.ReadFile(ctx, "/partitions/strata-aws/payloads/aws/strata-stack.yaml")
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	if !strings.Contains(string(payloadContent), "prebuiltAssemblyDir: /partitions/strata-aws/payloads/aws/cdk.out") {
		t.Fatalf("payload = %s", payloadContent)
	}
	if _, err := store.ReadFile(ctx, assemblyDir+"/StrataStack.template.json"); err != nil {
		t.Fatalf("template missing: %v", err)
	}
	if _, err := store.ReadFile(ctx, "/partitions/strata-aws/payloads/aws/cdk.out/StrataStack.template.json"); err != nil {
		t.Fatalf("copied template missing: %v", err)
	}
}

func TestGenerateToStoreManagedCopiesStagedAssets(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	assemblyDir := "/partitions/strata-aws/payloads/aws/cdk.out"
	seedStore(t, ctx, store, assemblyDir+"/manifest.json", `{
	  "version": "39.0.0",
	  "artifacts": {
	    "AppStack": {
	      "type": "aws:cloudformation:stack",
	      "properties": {"templateFile": "AppStack.template.json", "stackName": "AppStack"}
	    }
	  }
	}`)
	seedStore(t, ctx, store, assemblyDir+"/AppStack.template.json", `{"Resources":{"Bucket":{"Type":"AWS::S3::Bucket"}}}`)
	seedStore(t, ctx, store, assemblyDir+"/asset.abc/bundle.zip", "asset")

	created, err := GenerateToStore(ctx, store, StoreOptions{
		Mode:        ModeManaged,
		AssemblyDir: assemblyDir,
		Partition:   "strata-aws",
		Account:     "123456789012",
		Region:      "eu-west-1",
		Pusher:      "aws-123456789012",
	})
	if err != nil {
		t.Fatalf("GenerateToStore() error = %v", err)
	}
	if len(created) != 1 || created[0] != "app-stack" {
		t.Fatalf("created = %v", created)
	}
	intentContent, err := store.ReadFile(ctx, paths.IntentManifest("strata-aws", "app-stack"))
	if err != nil {
		t.Fatalf("read intent: %v", err)
	}
	parsed, err := manifest.ParseIntent(intentContent)
	if err != nil {
		t.Fatalf("parse intent: %v", err)
	}
	if parsed.Spec.IntentType != "standard" {
		t.Fatalf("intentType = %q, want standard", parsed.Spec.IntentType)
	}
	if _, err := store.ReadFile(ctx, "/partitions/strata-aws/payloads/aws/cdk.out/asset.abc/bundle.zip"); err != nil {
		t.Fatalf("staged asset missing from managed bundle: %v", err)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"AwsPusherStack": "aws-pusher-stack",
		"StrataStack":    "strata-stack",
		"my_stack":       "my-stack",
		"123Bad":         "stack-123-bad",
	}
	for input, want := range cases {
		if got := Slug(input); got != want {
			t.Fatalf("Slug(%q) = %q, want %q", input, got, want)
		}
	}
}
