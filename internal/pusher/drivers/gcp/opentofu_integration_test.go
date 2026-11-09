//go:build integration

package gcpdriver

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/radryc/guardian/internal/pusher/drivers/gcp/opentofu"
)

// TestOpenTofuBackendAgainstGCP runs real plans against the sandbox project.
// It never applies, so it is safe to run repeatedly.
//
//	GCP_PROJECT=strata-gcp-dev GCP_REGION=europe-west1 \
//	TF_STATE_BUCKET=strata-gcp-dev-tofu-state \
//	GCP_KMS_KEY=projects/.../cryptoKeys/state \
//	go test -tags integration -run TestOpenTofuBackendAgainstGCP ./internal/pusher/drivers/gcp/
func TestOpenTofuBackendAgainstGCP(t *testing.T) {
	project := os.Getenv("GCP_PROJECT")
	bucket := os.Getenv("TF_STATE_BUCKET")
	if project == "" || bucket == "" {
		t.Skip("set GCP_PROJECT and TF_STATE_BUCKET to run the integration test")
	}
	region := os.Getenv("GCP_REGION")
	if region == "" {
		region = "europe-west1"
	}

	backend, err := NewOpenTofuBackend(opentofu.Config{
		WorkRoot:        t.TempDir(),
		StateBucket:     bucket,
		StatePrefixRoot: "integration",
		StateKMSKey:     os.Getenv("GCP_KMS_KEY"),
		Env: map[string]string{
			"GOOGLE_CLOUD_PROJECT": project,
		},
	})
	if err != nil {
		t.Fatalf("NewOpenTofuBackend: %v", err)
	}

	cases := []struct {
		name      string
		blueprint string
		vars      map[string]any
	}{
		{
			name:      "objectstore",
			blueprint: "objectstore",
			vars: map[string]any{
				"bucket":        "guardian-integration-plan-only",
				"versioning":    true,
				"force_destroy": true,
			},
		},
		{
			name:      "network",
			blueprint: "network",
			vars: map[string]any{
				"name":     "guardian-integration-plan-only",
				"subnet":   "guardian-integration-plan-only-subnet",
				"cidr":     "10.99.0.0/16",
				"internal": true,
			},
		},
		{
			name:      "secret",
			blueprint: "secret",
			vars: map[string]any{
				"secret_id":      "guardian-integration-plan-only",
				"value":          "plan-only",
				"create_version": true,
			},
		},
		{
			name:      "compute",
			blueprint: "compute",
			vars: map[string]any{
				"name":     "guardian-integration-plan-only",
				"image":    "us-docker.pkg.dev/cloudrun/container/hello",
				"replicas": 1,
				"env":      map[string]string{"LOG_LEVEL": "info"},
				"args":     []string{},
				"command":  []string{},
				"port":     8080,
				"cpu":      "1",
				"memory":   "512Mi",
				"network":  "",
				"subnet":   "",
			},
		},
		{
			name:      "sqldatabase",
			blueprint: "sqldatabase",
			vars: map[string]any{
				"name":             "guardian-integration-plan-only",
				"engine":           "POSTGRES",
				"database_version": "POSTGRES_16",
				"tier":             "db-f1-micro",
				"database":         "app",
				"user":             "app",
				"port":             5432,
				"scheme":           "postgres",
				"network":          "",
			},
		},
		{
			name:      "volume",
			blueprint: "volume",
			vars: map[string]any{
				"name":        "guardian-integration-plan-only",
				"capacity_gb": 100,
				"tier":        "BASIC_HDD",
				"location":    region + "-a",
				"share":       "data",
				"network":     "default",
			},
		},
		{
			name:      "loadbalancer",
			blueprint: "loadbalancer",
			vars: map[string]any{
				"name":    "guardian-integration-plan-only",
				"targets": []string{"guardian-integration-plan-only-app"},
				"port":    80,
			},
		},
		{
			name:      "observability",
			blueprint: "observability",
			vars: map[string]any{
				"name":    "guardian-integration-plan-only",
				"image":   "otel/opentelemetry-collector-contrib:0.115.0",
				"config":  "receivers:\n  otlp:\n    protocols:\n      grpc:\nprocessors:\n  batch: {}\nexporters:\n  googlecloud: {}\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      processors: [batch]\n      exporters: [googlecloud]\n",
				"env":     map[string]string{},
				"port":    4317,
				"network": "",
				"subnet":  "",
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			req := StackRequest{
				Partition:   "smoke",
				Intent:      testCase.name,
				Project:     project,
				Region:      region,
				Blueprint:   testCase.blueprint,
				DesiredHash: "integration-plan-only",
				Variables:   testCase.vars,
			}
			if err := backend.CheckEnvironment(ctx, req); err != nil {
				t.Fatalf("CheckEnvironment: %v", err)
			}
			outcome, err := backend.Plan(ctx, req)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			t.Logf("plan: drift=%t summary=%q", outcome.Drift, outcome.Summary)
			if outcome.Summary == "" {
				t.Fatal("expected a plan summary")
			}
		})
	}
}
