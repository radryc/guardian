package state

import "testing"

func TestNormalizePartitionRuntimeTreatsDependencyBlocksAsProgressing(t *testing.T) {
	runtime := &PartitionRuntime{
		Partition: "demo",
		PartitionState: &PartitionState{
			Partition:      "demo",
			IntentVersions: map[string]string{"core": "core-v1", "worker": "worker-v1"},
		},
		Intents: map[string]*IntentState{
			"core":   {Partition: "demo", Intent: "core", Status: StatusChecking},
			"worker": {Partition: "demo", Intent: "worker", Status: StatusBlocked},
		},
	}

	NormalizePartitionRuntime(runtime)

	if got, want := runtime.PartitionState.Status, "Progressing"; got != want {
		t.Fatalf("partition status = %q, want %q", got, want)
	}
	if got, want := runtime.PartitionState.Metrics.PendingIntents, 2; got != want {
		t.Fatalf("pending intents = %d, want %d", got, want)
	}
	if got := runtime.PartitionState.Metrics.AttentionIntents; got != 0 {
		t.Fatalf("attention intents = %d, want 0", got)
	}
}

func TestNormalizePartitionRuntimeCountsBlockedErrorsAsFailing(t *testing.T) {
	errText := "missing secret reference"
	runtime := &PartitionRuntime{
		Partition: "demo",
		PartitionState: &PartitionState{
			Partition:      "demo",
			IntentVersions: map[string]string{"api": "api-v1"},
		},
		Intents: map[string]*IntentState{
			"api": {Partition: "demo", Intent: "api", Status: StatusBlocked, LastError: &errText},
		},
	}

	NormalizePartitionRuntime(runtime)

	if got, want := runtime.PartitionState.Status, "Failing"; got != want {
		t.Fatalf("partition status = %q, want %q", got, want)
	}
	if got, want := runtime.PartitionState.Metrics.FailingIntents, 1; got != want {
		t.Fatalf("failing intents = %d, want %d", got, want)
	}
	if got, want := runtime.PartitionState.DisplayStatus, "Needs action"; got != want {
		t.Fatalf("display status = %q, want %q", got, want)
	}
}
