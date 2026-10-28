package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/radryc/guardian/internal/awsgen"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	partitiondomain "github.com/radryc/guardian/internal/domain/partition"
	targetdomain "github.com/radryc/guardian/internal/domain/target"
)

func TestWriteDraftBundle(t *testing.T) {
	dir := t.TempDir()
	draft := &awsgen.Draft{
		Partition: partitiondomain.Partition{
			APIVersion: "guardian/v1alpha1",
			Kind:       "Partition",
			Metadata:   partitiondomain.Metadata{Name: "demo-import"},
		},
		Intents: []intentdomain.Intent{
			{
				APIVersion: "guardian/v1alpha1",
				Kind:       "Intent",
				Metadata:   intentdomain.Metadata{Name: "import-bucket"},
				Spec: intentdomain.IntentSpec{
					IntentType:   intentdomain.TypeStandard,
					TargetPusher: "aws-123456789012",
					Target:       targetdomain.Placement{Account: "123456789012", Region: "eu-west-1"},
				},
			},
			{
				APIVersion: "guardian/v1alpha1",
				Kind:       "Intent",
				Metadata:   intentdomain.Metadata{Name: "import-service"},
				Spec: intentdomain.IntentSpec{
					IntentType:   intentdomain.TypeStandard,
					TargetPusher: "aws-123456789012",
					Target:       targetdomain.Placement{Account: "123456789012", Region: "eu-west-1"},
				},
			},
		},
	}
	if err := writeDraftBundle(dir, draft); err != nil {
		t.Fatalf("writeDraftBundle: %v", err)
	}
	for _, rel := range []string{"config.yaml", "intents/import-bucket.yaml", "intents/import-service.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	bundle, err := loadLocalPartitionBundle(dir, false)
	if err != nil {
		t.Fatalf("loadLocalPartitionBundle: %v", err)
	}
	if bundle.PartitionName != "demo-import" {
		t.Fatalf("partition name = %q", bundle.PartitionName)
	}
	if len(bundle.IntentManifests) != 2 {
		t.Fatalf("intents = %d, want 2", len(bundle.IntentManifests))
	}
}

func TestSplitCSV(t *testing.T) {
	cases := map[string][]string{
		"":                  nil,
		" a , b ,,c ":       {"a", "b", "c"},
		"bucket,fileSystem": {"bucket", "fileSystem"},
		"single":            {"single"},
	}
	for input, want := range cases {
		got := splitCSV(input)
		if len(got) != len(want) {
			t.Fatalf("splitCSV(%q) = %v, want %v", input, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("splitCSV(%q) = %v, want %v", input, got, want)
			}
		}
	}
}
