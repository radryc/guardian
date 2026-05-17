package validator

import (
	"testing"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	partitiondomain "github.com/rydzu/ainfra/guardian/internal/domain/partition"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
)

func TestValidatePartitionName(t *testing.T) {
	part := &partitiondomain.Partition{
		Metadata: partitiondomain.Metadata{Name: "Bad_Name"},
		Spec: partitiondomain.Spec{
			DeletionPolicy: "orphan",
			Reconciliation: partitiondomain.ReconciliationSpec{Mode: "auto", Interval: "30s"},
		},
	}
	if err := ValidatePartition(part); err == nil {
		t.Fatalf("ValidatePartition() expected error")
	}
}

func TestValidateIntentJoinReference(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "worker"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			Joins:        []string{"core"},
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Assets: []intentdomain.AssetSpec{{
				Type: "Compute",
				Name: "worker",
				Properties: map[string]any{
					"image": "worker:v1",
				},
			}},
		},
	}
	if err := ValidateIntent(intent, []string{"worker"}, []string{"local"}); err == nil {
		t.Fatalf("ValidateIntent() expected unknown join error")
	}
}

func TestValidateIntentDependsOnAndCycles(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "app"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Assets: []intentdomain.AssetSpec{
				{Type: "Compute", Name: "web", DependsOn: []string{"db"}, Properties: map[string]any{"image": "web:v1"}},
				{Type: "Database", Name: "db", DependsOn: []string{"web"}, Properties: map[string]any{"engine": "postgres"}},
			},
		},
	}
	if err := ValidateIntent(intent, []string{"app"}, []string{"local"}); err == nil {
		t.Fatalf("ValidateIntent() expected cycle error")
	}

	intent.Spec.Assets[1].DependsOn = nil
	if err := ValidateIntent(intent, []string{"app"}, []string{"local"}); err != nil {
		t.Fatalf("ValidateIntent() error = %v", err)
	}
}

func TestValidateIntentGenericAssetCatalog(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "monofs"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Assets: []intentdomain.AssetSpec{
				{
					Type: "Config",
					Name: "fetcher-config",
					Properties: map[string]any{
						"format": "json",
						"data": map[string]any{
							"fetcher.json": `{"storage":{"type":"s3"}}`,
						},
					},
				},
				{
					Type: "Volume",
					Name: "node-data",
					Properties: map[string]any{
						"size":       "100Gi",
						"accessMode": "ReadWriteOnce",
					},
				},
				{
					Type: "Volume",
					Name: "object-data",
					Properties: map[string]any{
						"size": "50Gi",
					},
				},
				{
					Type: "Compute",
					Name: "router",
					Properties: map[string]any{
						"image":    "ghcr.io/radryc/monofs-router:v1",
						"replicas": 2,
						"ports": []any{
							map[string]any{"containerPort": 9090},
							map[string]any{"containerPort": 8080},
						},
						"volumeMounts": []any{
							map[string]any{"volume": "node-data", "path": "/data"},
						},
						"configMounts": []any{
							map[string]any{"config": "fetcher-config", "path": "/etc/monofs/fetcher.json", "readOnly": true},
						},
					},
				},
				{
					Type: "LoadBalancer",
					Name: "edge",
					Properties: map[string]any{
						"targets": []any{"router"},
						"listeners": []any{
							map[string]any{"port": 9090, "protocol": "TCP"},
							map[string]any{"port": 8080, "protocol": "TCP"},
						},
						"config": "fetcher-config",
					},
				},
				{
					Type: "ObjectStore",
					Name: "blob-store",
					Properties: map[string]any{
						"engine":     "minio",
						"volume":     "object-data",
						"versioning": true,
						"buckets":    []any{"monofs"},
					},
				},
			},
		},
	}

	if err := ValidateIntent(intent, []string{"monofs"}, []string{"local"}); err != nil {
		t.Fatalf("ValidateIntent() error = %v", err)
	}
}

func TestValidateIntentAllowsKubernetesTargetWithoutNamespace(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "registry"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "k8s-main",
			Target:       targetdomain.Placement{Cluster: "k8s-main"},
			Assets: []intentdomain.AssetSpec{{
				Type: "Compute",
				Name: "registry",
				Properties: map[string]any{
					"image": "registry:2",
				},
			}},
		},
	}

	if err := ValidateIntent(intent, []string{"registry"}, []string{"k8s-main"}); err != nil {
		t.Fatalf("ValidateIntent() error = %v", err)
	}
}

func TestValidateIntentRejectsUnknownAssetType(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "app"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Assets: []intentdomain.AssetSpec{{
				Type: "MagicThing",
				Name: "mystery",
			}},
		},
	}

	if err := ValidateIntent(intent, []string{"app"}, []string{"local"}); err == nil {
		t.Fatalf("ValidateIntent() expected unsupported asset type error")
	}
}

func TestValidateIntentRejectsInvalidGenericAssetRefs(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "app"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Assets: []intentdomain.AssetSpec{
				{
					Type: "ObjectStore",
					Name: "blob-store",
					Properties: map[string]any{
						"engine": "minio",
					},
				},
				{
					Type: "LoadBalancer",
					Name: "edge",
					Properties: map[string]any{
						"targets": []any{"blob-store"},
						"listeners": []any{
							map[string]any{"port": 9090},
						},
					},
				},
			},
		},
	}

	if err := ValidateIntent(intent, []string{"app"}, []string{"local"}); err == nil {
		t.Fatalf("ValidateIntent() expected load balancer target validation error")
	}
}

func TestValidateIntentRejectsConfigWithoutPayload(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "app"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Assets: []intentdomain.AssetSpec{{
				Type:       "Config",
				Name:       "empty-config",
				Properties: map[string]any{"format": "yaml"},
			}},
		},
	}

	if err := ValidateIntent(intent, []string{"app"}, []string{"local"}); err == nil {
		t.Fatalf("ValidateIntent() expected config payload validation error")
	}
}

func TestValidateIntentRejectsRelativePayloadPath(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "app"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Assets: []intentdomain.AssetSpec{{
				Type:    "Compute",
				Name:    "web",
				Payload: map[string]string{"docker": "payloads/web.docker.yaml"},
				Properties: map[string]any{
					"image": "nginx:latest",
				},
			}},
		},
	}

	if err := ValidateIntent(intent, []string{"app"}, []string{"local"}); err == nil {
		t.Fatalf("ValidateIntent() expected relative payload path error")
	}
}

func TestValidateIntentAllowsYamlHintOverrides(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "app"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Hints: []assetdomain.Hint{
				{Path: "outputs.url", Description: "Intent output URL."},
				{Path: "assets.web.ports[0].containerPort", Description: "Primary HTTP port."},
			},
			Assets: []intentdomain.AssetSpec{{
				Type: "Compute",
				Name: "web",
				Hints: []assetdomain.Hint{{
					Path:        "image",
					Description: "Workload image.",
				}},
				Properties: map[string]any{
					"image": "nginx:latest",
				},
			}},
		},
	}

	if err := ValidateIntent(intent, []string{"app"}, []string{"local"}); err != nil {
		t.Fatalf("ValidateIntent() error = %v", err)
	}
}

func TestValidateIntentRejectsUnknownAssetHintSelector(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "app"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Hints: []assetdomain.Hint{{
				Path:        "assets.missing.image",
				Description: "Bad selector.",
			}},
			Assets: []intentdomain.AssetSpec{{
				Type:       "Compute",
				Name:       "web",
				Properties: map[string]any{"image": "nginx:latest"},
			}},
		},
	}

	if err := ValidateIntent(intent, []string{"app"}, []string{"local"}); err == nil {
		t.Fatalf("ValidateIntent() expected unknown asset hint selector error")
	}
}

func TestValidateIntentExtendedAssetTypes(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "platform"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Assets: []intentdomain.AssetSpec{
				{
					Type: "CDKStack",
					Name: "network-stack",
					Payload: map[string]string{
						"aws": "/partitions/platform/payloads/aws/network/stack.yaml",
					},
					Properties: map[string]any{
						"context": map[string]any{
							"envName": "prod",
						},
					},
				},
				{
					Type: "Volume",
					Name: "pg-data",
					Properties: map[string]any{
						"size": "20Gi",
					},
				},
				{
					Type: "Config",
					Name: "otel-config",
					Properties: map[string]any{
						"content": "receivers: {}",
					},
				},
				{
					Type: "ObjectStore",
					Name: "shared-archive",
					Properties: map[string]any{
						"engine":   "minio",
						"endpoint": "http://host.docker.internal:19000",
						"buckets":  []any{"archive"},
					},
				},
				{
					Type: "SQLDatabase",
					Name: "app-db",
					Properties: map[string]any{
						"engine": "postgres",
						"volume": "pg-data",
						"port":   5432,
					},
				},
				{
					Type: "Observability",
					Name: "telemetry",
					Properties: map[string]any{
						"provider":  "otel",
						"config":    "otel-config",
						"receivers": []any{"otlp"},
						"exporters": []any{"logging"},
					},
				},
			},
		},
	}

	if err := ValidateIntent(intent, []string{"platform"}, []string{"local"}); err != nil {
		t.Fatalf("ValidateIntent() error = %v", err)
	}
}

func TestValidateIntentRejectsCDKStackWithoutAWSPayload(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "platform"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "aws",
			Target: targetdomain.Placement{
				Account: "123456789012",
				Region:  "eu-west-1",
			},
			Assets: []intentdomain.AssetSpec{{
				Type: "CDKStack",
				Name: "network",
			}},
		},
	}

	if err := ValidateIntent(intent, []string{"platform"}, []string{"aws"}); err == nil {
		t.Fatalf("ValidateIntent() expected CDKStack payload validation error")
	}
}

func TestValidateIntentRejectsPartialExternalObjectStoreCredentials(t *testing.T) {
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "platform"},
		Spec: intentdomain.IntentSpec{
			IntentType:   "standard",
			TargetPusher: "local",
			Target:       targetdomain.Placement{Cluster: "local"},
			Assets: []intentdomain.AssetSpec{{
				Type: "ObjectStore",
				Name: "shared-archive",
				Properties: map[string]any{
					"engine":      "minio",
					"endpoint":    "http://host.docker.internal:19000",
					"accessKeyID": "minio",
				},
			}},
		},
	}

	if err := ValidateIntent(intent, []string{"platform"}, []string{"local"}); err == nil {
		t.Fatalf("ValidateIntent() expected external object store credential validation error")
	}
}
