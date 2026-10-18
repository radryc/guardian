package manifest

import "testing"

func TestParsePartition(t *testing.T) {
	data := []byte(`
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: demo
spec:
  deletionPolicy: orphan
  reconciliation:
    mode: auto
    interval: 30s
  sets: [Core, Department X]
  primarySet: Core
`)

	part, err := ParsePartition(data)
	if err != nil {
		t.Fatalf("ParsePartition() error = %v", err)
	}
	if part.Metadata.Name != "demo" {
		t.Fatalf("part.Metadata.Name = %q, want demo", part.Metadata.Name)
	}
	if len(part.Spec.Sets) != 2 || part.Spec.Sets[0] != "Core" || part.Spec.Sets[1] != "Department X" {
		t.Fatalf("part.Spec.Sets = %v", part.Spec.Sets)
	}
	if part.Spec.PrimarySet != "Core" {
		t.Fatalf("part.Spec.PrimarySet = %q", part.Spec.PrimarySet)
	}
}

func TestParseIntent(t *testing.T) {
	data := []byte(`
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: app
spec:
  targetPusher: local
  target:
    cluster: local
  locked: false
  assets:
    - type: Compute
      name: web
      properties:
        image: app:v1
`)

	intent, err := ParseIntent(data)
	if err != nil {
		t.Fatalf("ParseIntent() error = %v", err)
	}
	if intent.Metadata.Name != "app" {
		t.Fatalf("intent.Metadata.Name = %q, want app", intent.Metadata.Name)
	}
	if intent.Spec.IntentType != "standard" {
		t.Fatalf("intent.Spec.IntentType = %q, want standard", intent.Spec.IntentType)
	}
}
