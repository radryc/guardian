package observed

import (
	"testing"
	"time"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	statedomain "github.com/radryc/guardian/internal/domain/state"
	"github.com/radryc/guardian/internal/gcpobserve"
	"github.com/radryc/guardian/internal/k8sobserve"
)

func TestGCPResourceObservationsKeepFlatDurableResourcesAndMarkRevisions(t *testing.T) {
	t.Parallel()

	base := "//run.googleapis.com/projects/p/locations/europe-west1"
	stack := gcpobserve.StackState{
		StackName: "diva-shared/gcp",
		Resources: []gcpobserve.StackResource{
			{
				Name:        base + "/services/guardian-diva-shared-gcp-app",
				DisplayName: "guardian-diva-shared-gcp-app",
				AssetType:   "run.googleapis.com/Service",
				Location:    "europe-west1",
				State:       "ACTIVE",
				UpdateTime:  time.Now().UTC(),
			},
			{
				Name:        base + "/revisions/guardian-diva-shared-gcp-app-00001-old",
				DisplayName: "guardian-diva-shared-gcp-app-00001-old",
				AssetType:   "run.googleapis.com/Revision",
				Location:    "europe-west1",
				State:       "ACTIVE",
				CreateTime:  time.Now().UTC().Add(-time.Hour),
			},
			{
				Name:        base + "/revisions/guardian-diva-shared-gcp-app-00002-new",
				DisplayName: "guardian-diva-shared-gcp-app-00002-new",
				AssetType:   "run.googleapis.com/Revision",
				Location:    "europe-west1",
				State:       "ACTIVE",
				CreateTime:  time.Now().UTC(),
			},
		},
	}
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "gcp"},
		Spec: intentdomain.IntentSpec{
			Assets: []assetdomain.Spec{{Type: assetdomain.TypeCompute, Name: "app"}},
		},
	}
	resources := gcpResourceObservations("diva-shared", stack, intent, &statedomain.IntentState{Outputs: map[string]string{}})
	if len(resources) != 3 {
		t.Fatalf("resources = %d, want 3", len(resources))
	}
	byType := map[string][]statedomain.ResourceObservation{}
	for _, resource := range resources {
		byType[resource.CfnType] = append(byType[resource.CfnType], resource)
	}
	service := byType["run.googleapis.com/Service"][0]
	if service.Provider != "gcp" || service.Ephemeral {
		t.Fatalf("service = %+v", service)
	}
	revisions := byType["run.googleapis.com/Revision"]
	if len(revisions) != 2 {
		t.Fatalf("revisions = %d", len(revisions))
	}
	for _, revision := range revisions {
		if !revision.Ephemeral {
			t.Fatalf("revision not marked ephemeral: %+v", revision)
		}
		if revision.Parent != service.LogicalID {
			t.Fatalf("revision parent = %q, want %q", revision.Parent, service.LogicalID)
		}
	}
}

func TestGCPResourceObservationsIgnorePrefixCollision(t *testing.T) {
	t.Parallel()

	stack := gcpobserve.StackState{
		Resources: []gcpobserve.StackResource{
			{
				Name:        "//run.googleapis.com/projects/p/locations/europe-west1/services/guardian-p-app-app",
				DisplayName: "guardian-p-app-app",
				AssetType:   "run.googleapis.com/Service",
			},
			{
				Name:        "//run.googleapis.com/projects/p/locations/europe-west1/services/guardian-p-app-v2-app",
				DisplayName: "guardian-p-app-v2-app",
				AssetType:   "run.googleapis.com/Service",
			},
		},
	}
	intent := &intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "app"},
		Spec: intentdomain.IntentSpec{
			Assets: []assetdomain.Spec{{Type: assetdomain.TypeCompute, Name: "app"}},
		},
	}
	resources := gcpResourceObservations("p", stack, intent, &statedomain.IntentState{Outputs: map[string]string{}})
	if len(resources) != 1 {
		t.Fatalf("resources = %d, want only the app intent service: %+v", len(resources), resources)
	}
	if resources[0].Name != "guardian-p-app-app" {
		t.Fatalf("unexpected resource: %+v", resources[0])
	}
}

func TestGCPRevisionOrdinalIgnoresNumericHash(t *testing.T) {
	t.Parallel()
	cases := map[string]int64{
		"guardian-p-app-00001-f4z": 1,
		"guardian-p-app-00002-876": 2,
		"guardian-p-app-00003-8cd": 3,
		"guardian-p-app":           0,
	}
	for name, want := range cases {
		if got := gcpRevisionOrdinal(name); got != want {
			t.Fatalf("gcpRevisionOrdinal(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestK8sResourceObservationsFlattenManagedObjects(t *testing.T) {
	t.Parallel()

	inventory := k8sobserve.Inventory{
		GeneratedAt: time.Now().UTC(),
		Resources: []k8sobserve.Resource{
			{Namespace: "doctor", Intent: "controller", Asset: "controller", Kind: "Deployment", Name: "controller", Status: "Available", Ready: "1/1 ready"},
			{Namespace: "doctor", Intent: "controller", Asset: "controller", Kind: "Service", Name: "controller", Status: "ClusterIP", Ready: "10.0.0.1"},
		},
	}
	resources := k8sResourceObservations(inventory)
	if len(resources) != 2 {
		t.Fatalf("resources = %d", len(resources))
	}
	if resources[0].AssetName != "k8s-res-deployment-controller" {
		t.Fatalf("asset name = %q, want k8s-res-deployment-controller", resources[0].AssetName)
	}
	if resources[1].AssetName != "k8s-res-service-controller" {
		t.Fatalf("asset name = %q, want k8s-res-service-controller", resources[1].AssetName)
	}
	for _, resource := range resources {
		if resource.Provider != "kubernetes" || resource.Ephemeral {
			t.Fatalf("resource = %+v", resource)
		}
		if resource.Parent != "" {
			t.Fatalf("resource parent = %q, want flat", resource.Parent)
		}
		if resource.Location != "doctor" {
			t.Fatalf("location = %q", resource.Location)
		}
	}
}
