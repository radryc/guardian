package cdkgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

// dumpGenerated writes the generated file tree when CDKGEN_DUMP_DIR is set so
// the app can be compiled and synthesized outside the Go tests.
func dumpGenerated(t *testing.T, result *Result) {
	t.Helper()
	dir := os.Getenv("CDKGEN_DUMP_DIR")
	if dir == "" {
		return
	}
	for rel, content := range result.Files {
		target := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", target, err)
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", target, err)
		}
	}
	t.Logf("dumped generated stack to %s", dir)
}

func demoIntent() Input {
	return Input{
		Partition: "aws-demo",
		Intent:    "demo-stack",
		Account:   "975049940689",
		Region:    "us-east-1",
		SourceDir: "/partitions/aws-demo/payloads/aws-generated/demo-stack",
		Order:     []string{"demo-api-key", "nginx-page", "nginx-data", "demo-bucket", "nginx", "nginx-edge", "tracegen", "tracegen-otel"},
		Assets: []assetdomain.Spec{
			{Type: "Secret", Name: "demo-api-key", Properties: map[string]any{"value": "placeholder"}},
			{Type: "Config", Name: "nginx-page", Properties: map[string]any{"format": "text", "data": map[string]any{"index.html": "<h1>hi</h1>"}}},
			{Type: "Volume", Name: "nginx-data", Properties: map[string]any{"accessMode": "ReadWriteMany", "size": "1Gi"}},
			{Type: "ObjectStore", Name: "demo-bucket", Properties: map[string]any{"engine": "s3", "region": "us-east-1", "versioning": true}},
			{Type: "Compute", Name: "nginx", DependsOn: []string{"nginx-data", "nginx-page", "demo-api-key"}, Properties: map[string]any{
				"image":        "975049940689.dkr.ecr.us-east-1.amazonaws.com/demo/nginx:latest",
				"ports":        []any{map[string]any{"name": "http", "containerPort": float64(80), "protocol": "TCP", "servicePort": float64(80)}},
				"configMounts": []any{map[string]any{"config": "nginx-page", "path": "/usr/share/nginx/html", "readOnly": true}},
				"volumeMounts": []any{map[string]any{"path": "/var/cache/nginx", "volume": "nginx-data"}},
				"resources":    map[string]any{"limits": map[string]any{"cpu": "256", "memory": "512"}, "requests": map[string]any{"cpu": "256", "memory": "512"}},
			}},
			{Type: "LoadBalancer", Name: "nginx-edge", DependsOn: []string{"nginx"}, Properties: map[string]any{
				"targets":   []any{"nginx"},
				"listeners": []any{map[string]any{"externalPort": float64(80), "name": "http", "port": float64(80), "protocol": "http"}},
			}},
			{Type: "Compute", Name: "tracegen", Properties: map[string]any{
				"image":   "ghcr.io/open-telemetry/opentelemetry-collector-contrib/telemetrygen:v0.149.0",
				"command": []any{"traces", "--otlp-insecure", "--service", "aws-demo-tracegen"},
			}},
			{Type: "Observability", Name: "tracegen-otel", DependsOn: []string{"tracegen"}, Properties: map[string]any{
				"exporters": []any{"awsemf", "xray"},
				"provider":  "otel",
				"targets":   []any{"tracegen"},
			}},
		},
	}
}

func TestGenerateDemoStack(t *testing.T) {
	result, err := Generate(demoIntent())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !result.Generated {
		t.Fatal("expected generated stack")
	}
	if got, want := strings.Join(result.NativeAssets, ","), "demo-api-key,nginx-page"; got != want {
		t.Fatalf("native assets = %q, want %q", got, want)
	}
	if got, want := strings.Join(result.GeneratedAssets, ","), "nginx-data,demo-bucket,nginx,nginx-edge,tracegen,tracegen-otel"; got != want {
		t.Fatalf("generated assets = %q, want %q", got, want)
	}
	if got, want := result.Manifest.StackName, "guardian-aws-demo-demo-stack"; got != want {
		t.Fatalf("stack name = %q, want %q", got, want)
	}
	for _, key := range []string{
		"nginx.service", "nginx.cluster", "nginx-edge.dns_name", "nginx-edge.tg_0_arn",
		"nginx-data.efsId", "demo-bucket.bucket", "tracegen-otel.namespace",
	} {
		if _, ok := result.Manifest.OutputMap[key]; !ok {
			t.Errorf("output map missing %q", key)
		}
	}
	for _, file := range []string{
		"package.json", "tsconfig.json", "cdk.json", "bin/app.ts", "lib/stack.ts",
		PayloadManifestFileName,
		"templates/_shared/helpers.ts",
		"templates/volume.efs/construct.ts",
		"templates/objectstore.s3/construct.ts",
		"templates/compute.fargate/construct.ts",
		"templates/loadbalancer.alb/construct.ts",
		"templates/observability.otel-sidecar/construct.ts",
		"templates/network.default/construct.ts",
	} {
		if _, ok := result.Files[file]; !ok {
			t.Errorf("generated files missing %q", file)
		}
	}
	stack := string(result.Files["lib/stack.ts"])
	for _, call := range []string{
		"const nginxData = volumeEfs(stack, 'nginx-data'",
		"const demoBucket = objectStoreS3(stack, 'demo-bucket'",
		"const nginx = computeFargate(stack, 'nginx'",
		"const nginxEdge = loadBalancerAlb(stack, 'nginx-edge'",
		"const tracegen = computeFargate(stack, 'tracegen'",
		"const tracegenOtel = observabilityOtelSidecar(stack, 'tracegen-otel'",
		"const defaultVpc = networkDefault(stack);",
		"import { volumeEfs } from '../templates/volume.efs/construct';",
	} {
		if !strings.Contains(stack, call) {
			t.Errorf("stack.ts missing %q", call)
		}
	}
	if strings.Contains(stack, "addSecret") || strings.Contains(stack, "addConfig") {
		t.Error("native asset types must not be generated into the stack")
	}
	var manifest PayloadManifest
	if err := json.Unmarshal(result.Files[PayloadManifestFileName], &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.SourceDir != "/partitions/aws-demo/payloads/aws-generated/demo-stack" {
		t.Fatalf("manifest source dir = %q", manifest.SourceDir)
	}
	if manifest.LibraryVersion != LibraryVersion {
		t.Fatalf("manifest library version = %q", manifest.LibraryVersion)
	}
	if manifest.CatalogVersion == "" {
		t.Fatal("manifest catalog version is empty")
	}
	if len(manifest.Templates) == 0 {
		t.Fatal("manifest templates are empty")
	}
	byID := map[string]TemplateRef{}
	for _, ref := range manifest.Templates {
		byID[ref.ID] = ref
		if ref.Digest == "" {
			t.Errorf("template %s missing digest", ref.ID)
		}
		if ref.Source != TemplateSourceEmbedded {
			t.Errorf("template %s source = %q, want embedded", ref.ID, ref.Source)
		}
	}
	for _, id := range []string{
		"aws.volume.efs", "aws.objectstore.s3", "aws.compute.fargate",
		"aws.loadbalancer.alb", "aws.observability.otel-sidecar", "aws.network.default",
	} {
		if _, ok := byID[id]; !ok {
			t.Errorf("manifest missing template %s", id)
		}
	}
	generatedNames := string(result.Files[PayloadManifestFileName])
	if !strings.Contains(generatedNames, "\"generatedAssets\"") {
		t.Fatalf("manifest missing generatedAssets: %s", generatedNames)
	}
	component := string(result.Files["lib/stack.ts"])
	if !strings.Contains(component, "\"serviceName\": \"aws-svc-975049940689-us-east-1-aws-demo-demo-stack-nginx\"") {
		t.Errorf("compute generated names missing from stack.ts:\n%s", component)
	}
	if !strings.Contains(component, "\"/guardian/aws/ssm/975049940689/us/east/1/aws/demo/demo/stack/nginx/page\"") {
		t.Errorf("config SSM parameter name missing from stack.ts:\n%s", component)
	}
	dumpGenerated(t, result)
}

func TestGenerateComputeClusterNameIsScopedToIntent(t *testing.T) {
	input := func(partition, intent string, computes ...string) Input {
		assets := make([]assetdomain.Spec, 0, len(computes))
		for _, name := range computes {
			assets = append(assets, assetdomain.Spec{
				Type:       "Compute",
				Name:       name,
				Properties: map[string]any{"image": "public.ecr.aws/nginx/nginx:latest"},
			})
		}
		return Input{
			Partition: partition,
			Intent:    intent,
			Account:   "780603797542",
			Region:    "us-east-1",
			Order:     computes,
			Assets:    assets,
		}
	}

	first, err := Generate(input("demo-deploy", "web", "api"))
	if err != nil {
		t.Fatalf("Generate(demo-deploy): %v", err)
	}
	second, err := Generate(input("demo-build", "web", "app"))
	if err != nil {
		t.Fatalf("Generate(demo-build): %v", err)
	}
	firstStack := string(first.Files["lib/stack.ts"])
	secondStack := string(second.Files["lib/stack.ts"])

	// ECS cluster names are account-wide, so two stacks in one account must
	// not both use the bare account id (CloudFormation reports
	// "<account> already exists in stack ...").
	if strings.Contains(firstStack, `"cluster": "780603797542"`) {
		t.Fatalf("cluster must not be the bare account id:\n%s", firstStack)
	}
	if !strings.Contains(firstStack, "aws-ecs-780603797542-us-east-1-demo-deploy-web") {
		t.Fatalf("demo-deploy cluster name missing:\n%s", firstStack)
	}
	if !strings.Contains(secondStack, "aws-ecs-780603797542-us-east-1-demo-build-web") {
		t.Fatalf("demo-build cluster name missing:\n%s", secondStack)
	}

	// Compute assets in one intent share the cluster handle.
	shared, err := Generate(input("demo-deploy", "web", "api", "worker"))
	if err != nil {
		t.Fatalf("Generate(shared cluster): %v", err)
	}
	sharedStack := string(shared.Files["lib/stack.ts"])
	if got := strings.Count(sharedStack, `"cluster": "aws-ecs-780603797542-us-east-1-demo-deploy-web"`); got != 2 {
		t.Fatalf("cluster name occurrences = %d, want 2 (one per compute):\n%s", got, sharedStack)
	}
}

func TestGenerateLoadBalancerNamesStayDistinct(t *testing.T) {
	// ALB/TG names are capped at 32 chars; they must stay unique per
	// partition/intent (regression: "target group ... already exists").
	input := func(partition string) Input {
		return Input{
			Partition: partition,
			Intent:    "web",
			Account:   "780603797542",
			Region:    "us-east-1",
			Order:     []string{"edge"},
			Assets: []assetdomain.Spec{
				{Type: "LoadBalancer", Name: "edge", Properties: map[string]any{
					"targets":   []any{},
					"listeners": []any{map[string]any{"name": "http", "port": float64(80), "protocol": "http"}},
				}},
			},
		}
	}
	first, err := Generate(input("demo-deploy"))
	if err != nil {
		t.Fatalf("Generate(demo-deploy): %v", err)
	}
	second, err := Generate(input("demo-build"))
	if err != nil {
		t.Fatalf("Generate(demo-build): %v", err)
	}
	firstStack := string(first.Files["lib/stack.ts"])
	secondStack := string(second.Files["lib/stack.ts"])
	if !strings.Contains(firstStack, "aws-lb-demo-deploy-web-edge") {
		t.Fatalf("unexpected demo-deploy lb name:\n%s", firstStack)
	}
	if !strings.Contains(secondStack, "aws-lb-demo-build-web-edge") {
		t.Fatalf("unexpected demo-build lb name:\n%s", secondStack)
	}
}

func TestGenerateHoistsNetworkBeforeDependents(t *testing.T) {
	// The planner's topological sort only sees explicit dependsOn edges, so
	// the Volume can precede the Network asset. The composer must declare the
	// Network first because refs reference it (regression: TS2448).
	input := Input{
		Partition: "demo-deploy",
		Intent:    "web",
		Account:   "807359647816",
		Region:    "us-east-1",
		Order:     []string{"data", "main", "api"},
		Assets: []assetdomain.Spec{
			{Type: "Network", Name: "main", Properties: map[string]any{"cidr": "10.42.0.0/16"}},
			{Type: "Volume", Name: "data", Properties: map[string]any{"size": "1Gi"}},
			{Type: "Compute", Name: "api", DependsOn: []string{"main", "data"}, Properties: map[string]any{
				"image":        "public.ecr.aws/nginx/nginx:latest",
				"volumeMounts": []any{map[string]any{"volume": "data", "path": "/data"}},
			}},
		},
	}
	result, err := Generate(input)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	stack := string(result.Files["lib/stack.ts"])
	mainDecl := strings.Index(stack, "const main =")
	if mainDecl < 0 {
		t.Fatalf("stack.ts does not declare the network:\n%s", stack)
	}
	for _, name := range []string{"data", "api"} {
		if decl := strings.Index(stack, "const "+name+" ="); decl >= 0 && decl < mainDecl {
			t.Fatalf("stack.ts declares %s before the network it references:\n%s", name, stack)
		}
	}
}

func TestGenerateRefsWiring(t *testing.T) {
	result, err := Generate(Input{
		Partition: "p",
		Intent:    "i",
		Account:   "123456789012",
		Region:    "eu-west-1",
		Order:     []string{"main", "data", "api-a", "api-b", "edge"},
		Assets: []assetdomain.Spec{
			{Type: "Network", Name: "main", Properties: map[string]any{"cidr": "10.1.0.0/16"}},
			{Type: "Volume", Name: "data", Properties: map[string]any{"size": "1Gi"}},
			{Type: "Compute", Name: "api-a", Properties: map[string]any{
				"image":        "nginx",
				"cluster":      "shared",
				"ports":        []any{map[string]any{"name": "http", "containerPort": float64(80)}},
				"volumeMounts": []any{map[string]any{"path": "/data", "volume": "data"}},
			}},
			{Type: "Compute", Name: "api-b", Properties: map[string]any{
				"image":   "nginx",
				"cluster": "shared",
				"ports":   []any{map[string]any{"name": "http", "containerPort": float64(80)}},
			}},
			{Type: "LoadBalancer", Name: "edge", Properties: map[string]any{
				"targets":   []any{"api-a", "api-b"},
				"listeners": []any{map[string]any{"port": float64(80), "protocol": "http"}},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	stack := string(result.Files["lib/stack.ts"])
	for _, want := range []string{
		"const main = networkVpc(stack, 'main'",
		"const data = volumeEfs(stack, 'data'",
		"refs: { network: main }",
		"refs: { network: main, volumes: { 'data': data } }",
		"refs: { network: main, cluster: apiA.cluster }",
		"refs: { network: main, targets: [apiA, apiB] }",
	} {
		if !strings.Contains(stack, want) {
			t.Errorf("stack.ts missing %q:\n%s", want, stack)
		}
	}
	if strings.Contains(stack, "networkDefault") {
		t.Error("default network must not be emitted when a Network asset exists")
	}
	dumpGenerated(t, result)
}

func TestGenerateNlbAndDatabaseVariants(t *testing.T) {
	result, err := Generate(Input{
		Partition: "p",
		Intent:    "i",
		Account:   "123456789012",
		Region:    "eu-west-1",
		Order:     []string{"main", "db", "api", "edge"},
		Assets: []assetdomain.Spec{
			{Type: "Network", Name: "main", Properties: map[string]any{}},
			{Type: "SQLDatabase", Name: "db", Properties: map[string]any{"engine": "aurora-postgresql"}},
			{Type: "Compute", Name: "api", Properties: map[string]any{
				"image": "nginx",
				"ports": []any{map[string]any{"containerPort": float64(8080)}},
			}},
			{Type: "LoadBalancer", Name: "edge", Properties: map[string]any{
				"targets":   []any{"api"},
				"listeners": []any{map[string]any{"port": float64(8080), "protocol": "tcp"}},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	stack := string(result.Files["lib/stack.ts"])
	for _, want := range []string{
		"const db = databaseAuroraPostgres(stack, 'db'",
		"const edge = loadBalancerNlb(stack, 'edge'",
		"refs: { network: main }",
	} {
		if !strings.Contains(stack, want) {
			t.Errorf("stack.ts missing %q:\n%s", want, stack)
		}
	}
	byID := map[string]TemplateRef{}
	for _, ref := range result.Manifest.Templates {
		byID[ref.ID] = ref
	}
	if _, ok := byID["aws.loadbalancer.nlb"]; !ok {
		t.Error("manifest missing nlb template")
	}
	if _, ok := byID["aws.database.aurora-postgres"]; !ok {
		t.Error("manifest missing postgres template")
	}
	if _, ok := result.Manifest.OutputMap["db.secretArn"]; !ok {
		t.Error("output map missing db.secretArn")
	}
	dumpGenerated(t, result)
}

func TestGenerateRejectsMultipleNetworks(t *testing.T) {
	_, err := Generate(Input{
		Partition: "p",
		Intent:    "i",
		Assets: []assetdomain.Spec{
			{Type: "Network", Name: "a", Properties: map[string]any{}},
			{Type: "Network", Name: "b", Properties: map[string]any{}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "multiple Network") {
		t.Fatalf("expected multiple network error, got %v", err)
	}
}

func TestGenerateRejectsMissingRefs(t *testing.T) {
	_, err := Generate(Input{
		Partition: "p",
		Intent:    "i",
		Assets: []assetdomain.Spec{
			{Type: "Compute", Name: "api", Properties: map[string]any{
				"image":        "nginx",
				"volumeMounts": []any{map[string]any{"path": "/data", "volume": "missing"}},
			}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "volume missing must be generated before it") {
		t.Fatalf("expected missing volume error, got %v", err)
	}

	_, err = Generate(Input{
		Partition: "p",
		Intent:    "i",
		Assets: []assetdomain.Spec{
			{Type: "LoadBalancer", Name: "edge", Properties: map[string]any{"targets": []any{"api"}}},
			{Type: "Compute", Name: "api", Properties: map[string]any{"image": "nginx"}},
		},
		Order: []string{"edge", "api"},
	})
	if err == nil || !strings.Contains(err.Error(), "target compute api must be generated before it") {
		t.Fatalf("expected ordering error, got %v", err)
	}
}

func TestGenerateSkipsIntentsWithoutSupportedAssets(t *testing.T) {
	result, err := Generate(Input{
		Partition: "p",
		Intent:    "i",
		Assets: []assetdomain.Spec{
			{Type: "Secret", Name: "s", Properties: map[string]any{"value": "x"}},
			{Type: "Config", Name: "c", Properties: map[string]any{"content": "y"}},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Generated {
		t.Fatal("expected no generated stack")
	}
	if len(result.Files) != 0 {
		t.Fatalf("expected no files, got %d", len(result.Files))
	}
}

func TestGenerateRejectsUnresolvedRefs(t *testing.T) {
	_, err := Generate(Input{
		Partition: "p",
		Intent:    "i",
		Assets: []assetdomain.Spec{
			{Type: "Compute", Name: "c", Properties: map[string]any{"image": "${intent.other.outputs.c.id}"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("expected unresolved reference error, got %v", err)
	}
}

func TestGenerateRejectsUnsupportedProperties(t *testing.T) {
	_, err := Generate(Input{
		Partition: "p",
		Intent:    "i",
		Assets: []assetdomain.Spec{
			{Type: "ObjectStore", Name: "b", Properties: map[string]any{"existingBucket": "my-bucket"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "existingBucket is not supported") {
		t.Fatalf("expected unsupported property error, got %v", err)
	}

	_, err = Generate(Input{
		Partition: "p",
		Intent:    "i",
		Assets: []assetdomain.Spec{
			{Type: "Compute", Name: "c", Properties: map[string]any{}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "property image is required") {
		t.Fatalf("expected required property error, got %v", err)
	}
}

func TestGenerateAWSResourceFallback(t *testing.T) {
	result, err := Generate(Input{
		Partition: "p",
		Intent:    "i",
		Account:   "123456789012",
		Region:    "eu-west-1",
		Assets: []assetdomain.Spec{
			{Type: "AWSResource", Name: "queue", Properties: map[string]any{
				"cfnType":    "AWS::SQS::Queue",
				"properties": map[string]any{"QueueName": "guardian-queue"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	stack := string(result.Files["lib/stack.ts"])
	if !strings.Contains(stack, "const queue = cfnResource(stack, 'queue'") {
		t.Errorf("stack.ts missing fallback call:\n%s", stack)
	}
	if _, ok := result.Files["templates/fallback.cfn-resource/construct.ts"]; !ok {
		t.Error("fallback template file missing from payload")
	}
	var ref TemplateRef
	for _, candidate := range result.Manifest.Templates {
		if candidate.ID == "aws.fallback.cfn-resource" {
			ref = candidate
		}
	}
	if ref.ID == "" || !strings.Contains(ref.ID, "fallback") {
		t.Errorf("manifest missing fallback template ref: %+v", result.Manifest.Templates)
	}
	dumpGenerated(t, result)
}

func TestOutputLogicalID(t *testing.T) {
	cases := map[string]string{
		OutputLogicalID("nginx-edge", "dns_name"):    "OutNginxEdgeDnsName",
		OutputLogicalID("nginx", "service"):          "OutNginxService",
		OutputLogicalID("tracegen-otel", "tg_0_arn"): "OutTracegenOtelTg0Arn",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestResourceNameMatchesDriverNaming(t *testing.T) {
	if got, want := ssmParameterName("975049940689", "us-east-1", "aws-demo", "demo-stack", "nginx-page"), "/guardian/aws/ssm/975049940689/us/east/1/aws/demo/demo/stack/nginx/page"; got != want {
		t.Fatalf("ssm name = %q, want %q", got, want)
	}
	if got, want := s3BucketName("975049940689", "us-east-1", "aws-demo", "demo-stack", "demo-bucket"), "guardian-975049940689-us-east-1-aws-demo-demo-stack-demo-bucket"; got != want {
		t.Fatalf("bucket name = %q, want %q", got, want)
	}
}

func TestTsVarNaming(t *testing.T) {
	cases := map[string]string{
		"nginx-edge":    "nginxEdge",
		"demo-bucket":   "demoBucket",
		"tracegen":      "tracegen",
		"tracegen-otel": "tracegenOtel",
		"2fa":           "a2fa",
	}
	for input, want := range cases {
		if got := tsVar(input); got != want {
			t.Errorf("tsVar(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestGenerateRepoDemoIntent generates from the checked-in aws-demo partition
// manifest so the demo conversion is exercised on every guardian test run.
func TestGenerateRepoDemoIntent(t *testing.T) {
	const manifestPath = "../../../stratatools/partitions/aws-demo/intents/demo-stack.yaml"
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Skipf("demo partition manifest not available: %v", err)
	}
	parsed, err := manifest.ParseIntent(content)
	if err != nil {
		t.Fatalf("parse demo intent: %v", err)
	}
	if parsed.Spec.Implementation != "cdk" {
		t.Fatalf("demo intent implementation = %q, want cdk", parsed.Spec.Implementation)
	}
	order := make([]string, 0, len(parsed.Spec.Assets))
	for _, asset := range parsed.Spec.Assets {
		order = append(order, asset.Name)
	}
	result, err := Generate(Input{
		Partition: "aws-demo",
		Intent:    "demo-stack",
		Account:   parsed.Spec.Target.Account,
		Region:    parsed.Spec.Target.Region,
		SourceDir: "/partitions/aws-demo/payloads/aws-generated/demo-stack",
		Assets:    parsed.Spec.Assets,
		Order:     order,
	})
	if err != nil {
		t.Fatalf("generate demo stack: %v", err)
	}
	if got, want := strings.Join(result.NativeAssets, ","), "demo-api-key,nginx-page"; got != want {
		t.Fatalf("native assets = %q, want %q", got, want)
	}
	for _, call := range []string{
		"volumeEfs(stack, 'nginx-data'",
		"objectStoreS3(stack, 'demo-bucket'",
		"computeFargate(stack, 'nginx'",
		"loadBalancerAlb(stack, 'nginx-edge'",
		"computeFargate(stack, 'tracegen'",
		"observabilityOtelSidecar(stack, 'tracegen-otel'",
	} {
		if !strings.Contains(string(result.Files["lib/stack.ts"]), call) {
			t.Errorf("generated demo stack missing %q", call)
		}
	}
}
