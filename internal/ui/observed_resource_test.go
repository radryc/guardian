package ui

import (
	"context"
	"testing"
	"time"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	statedomain "github.com/radryc/guardian/internal/domain/state"
)

func TestBuildIntentDocumentAddsGCPResourcesToStandardIntent(t *testing.T) {
	t.Parallel()

	manifest := intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "gcp"},
		Spec: intentdomain.IntentSpec{
			IntentType:   intentdomain.TypeStandard,
			TargetPusher: "gcp-demo",
			Assets:       []assetdomain.Spec{{Type: assetdomain.TypeCompute, Name: "app"}},
		},
	}
	observedAt := time.Now().UTC()
	state := &statedomain.IntentState{
		IntentType: intentdomain.TypeStandard,
		Status:     statedomain.StatusHealthy,
		Resources: []statedomain.ResourceObservation{
			{
				LogicalID:     "//run.googleapis.com/projects/p/locations/europe-west1/services/guardian-gcp-app",
				CfnType:       "run.googleapis.com/Service",
				AssetName:     "gcp-res-app",
				Name:          "guardian-gcp-app",
				Status:        statedomain.ResourceInSync,
				Provider:      "gcp",
				Location:      "europe-west1",
				PhysicalID:    "//run.googleapis.com/projects/p/locations/europe-west1/services/guardian-gcp-app",
				LiveStatus:    "ACTIVE",
				LiveUpdatedAt: observedAt,
			},
			{
				LogicalID:     "//run.googleapis.com/projects/p/locations/europe-west1/revisions/guardian-gcp-app-00002-abc",
				CfnType:       "run.googleapis.com/Revision",
				AssetName:     "gcp-res-rev-2",
				Name:          "guardian-gcp-app-00002-abc",
				Status:        statedomain.ResourceInSync,
				Provider:      "gcp",
				PhysicalID:    "//run.googleapis.com/projects/p/locations/europe-west1/revisions/guardian-gcp-app-00002-abc",
				LiveStatus:    "ACTIVE",
				LiveUpdatedAt: observedAt.Add(time.Minute),
				Ephemeral:     true,
				Parent:        "//run.googleapis.com/projects/p/locations/europe-west1/services/guardian-gcp-app",
			},
			{
				LogicalID:     "//run.googleapis.com/projects/p/locations/europe-west1/revisions/guardian-gcp-app-00001-old",
				CfnType:       "run.googleapis.com/Revision",
				AssetName:     "gcp-res-rev-1",
				Name:          "guardian-gcp-app-00001-old",
				Status:        statedomain.ResourceInSync,
				Provider:      "gcp",
				PhysicalID:    "//run.googleapis.com/projects/p/locations/europe-west1/revisions/guardian-gcp-app-00001-old",
				LiveStatus:    "ACTIVE",
				LiveUpdatedAt: observedAt,
				Ephemeral:     true,
				Parent:        "//run.googleapis.com/projects/p/locations/europe-west1/services/guardian-gcp-app",
			},
		},
	}

	doc := buildIntentDocument(context.Background(), nil, manifest, "demo", nil, "ver-1", state, intentTaskRuntime{}, []string{"app"}, nil, nil, false, nil)
	if len(doc.Assets) != 2 {
		t.Fatalf("assets = %+v, want app + one durable resource", doc.Assets)
	}
	byName := map[string]AssetDocument{}
	for _, asset := range doc.Assets {
		byName[asset.Name] = asset
	}
	service, ok := byName["gcp-res-app"]
	if !ok {
		t.Fatalf("gcp service asset missing: %+v", doc.Assets)
	}
	if service.Type != assetdomain.TypeObservedResource {
		t.Fatalf("service type = %q, want %q", service.Type, assetdomain.TypeObservedResource)
	}
	if got := service.Properties["latestRevision"]; got != "guardian-gcp-app-00002-abc" {
		t.Fatalf("latestRevision = %v", got)
	}
	for _, asset := range doc.Assets {
		if asset.Type == assetdomain.TypeObservedResource && asset.Name != "gcp-res-app" {
			t.Fatalf("ephemeral revision was rendered as an asset: %+v", asset)
		}
	}

	topology := buildTopology(newPartition("demo"), []IntentDocument{doc})
	nodes := map[string]TopologyNode{}
	for _, node := range topology.Nodes {
		nodes[node.ID] = node
	}
	node, ok := nodes["asset:gcp:gcp-res-app"]
	if !ok {
		t.Fatalf("gcp resource node missing: %+v", topology.Nodes)
	}
	if node.Meta["provider"] != "gcp" || node.Meta["tier"] != "application" {
		t.Fatalf("gcp node meta = %+v", node.Meta)
	}
	if node.ParentID != "intent:gcp" {
		t.Fatalf("gcp resource parent = %q, want intent gcp (flat)", node.ParentID)
	}
}

func TestBuildIntentDocumentAddsKubernetesResourcesToStandardIntent(t *testing.T) {
	t.Parallel()

	manifest := intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "k8s"},
		Spec: intentdomain.IntentSpec{
			IntentType:   intentdomain.TypeStandard,
			TargetPusher: "k8s-main",
			Assets:       []assetdomain.Spec{{Type: assetdomain.TypeCompute, Name: "client"}},
		},
	}
	state := &statedomain.IntentState{
		IntentType: intentdomain.TypeStandard,
		Status:     statedomain.StatusHealthy,
		Resources: []statedomain.ResourceObservation{
			{
				LogicalID:  "div-demo/Deployment/client",
				CfnType:    "Deployment",
				AssetName:  "k8s-res-deployment-client",
				Name:       "client",
				Status:     statedomain.ResourceInSync,
				Provider:   "kubernetes",
				Location:   "div-demo",
				PhysicalID: "div-demo/client",
				LiveStatus: "1/1 ready",
			},
			{
				LogicalID:  "div-demo/Service/client",
				CfnType:    "Service",
				AssetName:  "k8s-res-service-client",
				Name:       "client",
				Status:     statedomain.ResourceInSync,
				Provider:   "kubernetes",
				Location:   "div-demo",
				PhysicalID: "div-demo/client",
				LiveStatus: "ClusterIP",
			},
			{
				LogicalID:  "div-demo/ConfigMap/client-config",
				CfnType:    "ConfigMap",
				AssetName:  "k8s-res-configmap-client-config",
				Name:       "client-config",
				Status:     statedomain.ResourceInSync,
				Provider:   "kubernetes",
				Location:   "div-demo",
				PhysicalID: "div-demo/client-config",
				LiveStatus: "2 keys",
			},
		},
	}

	doc := buildIntentDocument(context.Background(), nil, manifest, "demo", nil, "ver-1", state, intentTaskRuntime{}, []string{"client"}, nil, nil, false, nil)
	if len(doc.Assets) != 4 {
		t.Fatalf("assets = %d, want client + 3 resources", len(doc.Assets))
	}
	topology := buildTopology(newPartition("demo"), []IntentDocument{doc})
	byID := map[string]TopologyNode{}
	for _, node := range topology.Nodes {
		byID[node.ID] = node
	}
	deployment := byID["asset:k8s:k8s-res-deployment-client"]
	if deployment.Meta["provider"] != "kubernetes" || deployment.Meta["tier"] != "application" {
		t.Fatalf("deployment meta = %+v", deployment.Meta)
	}
	if deployment.ParentID != "intent:k8s" {
		t.Fatalf("deployment parent = %q, want intent (flat)", deployment.ParentID)
	}
	configMap := byID["asset:k8s:k8s-res-configmap-client-config"]
	if configMap.Meta["tier"] != "infrastructure" {
		t.Fatalf("configmap tier = %q, want infrastructure", configMap.Meta["tier"])
	}
}
