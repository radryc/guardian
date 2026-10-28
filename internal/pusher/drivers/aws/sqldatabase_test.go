package awsdriver

import (
	"context"
	"strings"
	"testing"

	targetdomain "github.com/radryc/guardian/internal/domain/target"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/driverutil"
	"github.com/radryc/guardian/internal/pusher/registry"
)

func TestAuroraSQLDatabaseApplyDiffDestroy(t *testing.T) {
	ctx := context.Background()
	backend := NewBackend()
	driver := &SQLDatabaseDriver{baseDriver: baseDriver{backend: backend}, typeName: "SQLDatabase"}
	in := registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "data",
		Asset: taskdomain.AbstractAsset{
			Type: "SQLDatabase",
			Name: "orders",
			Properties: map[string]any{
				"engine":   "postgres",
				"version":  "16.4",
				"database": "orders",
				"user":     "app",
			},
		},
		Target: targetdomain.Placement{Account: "123456789012", Region: "eu-west-1"},
	}

	report, err := driver.Diff(ctx, in)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if report.Status != "Changed" {
		t.Fatalf("Diff() = %+v, want changed before apply", report)
	}

	result, err := driver.Apply(ctx, in)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Outputs["engine"] != "aurora-postgresql" {
		t.Fatalf("outputs engine = %q, want aurora-postgresql (%v)", result.Outputs["engine"], result.Outputs)
	}
	if result.Outputs["database"] != "orders" || result.Outputs["user"] != "app" {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	if !strings.HasPrefix(result.Outputs["url"], "postgresql://app@") {
		t.Fatalf("outputs url = %q", result.Outputs["url"])
	}

	cluster, ok, err := backend.GetDBCluster(ctx, awsDBClusterName(in))
	if err != nil || !ok {
		t.Fatalf("aurora cluster missing: ok=%v err=%v", ok, err)
	}
	if cluster.Engine != "aurora-postgresql" || cluster.Port != 5432 {
		t.Fatalf("cluster = %+v", cluster)
	}
	if cluster.Hash != driverutil.CompositeHash(in) {
		t.Fatalf("cluster hash = %q, want composite hash", cluster.Hash)
	}

	report, err = driver.Diff(ctx, in)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("Diff() = %+v, want InSync", report)
	}

	if err := driver.Destroy(ctx, in); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if _, ok, _ := backend.GetDBCluster(ctx, awsDBClusterName(in)); ok {
		t.Fatalf("aurora cluster still present after destroy")
	}
}

func TestAuroraEngineMapping(t *testing.T) {
	cases := map[string]string{
		"mysql":             "aurora-mysql",
		"aurora":            "aurora-mysql",
		"aurora-mysql":      "aurora-mysql",
		"postgres":          "aurora-postgresql",
		"postgresql":        "aurora-postgresql",
		"aurora-postgresql": "aurora-postgresql",
	}
	for input, want := range cases {
		got, err := auroraEngine(input)
		if err != nil || got != want {
			t.Fatalf("auroraEngine(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := auroraEngine("sqlite"); err == nil {
		t.Fatalf("auroraEngine(sqlite) expected error")
	}
}
