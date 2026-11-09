package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/cdkgen"
	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func TestIntentOriginAdoptedAndObserved(t *testing.T) {
	store := memory.New()
	observed := intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "web"},
		Spec: intentdomain.IntentSpec{
			IntentType: "observed",
			Assets:     []assetdomain.Spec{{Type: "CDKStack", Name: "web"}},
		},
	}

	origin := intentOrigin(context.Background(), store, "demo-import", observed, map[string]string{
		"guardian.io/origin":        "adopted",
		"guardian.io/adopted-stack": "DemoImportStack",
	})
	if origin.Origin != originAdopted || origin.Label != "Adopted from AWS" {
		t.Fatalf("adopted origin = %+v", origin)
	}
	if !strings.Contains(origin.Detail, "DemoImportStack") {
		t.Fatalf("adopted detail = %q", origin.Detail)
	}

	origin = intentOrigin(context.Background(), store, "demo-observe", observed, nil)
	if origin.Origin != originObserved || origin.Label != "Observe only" {
		t.Fatalf("observed origin = %+v", origin)
	}
}

func TestIntentOriginGeneratedAndUserCDK(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	intent := intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "web"},
		Spec: intentdomain.IntentSpec{
			Implementation: intentdomain.ImplementationCDK,
			Assets:         []assetdomain.Spec{{Type: "Network", Name: "main"}},
		},
	}

	// A CDK intent without a generated payload is user-authored CDK code.
	origin := intentOrigin(ctx, store, "demo-deploy", intent, nil)
	if origin.Origin != originUserCDK || origin.Label != "User CDK" {
		t.Fatalf("user cdk origin = %+v", origin)
	}

	payload := cdkgen.PayloadManifest{
		StackName:      "guardian-demo-deploy-web",
		CatalogVersion: "v1",
		Templates:      []cdkgen.TemplateRef{{ID: "aws.network.vpc"}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := paths.GeneratedAWSPayloadDir("demo-deploy", "web") + "/" + cdkgen.PayloadManifestFileName
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: manifestPath, Content: raw}},
	}); err != nil {
		t.Fatalf("write payload manifest: %v", err)
	}

	origin = intentOrigin(ctx, store, "demo-deploy", intent, nil)
	if origin.Origin != originGenerated || origin.Label != "Native CDK" {
		t.Fatalf("generated origin = %+v", origin)
	}
	if !strings.Contains(origin.Detail, "guardian-demo-deploy-web") || !strings.Contains(origin.Detail, "catalog v1") {
		t.Fatalf("generated detail = %q", origin.Detail)
	}
}

func TestIntentOriginStandardCDKStack(t *testing.T) {
	origin := intentOrigin(context.Background(), memory.New(), "p", intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "web"},
		Spec: intentdomain.IntentSpec{
			IntentType: intentdomain.TypeStandard,
			Assets:     []assetdomain.Spec{{Type: "CDKStack", Name: "web"}},
		},
	}, nil)
	if origin.Origin != originUserCDK || origin.Label != "User CDK" {
		t.Fatalf("cdk stack origin = %+v", origin)
	}
}

func TestIntentOriginNative(t *testing.T) {
	origin := intentOrigin(context.Background(), memory.New(), "p", intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "web"},
		Spec: intentdomain.IntentSpec{
			Implementation: intentdomain.ImplementationNative,
			Assets:         []assetdomain.Spec{{Type: "Compute", Name: "api"}},
		},
	}, nil)
	if origin.Origin != originNative || origin.Label != "Native driver" {
		t.Fatalf("native origin = %+v", origin)
	}
}

func TestAssetOriginsClassifyTemplatesAndFallback(t *testing.T) {
	intent := intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "web"},
		Spec: intentdomain.IntentSpec{
			Implementation: intentdomain.ImplementationCDK,
			Assets: []assetdomain.Spec{
				{Type: "Network", Name: "main"},
				{Type: "AWSResource", Name: "queue", Properties: map[string]any{"type": "AWS::SQS::Queue"}},
				{Type: "Secret", Name: "token"},
			},
		},
	}
	origins := assetOrigins(intent, &cdkgen.PayloadManifest{GeneratedAssets: []string{"main"}})

	if got := origins["main"]; got.Origin != originGenerated || got.Template != "aws.network.vpc" {
		t.Fatalf("network origin = %+v", got)
	}
	if got := origins["queue"]; got.Origin != originGenerated || got.Label != "fallback" || got.Template != "aws.fallback.cfn-resource" {
		t.Fatalf("fallback origin = %+v", got)
	}
	if got := origins["token"]; got.Origin != originNative {
		t.Fatalf("native asset origin = %+v", got)
	}
}

// readSpyStore records ReadFile calls while delegating to an in-memory store.
type readSpyStore struct {
	guardianapi.Store
	reads []string
}

func (s *readSpyStore) ReadFile(ctx context.Context, logicalPath string) ([]byte, error) {
	s.reads = append(s.reads, logicalPath)
	return s.Store.ReadFile(ctx, logicalPath)
}

func TestGeneratedPayloadManifestProbesOnlyCDKIntents(t *testing.T) {
	ctx := context.Background()
	spy := &readSpyStore{Store: memory.New()}

	native := intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "images"},
		Spec: intentdomain.IntentSpec{
			Implementation: intentdomain.ImplementationNative,
			Assets:         []assetdomain.Spec{{Type: "Compute", Name: "api"}},
		},
	}
	if payload, ok := generatedPayloadManifestForIntent(ctx, spy, "agent", native); ok || payload != nil {
		t.Fatalf("native intent payload = %+v, %v; want none", payload, ok)
	}
	if len(spy.reads) != 0 {
		t.Fatalf("native intent probed generated payload: %v", spy.reads)
	}

	cdk := native
	cdk.Metadata.Name = "stack"
	cdk.Spec.Implementation = intentdomain.ImplementationCDK
	if payload, ok := generatedPayloadManifestForIntent(ctx, spy, "agent", cdk); ok || payload != nil {
		t.Fatalf("absent CDK payload = %+v, %v; want none", payload, ok)
	}
	want := paths.GeneratedAWSPayloadDir("agent", "stack") + "/" + cdkgen.PayloadManifestFileName
	if len(spy.reads) != 1 || spy.reads[0] != want {
		t.Fatalf("CDK probe reads = %v; want [%s]", spy.reads, want)
	}
}

func TestPartitionOriginAggregatesIntents(t *testing.T) {
	adopted := originInfo{Origin: originAdopted, Label: "Adopted from AWS"}

	got := partitionOrigin(nil, []IntentDocument{{Origin: originGenerated}, {Origin: originGenerated}})
	if got.Origin != originGenerated {
		t.Fatalf("generated partition origin = %+v", got)
	}
	got = partitionOrigin(nil, []IntentDocument{{Origin: originGenerated}, {Origin: originObserved}})
	if got.Origin != "mixed" || !strings.Contains(got.Detail, "generated") {
		t.Fatalf("mixed partition origin = %+v", got)
	}
	// The adopted label wins over the intent origins.
	got = partitionOrigin(map[string]string{"guardian.io/origin": "adopted"}, []IntentDocument{{Origin: originGenerated}})
	if got.Origin != adopted.Origin || got.Label != adopted.Label {
		t.Fatalf("adopted partition origin = %+v", got)
	}
}
