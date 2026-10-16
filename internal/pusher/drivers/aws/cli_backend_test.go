package awsdriver

import (
	"reflect"
	"testing"
)

func TestTagArgsSkipsEmptyValues(t *testing.T) {
	args := tagArgs(map[string]string{
		"guardian.managed":   "true",
		"guardian.cluster":   "",
		"guardian.partition": "demo-deploy",
		"guardian.hash":      "   ",
	})
	want := []string{
		"--tags", "guardian.managed=true",
		"--tags", "guardian.partition=demo-deploy",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("tagArgs = %v, want %v", args, want)
	}
}

func TestTagArgsStableOrder(t *testing.T) {
	args := tagArgs(map[string]string{"b": "2", "a": "1"})
	want := []string{"--tags", "a=1", "--tags", "b=2"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("tagArgs = %v, want %v", args, want)
	}
}
