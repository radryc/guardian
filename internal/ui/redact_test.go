package ui

import (
	"context"
	"strings"
	"testing"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	partitiondomain "github.com/radryc/guardian/internal/domain/partition"
	targetdomain "github.com/radryc/guardian/internal/domain/target"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/store/memory"
)

func TestSaveBundleRejectsIntentTraversal(t *testing.T) {
	srv := &Server{store: memory.New(), principalID: "test"}
	req := &SaveBundleRequest{
		Partition: partitiondomain.Partition{
			APIVersion: "guardian/v1alpha1",
			Kind:       "Partition",
			Metadata:   partitiondomain.Metadata{Name: "demo"},
			Spec: partitiondomain.Spec{
				DeletionPolicy: "orphan",
				Reconciliation: partitiondomain.ReconciliationSpec{Mode: "auto", Interval: "10m"},
			},
		},
		Intents: []SaveIntentRequest{{
			Manifest: intentdomain.Intent{
				APIVersion: "guardian/v1alpha1",
				Kind:       "Intent",
				Metadata:   intentdomain.Metadata{Name: "../.registry"},
				Spec: intentdomain.IntentSpec{
					IntentType:   "standard",
					TargetPusher: "local",
					Assets:       []assetdomain.Spec{{Type: "Compute", Name: "web"}},
				},
			},
		}},
	}
	_, err := srv.saveBundle(context.Background(), "demo", req)
	if err == nil {
		t.Fatal("saveBundle() = nil, want traversal rejection")
	}
	if !strings.Contains(err.Error(), "invalid intent name") {
		t.Fatalf("error = %v, want intent name validation", err)
	}
	if _, readErr := srv.store.ReadFile(context.Background(), "/.registry"); readErr == nil {
		t.Fatal("traversal write reached the registry namespace")
	}
}

func TestSecretPropertiesRedactedForUI(t *testing.T) {
	properties := map[string]any{"value": "super-secret", "secretRef": "monofs-secret://demo/token"}
	redacted := redactSecretPropertiesForUI("Secret", properties)
	if redacted["value"] != redactedSecretValue {
		t.Fatalf("value = %v, want redacted", redacted["value"])
	}
	if redacted["secretRef"] != "monofs-secret://demo/token" {
		t.Fatalf("secretRef unexpectedly redacted: %v", redacted["secretRef"])
	}
	if properties["value"] != "super-secret" {
		t.Fatal("redaction mutated the source properties")
	}

	manifest := intentdomain.Intent{
		Metadata: intentdomain.Metadata{Name: "secrets"},
		Spec: intentdomain.IntentSpec{Assets: []assetdomain.Spec{{
			Type:       "Secret",
			Name:       "token",
			Properties: properties,
		}}},
	}
	redactedManifest := redactIntentManifestForUI(manifest)
	if redactedManifest.Spec.Assets[0].Properties["value"] != redactedSecretValue {
		t.Fatalf("manifest value = %v, want redacted", redactedManifest.Spec.Assets[0].Properties["value"])
	}
	if manifest.Spec.Assets[0].Properties["value"] != "super-secret" {
		t.Fatal("manifest redaction mutated the source")
	}
}

func TestSaveBundleRestoresRedactedSecret(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	srv := &Server{store: store, principalID: "test"}

	existing := `apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: secrets
spec:
  intentType: standard
  targetPusher: local
  assets:
    - type: Secret
      name: token
      properties:
        value: super-secret
`
	writeAuthzFixture(t, store, paths.IntentManifest("demo", "secrets"), existing)

	req := &SaveBundleRequest{
		Partition: partitiondomain.Partition{
			APIVersion: "guardian/v1alpha1",
			Kind:       "Partition",
			Metadata:   partitiondomain.Metadata{Name: "demo"},
			Spec: partitiondomain.Spec{
				DeletionPolicy: "orphan",
				Reconciliation: partitiondomain.ReconciliationSpec{Mode: "auto", Interval: "10m"},
			},
		},
		Intents: []SaveIntentRequest{{
			Manifest: intentdomain.Intent{
				APIVersion: "guardian/v1alpha1",
				Kind:       "Intent",
				Metadata:   intentdomain.Metadata{Name: "secrets"},
				Spec: intentdomain.IntentSpec{
					IntentType:   "standard",
					TargetPusher: "local",
					Target:       targetdomain.Placement{Cluster: "local"},
					Assets: []assetdomain.Spec{{
						Type:       "Secret",
						Name:       "token",
						Properties: map[string]any{"value": redactedSecretValue},
					}},
				},
			},
		}},
	}
	if _, err := srv.saveBundle(ctx, "demo", req); err != nil {
		t.Fatalf("saveBundle: %v", err)
	}
	stored, err := store.ReadFile(ctx, paths.IntentManifest("demo", "secrets"))
	if err != nil {
		t.Fatalf("read saved intent: %v", err)
	}
	if !strings.Contains(string(stored), "value: super-secret") {
		t.Fatalf("stored manifest did not restore the secret:\n%s", stored)
	}
}
