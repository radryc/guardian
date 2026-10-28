package cdkgen

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
)

// TestCloudFormationFromTemplates compiles the generated payload with tsc and
// synthesizes it with aws-cdk-lib, then asserts on the produced CloudFormation
// template: every payload output must exist, and the expected resource
// families must be present. This is the end-to-end guard that the templates
// compose into valid CloudFormation.
//
// The test needs node plus the aws/ node_modules. Set
// GUARDIAN_AWS_NODE_MODULES to override the default (../../../aws/node_modules
// relative to the guardian package) and run without -short to enable it.
func TestCloudFormationFromTemplates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CloudFormation synthesis test in short mode")
	}
	fixtures := []struct {
		name   string
		input  Input
		checks func(t *testing.T, template map[string]any)
	}{
		{
			name:  "demo intent with implicit default vpc",
			input: demoIntent(),
			checks: func(t *testing.T, template map[string]any) {
				types := resourceTypes(template)
				if types["AWS::EC2::VPC"] != 1 {
					t.Errorf("VPC count = %d, want 1 (DefaultVpc)", types["AWS::EC2::VPC"])
				}
				if types["AWS::ECS::Service"] != 2 {
					t.Errorf("ECS service count = %d, want 2", types["AWS::ECS::Service"])
				}
				if types["AWS::EFS::FileSystem"] != 1 {
					t.Errorf("EFS count = %d, want 1", types["AWS::EFS::FileSystem"])
				}
				if types["AWS::S3::Bucket"] != 1 {
					t.Errorf("S3 bucket count = %d, want 1", types["AWS::S3::Bucket"])
				}
				if types["AWS::ElasticLoadBalancingV2::LoadBalancer"] != 1 {
					t.Errorf("load balancer count = %d, want 1", types["AWS::ElasticLoadBalancingV2::LoadBalancer"])
				}
				assertLoadBalancerType(t, template, "application")
				// Observability injects a second container into the tracegen task.
				assertTaskContainer(t, template, "TaskTracegen", "otel-collector")
				// Stable construct IDs from the historical construct library.
				assertLogicalIDPrefix(t, template, "EfsNginxData")
				assertLogicalIDPrefix(t, template, "ServiceNginx")
				assertLogicalIDPrefix(t, template, "TaskNginx")
				assertLogicalIDPrefix(t, template, "LbNginxEdge")
				assertLogicalIDPrefix(t, template, "TgNginxEdge0")
				assertLogicalIDPrefix(t, template, "DefaultVpc")
				assertLogicalIDPrefix(t, template, "BucketDemoBucket")
				assertLogicalIDPrefix(t, template, "ClusterAwsEcs975049940689")
				// Private ECR images need explicit pull permissions on the
				// task execution role (ContainerImage.fromRegistry cannot
				// infer them).
				assertPolicyAction(t, template, "ecr:GetAuthorizationToken")
				assertPolicyAction(t, template, "ecr:BatchGetImage")
			},
		},
		{
			name: "network stack with shared cluster, postgres and fallback resource",
			input: Input{
				Partition: "aws-kitchen",
				Intent:    "sink",
				Account:   "123456789012",
				Region:    "eu-west-1",
				Order:     []string{"main", "data", "bucket", "db", "api-a", "api-b", "edge", "otel", "queue"},
				Assets: []assetdomain.Spec{
					{Type: "Network", Name: "main", Properties: map[string]any{"cidr": "10.7.0.0/16"}},
					{Type: "Volume", Name: "data", Properties: map[string]any{"size": "1Gi"}},
					{Type: "ObjectStore", Name: "bucket", Properties: map[string]any{"versioning": true}},
					{Type: "SQLDatabase", Name: "db", Properties: map[string]any{"engine": "aurora-postgresql"}},
					{Type: "Compute", Name: "api-a", Properties: map[string]any{
						"image":        "public.ecr.aws/nginx/nginx:latest",
						"cluster":      "shared",
						"ports":        []any{map[string]any{"name": "http", "containerPort": float64(80)}},
						"volumeMounts": []any{map[string]any{"path": "/data", "volume": "data"}},
					}},
					{Type: "Compute", Name: "api-b", Properties: map[string]any{
						"image":   "public.ecr.aws/nginx/nginx:latest",
						"cluster": "shared",
						"ports":   []any{map[string]any{"name": "http", "containerPort": float64(80)}},
					}},
					{Type: "LoadBalancer", Name: "edge", Properties: map[string]any{
						"targets":   []any{"api-a", "api-b"},
						"listeners": []any{map[string]any{"port": float64(80), "protocol": "http"}},
					}},
					{Type: "Observability", Name: "otel", Properties: map[string]any{
						"targets":   []any{"api-a"},
						"exporters": []any{"awsemf", "xray"},
					}},
					{Type: "AWSResource", Name: "queue", Properties: map[string]any{
						"cfnType":    "AWS::SQS::Queue",
						"properties": map[string]any{"QueueName": "guardian-kitchen-queue"},
					}},
				},
			},
			checks: func(t *testing.T, template map[string]any) {
				types := resourceTypes(template)
				if types["AWS::EC2::VPC"] != 1 {
					t.Errorf("VPC count = %d, want 1", types["AWS::EC2::VPC"])
				}
				if types["AWS::ECS::Cluster"] != 1 {
					t.Errorf("ECS cluster count = %d, want 1 (shared)", types["AWS::ECS::Cluster"])
				}
				if types["AWS::ECS::Service"] != 2 {
					t.Errorf("ECS service count = %d, want 2", types["AWS::ECS::Service"])
				}
				if types["AWS::RDS::DBCluster"] != 1 {
					t.Errorf("RDS cluster count = %d, want 1", types["AWS::RDS::DBCluster"])
				}
				if types["AWS::SQS::Queue"] != 1 {
					t.Errorf("SQS queue count = %d, want 1 (fallback template)", types["AWS::SQS::Queue"])
				}
				assertLoadBalancerType(t, template, "application")
				assertRdsEngine(t, template, "aurora-postgresql")
				assertTaskContainer(t, template, "TaskApiA", "otel-collector")
				assertVpcCidr(t, template, "10.7.0.0/16")
				assertLogicalIDPrefix(t, template, "ServiceApiA")
				assertLogicalIDPrefix(t, template, "ServiceApiB")
				assertLogicalIDPrefix(t, template, "ResQueue")
			},
		},
		{
			name: "tcp listener selects nlb and mysql engine",
			input: Input{
				Partition: "aws-net",
				Intent:    "nlb",
				Account:   "123456789012",
				Region:    "eu-west-1",
				Order:     []string{"main", "db", "api", "edge"},
				Assets: []assetdomain.Spec{
					{Type: "Network", Name: "main", Properties: map[string]any{}},
					{Type: "Database", Name: "db", Properties: map[string]any{}},
					{Type: "Compute", Name: "api", Properties: map[string]any{
						"image": "public.ecr.aws/nginx/nginx:latest",
						"ports": []any{map[string]any{"name": "tcp", "containerPort": float64(9092)}},
					}},
					{Type: "LoadBalancer", Name: "edge", Properties: map[string]any{
						"targets":   []any{"api"},
						"listeners": []any{map[string]any{"port": float64(9092), "protocol": "tcp"}},
					}},
				},
			},
			checks: func(t *testing.T, template map[string]any) {
				assertLoadBalancerType(t, template, "network")
				assertRdsEngine(t, template, "aurora-mysql")
				assertLogicalIDPrefix(t, template, "TgEdge0")
			},
		},
		{
			name: "planner order puts volume before network (regression)",
			input: Input{
				Partition: "demo-deploy",
				Intent:    "web",
				Account:   "807359647816",
				Region:    "us-east-1",
				// The planner's topological sort only sees explicit dependsOn
				// edges, so the Volume can precede the Network asset even
				// though the generated refs reference it. The composer must
				// reorder so stack.ts never uses `main` before declaring it.
				Order: []string{"data", "main", "api", "edge"},
				Assets: []assetdomain.Spec{
					{Type: "Network", Name: "main", Properties: map[string]any{"cidr": "10.42.0.0/16"}},
					{Type: "Volume", Name: "data", Properties: map[string]any{"size": "1Gi"}},
					{Type: "Compute", Name: "api", DependsOn: []string{"main", "data"}, Properties: map[string]any{
						"image":    "public.ecr.aws/nginx/nginx:latest",
						"replicas": float64(1),
						"ports":    []any{map[string]any{"name": "http", "containerPort": float64(80)}},
						"volumeMounts": []any{
							map[string]any{"volume": "data", "path": "/data"},
						},
					}},
					{Type: "LoadBalancer", Name: "edge", DependsOn: []string{"api"}, Properties: map[string]any{
						"targets":   []any{"api"},
						"listeners": []any{map[string]any{"name": "http", "port": float64(80), "protocol": "http"}},
					}},
				},
			},
			checks: func(t *testing.T, template map[string]any) {
				types := resourceTypes(template)
				if types["AWS::EC2::VPC"] != 1 {
					t.Errorf("VPC count = %d, want 1", types["AWS::EC2::VPC"])
				}
				if types["AWS::ECS::Service"] != 1 {
					t.Errorf("ECS service count = %d, want 1", types["AWS::ECS::Service"])
				}
				if types["AWS::EFS::FileSystem"] != 1 {
					t.Errorf("EFS count = %d, want 1", types["AWS::EFS::FileSystem"])
				}
			},
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			result, err := Generate(fixture.input)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			template := synthCloudFormation(t, result)
			assertOutputsCoveredByTemplate(t, result.Manifest, template)
			fixture.checks(t, template)
		})
	}
}

// synthCloudFormation writes the generated payload to a temp dir, compiles it
// with tsc (aws/node_modules) and synthesizes the CDK app, returning the
// CloudFormation template.
func synthCloudFormation(t *testing.T, result *Result) map[string]any {
	t.Helper()
	if !result.Generated {
		t.Fatal("expected a generated stack")
	}
	modules := awsNodeModules(t)
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	for rel, content := range result.Files {
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", target, err)
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", target, err)
		}
	}
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatalf("link node_modules: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	tsc := filepath.Join(modules, ".bin", "tsc")
	compile := exec.CommandContext(ctx, tsc)
	compile.Dir = dir
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("tsc failed: %v\n%s", err, output)
	}

	synthScript := `
const fs = require('fs');
const path = require('path');
const cdk = require('aws-cdk-lib');
const { buildStack } = require('./lib/stack.js');
const app = new cdk.App();
buildStack(app);
const asm = app.synth();
const files = fs.readdirSync(asm.directory).filter((f) => f.endsWith('.template.json'));
if (files.length !== 1) { throw new Error('expected one template, got ' + files.length); }
fs.copyFileSync(path.join(asm.directory, files[0]), path.join(process.cwd(), 'template.json'));
`
	synth := exec.CommandContext(ctx, node, "-e", synthScript)
	synth.Dir = dir
	if output, err := synth.CombinedOutput(); err != nil {
		t.Fatalf("cdk synth failed: %v\n%s", err, output)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "template.json"))
	if err != nil {
		t.Fatalf("read synthesized template: %v", err)
	}
	var template map[string]any
	if err := json.Unmarshal(raw, &template); err != nil {
		t.Fatalf("parse synthesized template: %v", err)
	}
	return template
}

func awsNodeModules(t *testing.T) string {
	t.Helper()
	modules := strings.TrimSpace(os.Getenv("GUARDIAN_AWS_NODE_MODULES"))
	if modules == "" {
		modules = filepath.Join("..", "..", "..", "aws", "node_modules")
	}
	absolute, err := filepath.Abs(modules)
	if err != nil {
		t.Fatalf("resolve node_modules: %v", err)
	}
	if _, err := os.Stat(filepath.Join(absolute, ".bin", "tsc")); err != nil {
		t.Skipf("aws node_modules not available at %s (set GUARDIAN_AWS_NODE_MODULES)", absolute)
	}
	return absolute
}

func assertOutputsCoveredByTemplate(t *testing.T, manifest PayloadManifest, template map[string]any) {
	t.Helper()
	outputs, _ := template["Outputs"].(map[string]any)
	for alias, logicalID := range manifest.OutputMap {
		if _, ok := outputs[logicalID]; !ok {
			t.Errorf("output %s (%s) missing from synthesized template", alias, logicalID)
		}
	}
}

func resourceTypes(template map[string]any) map[string]int {
	types := map[string]int{}
	resources, _ := template["Resources"].(map[string]any)
	for _, resource := range resources {
		entry, ok := resource.(map[string]any)
		if !ok {
			continue
		}
		if resourceType, ok := entry["Type"].(string); ok {
			types[resourceType]++
		}
	}
	return types
}

func assertLogicalIDPrefix(t *testing.T, template map[string]any, prefix string) {
	t.Helper()
	resources, _ := template["Resources"].(map[string]any)
	for id := range resources {
		if strings.HasPrefix(id, prefix) {
			return
		}
	}
	t.Errorf("no resource logical ID starts with %q", prefix)
}

func assertLoadBalancerType(t *testing.T, template map[string]any, want string) {
	t.Helper()
	resources, _ := template["Resources"].(map[string]any)
	for _, resource := range resources {
		entry, ok := resource.(map[string]any)
		if !ok || entry["Type"] != "AWS::ElasticLoadBalancingV2::LoadBalancer" {
			continue
		}
		properties, _ := entry["Properties"].(map[string]any)
		if properties["Type"] != want {
			t.Errorf("load balancer type = %v, want %q", properties["Type"], want)
		}
		return
	}
	t.Error("no load balancer resource found")
}

func assertPolicyAction(t *testing.T, template map[string]any, want string) {
	t.Helper()
	resources, _ := template["Resources"].(map[string]any)
	for _, resource := range resources {
		entry, ok := resource.(map[string]any)
		if !ok || entry["Type"] != "AWS::IAM::Policy" {
			continue
		}
		properties, _ := entry["Properties"].(map[string]any)
		document, _ := properties["PolicyDocument"].(map[string]any)
		statements, _ := document["Statement"].([]any)
		for _, statement := range statements {
			stmt, _ := statement.(map[string]any)
			switch actions := stmt["Action"].(type) {
			case string:
				if actions == want {
					return
				}
			case []any:
				for _, action := range actions {
					if action == want {
						return
					}
				}
			}
		}
	}
	t.Errorf("no IAM policy grants %q", want)
}

func assertRdsEngine(t *testing.T, template map[string]any, want string) {
	t.Helper()
	resources, _ := template["Resources"].(map[string]any)
	for _, resource := range resources {
		entry, ok := resource.(map[string]any)
		if !ok || entry["Type"] != "AWS::RDS::DBCluster" {
			continue
		}
		properties, _ := entry["Properties"].(map[string]any)
		if properties["Engine"] != want {
			t.Errorf("rds engine = %v, want %q", properties["Engine"], want)
		}
		return
	}
	t.Error("no RDS cluster resource found")
}

func assertTaskContainer(t *testing.T, template map[string]any, taskPrefix, containerName string) {
	t.Helper()
	resources, _ := template["Resources"].(map[string]any)
	for id, resource := range resources {
		if !strings.HasPrefix(id, taskPrefix) {
			continue
		}
		entry, ok := resource.(map[string]any)
		if !ok || entry["Type"] != "AWS::ECS::TaskDefinition" {
			continue
		}
		properties, _ := entry["Properties"].(map[string]any)
		containers, _ := properties["ContainerDefinitions"].([]any)
		for _, container := range containers {
			definition, _ := container.(map[string]any)
			if definition["Name"] == containerName {
				return
			}
		}
		t.Errorf("task %s missing container %q", id, containerName)
		return
	}
	t.Errorf("no task definition with prefix %q", taskPrefix)
}

func assertVpcCidr(t *testing.T, template map[string]any, want string) {
	t.Helper()
	resources, _ := template["Resources"].(map[string]any)
	for _, resource := range resources {
		entry, ok := resource.(map[string]any)
		if !ok || entry["Type"] != "AWS::EC2::VPC" {
			continue
		}
		properties, _ := entry["Properties"].(map[string]any)
		if properties["CidrBlock"] != want {
			t.Errorf("vpc cidr = %v, want %q", properties["CidrBlock"], want)
		}
		return
	}
	t.Error("no VPC resource found")
}
