package observedgen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	"gopkg.in/yaml.v3"
)

const liveTemplateJSON = `{
  "AWSTemplateFormatVersion": "2010-09-09",
  "Resources": {
    "Queue": { "Type": "AWS::SQS::Queue", "Properties": { "QueueName": "guardian-queue" } }
  }
}`

func TestSnapshotBuildsObservedBundle(t *testing.T) {
	files, err := Snapshot(SnapshotOptions{
		Partition: "strata-aws",
		StackName: "AwsPusherStack",
		Account:   "123456789012",
		Region:    "eu-west-1",
		Pusher:    "aws-123456789012",
		Template:  liveTemplateJSON,
	})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	for _, path := range []string{
		"config.yaml",
		"intents/aws-pusher-stack.yaml",
		"payloads/aws/aws-pusher-stack.yaml",
		"payloads/aws/cdk.out/manifest.json",
		"payloads/aws/cdk.out/AwsPusherStack.template.json",
	} {
		if _, ok := files[path]; !ok {
			t.Errorf("snapshot missing %s", path)
		}
	}

	partition, err := manifest.ParsePartition(files["config.yaml"])
	if err != nil {
		t.Fatalf("parse partition: %v", err)
	}
	if partition.Metadata.Name != "strata-aws" {
		t.Fatalf("partition name = %q", partition.Metadata.Name)
	}
	// The adopted marker lets the UI distinguish an adopted existing stack
	// from an observe-only snapshot.
	if partition.Metadata.Labels["guardian.io/origin"] != "adopted" {
		t.Fatalf("adopted origin label = %q", partition.Metadata.Labels["guardian.io/origin"])
	}
	if partition.Metadata.Labels["guardian.io/adopted-stack"] != "AwsPusherStack" {
		t.Fatalf("adopted stack label = %q", partition.Metadata.Labels["guardian.io/adopted-stack"])
	}

	intent, err := manifest.ParseIntent(files["intents/aws-pusher-stack.yaml"])
	if err != nil {
		t.Fatalf("parse intent: %v", err)
	}
	if intent.Spec.IntentType != "observed" {
		t.Fatalf("intent type = %q", intent.Spec.IntentType)
	}
	if intent.Spec.TargetPusher != "aws-123456789012" {
		t.Fatalf("pusher = %q", intent.Spec.TargetPusher)
	}
	if got := intent.Spec.Assets[0].Payload["aws"]; got != "/partitions/strata-aws/payloads/aws/aws-pusher-stack.yaml" {
		t.Fatalf("payload path = %q", got)
	}

	var payload struct {
		PrebuiltAssemblyDir string `yaml:"prebuiltAssemblyDir"`
		StackName           string `yaml:"stackName"`
		Snapshot            bool   `yaml:"snapshot"`
	}
	if err := yaml.Unmarshal(files["payloads/aws/aws-pusher-stack.yaml"], &payload); err != nil {
		t.Fatalf("parse payload manifest: %v", err)
	}
	if payload.StackName != "AwsPusherStack" {
		t.Fatalf("payload stack name = %q", payload.StackName)
	}
	if !payload.Snapshot {
		t.Fatal("snapshot payload must be marked snapshot: true")
	}
	if payload.PrebuiltAssemblyDir != "/partitions/strata-aws/payloads/aws/cdk.out" {
		t.Fatalf("assembly dir = %q", payload.PrebuiltAssemblyDir)
	}

	var assembly struct {
		Artifacts map[string]struct {
			Type       string `json:"type"`
			Properties struct {
				TemplateFile string `json:"templateFile"`
				StackName    string `json:"stackName"`
			} `json:"properties"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(files["payloads/aws/cdk.out/manifest.json"], &assembly); err != nil {
		t.Fatalf("parse assembly manifest: %v", err)
	}
	artifact, ok := assembly.Artifacts["AwsPusherStack"]
	if !ok {
		t.Fatalf("assembly missing stack artifact: %v", assembly.Artifacts)
	}
	if artifact.Type != "aws:cloudformation:stack" || artifact.Properties.TemplateFile != "AwsPusherStack.template.json" {
		t.Fatalf("artifact = %+v", artifact)
	}

	template := files["payloads/aws/cdk.out/AwsPusherStack.template.json"]
	if !json.Valid(template) {
		t.Fatalf("snapshot template is not JSON: %s", template)
	}
	if !strings.Contains(string(template), "guardian-queue") {
		t.Fatalf("snapshot template lost resources: %s", template)
	}
}

func TestSnapshotNormalizesYAMLTemplate(t *testing.T) {
	yamlTemplate := `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  Queue:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: guardian-queue
  Policy:
    Type: AWS::SQS::QueuePolicy
    Properties:
      Queues: [!Ref Queue]
`
	files, err := Snapshot(SnapshotOptions{
		Partition: "p",
		StackName: "Stack",
		Account:   "1",
		Region:    "eu-west-1",
		Pusher:    "aws-1",
		Template:  yamlTemplate,
	})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	raw := files["payloads/aws/cdk.out/Stack.template.json"]
	if !json.Valid(raw) {
		t.Fatalf("normalized template is not JSON: %s", raw)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resources, _ := parsed["Resources"].(map[string]any)
	if resources == nil {
		t.Fatalf("normalized template missing resources: %s", raw)
	}
	policy, _ := resources["Policy"].(map[string]any)
	properties, _ := policy["Properties"].(map[string]any)
	queues, _ := properties["Queues"].([]any)
	if len(queues) != 1 {
		t.Fatalf("queues = %#v", properties["Queues"])
	}
	ref, _ := queues[0].(map[string]any)
	if ref["Ref"] != "Queue" {
		t.Fatalf("short-form !Ref was not expanded: %#v", queues[0])
	}
}

func TestSnapshotUsesCustomIntent(t *testing.T) {
	files, err := Snapshot(SnapshotOptions{
		Partition: "customer-prod-1",
		Intent:    "app",
		StackName: "CustomerAppStack",
		Account:   "123456789012",
		Region:    "eu-west-1",
		Pusher:    "aws-123456789012",
		Template:  `{"Resources":{}}`,
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	intentContent, ok := files["intents/app.yaml"]
	if !ok {
		t.Fatalf("custom intent file missing: %v", fileKeys(files))
	}
	intent, err := manifest.ParseIntent(intentContent)
	if err != nil {
		t.Fatalf("parse intent: %v", err)
	}
	if intent.Metadata.Name != "app" {
		t.Fatalf("intent name = %q", intent.Metadata.Name)
	}
	if got := intent.Spec.Assets[0].Name; got != "app" {
		t.Fatalf("asset name = %q", got)
	}
	if got := intent.Spec.Assets[0].Payload["aws"]; got != "/partitions/customer-prod-1/payloads/aws/customer-app-stack.yaml" {
		t.Fatalf("payload = %q", got)
	}
}

func fileKeys(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for key := range files {
		out = append(out, key)
	}
	return out
}

func TestSnapshotValidation(t *testing.T) {
	if _, err := Snapshot(SnapshotOptions{}); err == nil {
		t.Fatal("expected partition error")
	}
	if _, err := Snapshot(SnapshotOptions{Partition: "p", StackName: "s", Pusher: "aws-1", Template: "not: [valid"}); err == nil {
		t.Fatal("expected template error")
	}
	if _, err := Snapshot(SnapshotOptions{Partition: "p", StackName: "s", Pusher: "aws-1", Template: ""}); err == nil {
		t.Fatal("expected empty template error")
	}
}
