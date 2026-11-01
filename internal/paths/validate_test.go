package paths

import "testing"

func TestValidateSegmentRejectsTraversal(t *testing.T) {
	cases := []string{"", ".", "..", "../victim", "a/b", `a\b`, "/abs", "UPPER", "with space", "a.b"}
	for _, value := range cases {
		if err := ValidatePartitionName(value); err == nil {
			t.Fatalf("ValidatePartitionName(%q) = nil, want error", value)
		}
	}
	for _, value := range []string{"demo", "team-a", "a1"} {
		if err := ValidatePartitionName(value); err != nil {
			t.Fatalf("ValidatePartitionName(%q) = %v", value, err)
		}
	}
}

func TestValidatePusherNameAllowsClusterDots(t *testing.T) {
	if err := ValidatePusherName("k8s-kind.prod-cluster"); err != nil {
		t.Fatalf("dotted pusher name rejected: %v", err)
	}
	for _, value := range []string{"", ".", "..", "../x", "a/b", `a\b`} {
		if err := ValidatePusherName(value); err == nil {
			t.Fatalf("ValidatePusherName(%q) = nil, want error", value)
		}
	}
}

func TestValidateTaskID(t *testing.T) {
	valid := []string{"task_123_abcdef", "task-1", "A.B_c-1"}
	for _, value := range valid {
		if err := ValidateTaskID(value); err != nil {
			t.Fatalf("ValidateTaskID(%q) = %v", value, err)
		}
	}
	invalid := []string{"", ".", "..", "../x", "a/b", `a\b`, ".hidden"}
	for _, value := range invalid {
		if err := ValidateTaskID(value); err == nil {
			t.Fatalf("ValidateTaskID(%q) = nil, want error", value)
		}
	}
}

func TestValidateEnvName(t *testing.T) {
	if err := ValidateEnvName("prod"); err != nil {
		t.Fatalf("valid env rejected: %v", err)
	}
	for _, value := range []string{"", ".", "..", "../x", "prod/us", "PROD"} {
		if err := ValidateEnvName(value); err == nil {
			t.Fatalf("ValidateEnvName(%q) = nil, want error", value)
		}
	}
}
