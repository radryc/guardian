package awsgen

import (
	"context"
	"strings"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/awsscan"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func scanResult() *awsscan.ScanResult {
	return &awsscan.ScanResult{
		APIVersion: awsscan.APIVersion,
		Kind:       awsscan.ResultKind,
		ScanID:     "scan-1",
		Pusher:     "aws-123456789012",
		Account:    "123456789012",
		Status:     awsscan.ScanStatusSucceeded,
		Regions:    []string{"eu-west-1"},
	}
}

func TestGenerateStackGrouping(t *testing.T) {
	result := scanResult()
	result.Buckets = []awsscan.BucketResource{{
		Name: "data-lake", Region: "eu-west-1", Versioned: true,
		Tags:  map[string]string{"aws:cloudformation:stack-name": "analytics"},
		Stack: "analytics",
	}}
	result.Services = []awsscan.ServiceResource{{
		Region: "eu-west-1", ClusterName: "tools", Name: "etl", DesiredCount: 2,
		Image: "public.ecr.aws/etl/worker:1.4", CPU: 512, Memory: 1024,
		Stack: "analytics",
		Ports: []awsscan.ServicePort{{ContainerPort: 8080, HostPort: 8080, Protocol: "tcp"}},
	}}
	draft, err := Generate(result, Options{PartitionName: "imported"}, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(draft.Intents) != 1 {
		t.Fatalf("expected 1 intent, got %d", len(draft.Intents))
	}
	intent := draft.Intents[0]
	if intent.Metadata.Name != "import-analytics" {
		t.Fatalf("expected intent name import-analytics, got %s", intent.Metadata.Name)
	}
	if intent.Spec.TargetPusher != "aws-123456789012" {
		t.Fatalf("expected targetPusher from scan, got %s", intent.Spec.TargetPusher)
	}
	if intent.Spec.Target.Account != "123456789012" || intent.Spec.Target.Region != "eu-west-1" {
		t.Fatalf("unexpected target placement %+v", intent.Spec.Target)
	}
	if len(intent.Spec.Assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(intent.Spec.Assets))
	}

	var bucketProps, computeProps map[string]any
	for _, asset := range intent.Spec.Assets {
		switch asset.Type {
		case "ObjectStore":
			bucketProps = asset.Properties
		case "Compute":
			computeProps = asset.Properties
		}
	}
	if bucketProps == nil || computeProps == nil {
		t.Fatalf("expected ObjectStore and Compute assets, got %+v", intent.Spec.Assets)
	}
	if bucketProps["existingBucket"] != "data-lake" || bucketProps["versioning"] != true {
		t.Fatalf("unexpected bucket properties %+v", bucketProps)
	}
	if computeProps["existingServiceName"] != "etl" || computeProps["cluster"] != "tools" || computeProps["observeExisting"] != true {
		t.Fatalf("unexpected compute properties %+v", computeProps)
	}
	if computeProps["replicas"] != 2 {
		t.Fatalf("expected replicas 2, got %v", computeProps["replicas"])
	}
	resources := computeProps["resources"].(map[string]any)
	limits := resources["limits"].(map[string]any)
	if limits["cpu"] != "500m" {
		t.Fatalf("expected cpu 500m, got %v", limits["cpu"])
	}
	if limits["memory"] != "1024" {
		t.Fatalf("expected memory 1024, got %v", limits["memory"])
	}

	if draft.Partition.Metadata.Name != "imported" {
		t.Fatalf("unexpected partition name %s", draft.Partition.Metadata.Name)
	}
	if draft.Partition.Spec.DeletionPolicy != "orphan" {
		t.Fatalf("expected orphan deletion policy, got %s", draft.Partition.Spec.DeletionPolicy)
	}
	if draft.Partition.Spec.Defaults.TargetPusher != "aws-123456789012" {
		t.Fatalf("unexpected partition defaults %+v", draft.Partition.Spec.Defaults)
	}
}

func TestGenerateResourceGroupingAndFilters(t *testing.T) {
	result := scanResult()
	result.Buckets = []awsscan.BucketResource{{Name: "data-lake", Region: "eu-west-1"}}
	result.Secrets = []awsscan.SecretResource{{ARN: "arn:1", Name: "legacy/token", Region: "eu-west-1"}}
	draft, err := Generate(result, Options{PartitionName: "imported", Grouping: GroupingResource, IncludeTypes: []string{KindSecret}}, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(draft.Intents) != 1 || len(draft.Intents[0].Spec.Assets) != 1 {
		t.Fatalf("expected only secret import, got %+v", draft.Intents)
	}
	intent := draft.Intents[0]
	if intent.Metadata.Name != "import-secret-legacy-token" {
		t.Fatalf("unexpected intent name %s", intent.Metadata.Name)
	}
	asset := intent.Spec.Assets[0]
	if asset.Type != "Secret" || asset.Name != "legacy-token" {
		t.Fatalf("unexpected asset %+v", asset)
	}
	if asset.Properties["existingSecret"] != "legacy/token" {
		t.Fatalf("unexpected secret properties %+v", asset.Properties)
	}
	if _, has := asset.Properties["value"]; has {
		t.Fatalf("adopted secret must not embed a value")
	}
}

func TestGenerateSkipsManagedAndCrossReferences(t *testing.T) {
	result := scanResult()
	result.Buckets = []awsscan.BucketResource{
		{Name: "guardian-bucket", Region: "eu-west-1", Managed: awsscan.ManagedInfo{
			Managed: true, Partition: "demo", Intent: "network", Asset: "store",
		}},
		{Name: "already-imported", Region: "eu-west-1"},
	}
	store := memory.New()
	writeIntent(t, store, "imported", "existing-import", `
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: existing-import
spec:
  targetPusher: aws-123456789012
  target:
    account: "123456789012"
    region: eu-west-1
  assets:
    - type: ObjectStore
      name: store
      properties:
        engine: s3
        existingBucket: already-imported
`)
	draft, err := Generate(result, Options{PartitionName: "imported"}, store)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(draft.Intents) != 0 {
		t.Fatalf("expected no generated intents, got %d", len(draft.Intents))
	}
	if len(draft.Managed) != 2 {
		t.Fatalf("expected 2 managed entries, got %d", len(draft.Managed))
	}
	byIdentifier := map[string]ManagedEntry{}
	for _, entry := range draft.Managed {
		byIdentifier[entry.Identifier] = entry
	}
	if entry := byIdentifier["guardian-bucket"]; entry.ExistingIntent || entry.Partition != "demo" || entry.Intent != "network" {
		t.Fatalf("unexpected managed entry %+v", entry)
	}
	if entry := byIdentifier["already-imported"]; !entry.ExistingIntent {
		t.Fatalf("expected existingIntent cross-reference, got %+v", entry)
	}
}

func TestGenerateUnmappedEntries(t *testing.T) {
	result := scanResult()
	result.Parameters = []awsscan.ParameterResource{
		{Name: "/app/plain", Region: "eu-west-1", Type: "String", Value: "v"},
		{Name: "/app/secure", Region: "eu-west-1", Type: "SecureString"},
	}
	result.Stacks = []awsscan.StackResource{{
		Region: "eu-west-1", ID: "arn:1", Name: "foreign-stack", Status: "CREATE_COMPLETE",
	}}
	draft, err := Generate(result, Options{PartitionName: "imported"}, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(draft.Intents) != 1 || draft.Intents[0].Metadata.Name != "import-parameter-app-plain" {
		t.Fatalf("expected plain parameter import only, got %+v", draft.Intents)
	}
	reasons := map[string]string{}
	for _, entry := range draft.Unmapped {
		reasons[entry.Identifier] = entry.Reason
	}
	if _, ok := reasons["/app/secure"]; !ok {
		t.Fatalf("expected secure parameter unmapped, got %+v", draft.Unmapped)
	}
	if _, ok := reasons["foreign-stack"]; !ok {
		t.Fatalf("expected foreign stack unmapped, got %+v", draft.Unmapped)
	}
}

func TestGenerateRegionFilterAndCollisionSuffix(t *testing.T) {
	result := scanResult()
	result.Regions = []string{"eu-west-1", "us-east-1"}
	result.Parameters = []awsscan.ParameterResource{
		{Name: "/app/legacy", Region: "eu-west-1", Type: "String", Value: "v"},
		{Name: "/app/legacy", Region: "us-east-1", Type: "String", Value: "v"},
	}
	draft, err := Generate(result, Options{PartitionName: "imported", Grouping: GroupingStack}, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	names := map[string]bool{}
	for _, intent := range draft.Intents {
		names[intent.Metadata.Name] = true
	}
	if len(names) != 2 {
		t.Fatalf("expected two intents, got %v", names)
	}
	if !names["import-parameter-app-legacy-eu-west-1"] || !names["import-parameter-app-legacy-us-east-1"] {
		t.Fatalf("expected region-suffixed intent names, got %v", names)
	}

	draft, err = Generate(result, Options{PartitionName: "imported", Regions: []string{"eu-west-1"}}, nil)
	if err != nil {
		t.Fatalf("generate filtered: %v", err)
	}
	if len(draft.Intents) != 1 || draft.Intents[0].Spec.Target.Region != "eu-west-1" {
		t.Fatalf("expected single eu-west-1 intent, got %+v", draft.Intents)
	}
}

func TestGenerateRejectsBadInput(t *testing.T) {
	if _, err := Generate(nil, Options{PartitionName: "x"}, nil); err == nil {
		t.Fatalf("expected error for nil result")
	}
	result := scanResult()
	if _, err := Generate(result, Options{}, nil); err == nil {
		t.Fatalf("expected error for missing partition name")
	}
	result.Status = awsscan.ScanStatusFailed
	if _, err := Generate(result, Options{PartitionName: "x"}, nil); err == nil {
		t.Fatalf("expected error for failed scan")
	}
	result.Status = awsscan.ScanStatusSucceeded
	if _, err := Generate(result, Options{PartitionName: "x", Grouping: "nope"}, nil); err == nil {
		t.Fatalf("expected error for bad grouping")
	}
}

func TestGenerateStackLookupByPhysicalID(t *testing.T) {
	result := scanResult()
	result.Buckets = []awsscan.BucketResource{{Name: "stacked-bucket", Region: "eu-west-1"}}
	result.Stacks = []awsscan.StackResource{{
		Region: "eu-west-1", ID: "arn:2", Name: "data-stack", Status: "CREATE_COMPLETE",
		ResourceIDs: map[string]string{"Bucket": "stacked-bucket"},
	}}
	draft, err := Generate(result, Options{PartitionName: "imported"}, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(draft.Intents) != 1 || draft.Intents[0].Metadata.Name != "import-data-stack" {
		t.Fatalf("expected stack grouping via physical id, got %+v", draft.Intents)
	}
	if len(draft.Unmapped) != 1 || draft.Unmapped[0].Identifier != "data-stack" {
		t.Fatalf("expected foreign stack in unmapped, got %+v", draft.Unmapped)
	}
}

func writeIntent(t *testing.T, store guardianapi.Store, partition, name, content string) {
	t.Helper()
	if _, err := store.UpsertFiles(context.Background(), guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: paths.IntentManifest(partition, name),
			Content:     []byte(content),
		}},
	}); err != nil {
		t.Fatalf("write intent: %v", err)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Data_Lake.Production", "data-lake-production"},
		{"/app/legacy", "app-legacy"},
		{"UPPER", "upper"},
		{"---", ""},
		{strings.Repeat("a", 100), strings.Repeat("a", maxNameLen)},
	}
	for _, tc := range cases {
		if got := sanitizeName(tc.input); got != tc.want {
			t.Fatalf("sanitizeName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
