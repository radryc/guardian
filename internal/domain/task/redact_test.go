package task

import (
	"reflect"
	"testing"
)

func TestSecretAssetNames(t *testing.T) {
	var nilTask *Task
	if got := nilTask.SecretAssetNames(); got != nil {
		t.Fatalf("nil task secrets = %v, want nil", got)
	}
	task := &Task{Assets: []AbstractAsset{
		{Type: "Secret", Name: "api-key"},
		{Type: "secret", Name: "db-password"},
		{Type: "Compute", Name: "app"},
	}}
	want := []string{"api-key", "db-password"}
	if got := task.SecretAssetNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SecretAssetNames = %v, want %v", got, want)
	}
}

func TestRedactSecretOutputs(t *testing.T) {
	task := &Task{Assets: []AbstractAsset{{Type: "Secret", Name: "api-key"}}}
	inputs := map[string]string{
		"api-key.value":     "super-secret",
		"api-key.valueHash": "abc123",
		"api-key.secretRef": "arn:secret",
		"other.value":       "keep-me",
	}
	got := RedactSecretOutputs(task, inputs)
	if got["api-key.value"] != RedactedValue {
		t.Fatalf("secret value = %q, want redacted", got["api-key.value"])
	}
	for _, key := range []string{"api-key.valueHash", "api-key.secretRef", "other.value"} {
		if got[key] != inputs[key] {
			t.Fatalf("%s = %q, want %q", key, got[key], inputs[key])
		}
	}
	if inputs["api-key.value"] != "super-secret" {
		t.Fatal("RedactSecretOutputs mutated the caller's map")
	}
	if RedactSecretOutputs(task, nil) != nil {
		t.Fatal("nil outputs must stay nil")
	}
}

func TestRedactSecretStrings(t *testing.T) {
	values := []string{"token=abc and abc again", "unrelated"}
	got := RedactSecretStrings(values, []string{"abc", ""})
	want := []string{"token=****** and ****** again", "unrelated"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RedactSecretStrings = %v, want %v", got, want)
	}
	if values[0] != "token=abc and abc again" {
		t.Fatal("RedactSecretStrings mutated its input")
	}
}
