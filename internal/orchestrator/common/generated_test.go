package common

import (
	"testing"

	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
)

const cdkIntentManifest = `apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: stack
spec:
  intentType: standard
  implementation: cdk
  targetPusher: aws-123
  target:
    account: "123456789012"
    region: eu-west-1
  locked: false
  assets:
    - type: Secret
      name: token
      properties:
        value: x
    - type: Volume
      name: data
      properties:
        size: 1Gi
    - type: Compute
      name: web
      dependsOn:
        - data
      properties:
        image: nginx
`

const nativeIntentManifest = `apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: stack
spec:
  intentType: standard
  targetPusher: aws-123
  target:
    account: "123456789012"
    region: eu-west-1
  locked: false
  assets:
    - type: Volume
      name: data
      properties:
        size: 1Gi
`

func TestBuildTaskSubstitutesGeneratedStack(t *testing.T) {
	state := &statedomain.IntentState{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "IntentState",
		Partition:    "demo",
		Intent:       "stack",
		TargetPusher: "aws-123",
		Outputs:      map[string]string{},
	}
	task, err := BuildTaskFromManifest(state, []byte(cdkIntentManifest), taskdomain.OpDiff, nil, false)
	if err != nil {
		t.Fatalf("build task: %v", err)
	}
	if len(task.Assets) != 2 {
		t.Fatalf("assets = %+v, want [Secret, GeneratedStack]", task.Assets)
	}
	if task.Assets[0].Type != "Secret" || task.Assets[0].Name != "token" {
		t.Fatalf("first asset = %+v, want native Secret", task.Assets[0])
	}
	stack := task.Assets[1]
	if stack.Type != "GeneratedStack" || stack.Name != "aws-stack" {
		t.Fatalf("synthetic asset = %+v", stack)
	}
	if got := stack.Payload["generated"]; got != "/partitions/demo/payloads/aws-generated/stack" {
		t.Fatalf("generated payload dir = %q", got)
	}
}

func TestBuildTaskLeavesNativeIntentUntouched(t *testing.T) {
	state := &statedomain.IntentState{
		APIVersion:   "guardian/v1alpha1",
		Kind:         "IntentState",
		Partition:    "demo",
		Intent:       "stack",
		TargetPusher: "aws-123",
		Outputs:      map[string]string{},
	}
	task, err := BuildTaskFromManifest(state, []byte(nativeIntentManifest), taskdomain.OpDiff, nil, false)
	if err != nil {
		t.Fatalf("build task: %v", err)
	}
	if len(task.Assets) != 1 || task.Assets[0].Type != "Volume" {
		t.Fatalf("assets = %+v, want native Volume only", task.Assets)
	}
}
