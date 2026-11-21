package partition

import "testing"

func TestIsEnabledDefaultsToTrue(t *testing.T) {
	var zero Partition
	if !zero.IsEnabled() {
		t.Fatal("zero-value partition must be enabled")
	}
	var nilPartition *Partition
	if !nilPartition.IsEnabled() {
		t.Fatal("nil partition must be treated as enabled")
	}

	disabled := false
	if (&Partition{Spec: Spec{Enabled: &disabled}}).IsEnabled() {
		t.Fatal("explicitly disabled partition reported enabled")
	}

	enabled := true
	if !(&Partition{Spec: Spec{Enabled: &enabled}}).IsEnabled() {
		t.Fatal("explicitly enabled partition reported disabled")
	}
}
