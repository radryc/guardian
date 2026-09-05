package revisions

import "testing"

func TestPartitionRevisionDeterministic(t *testing.T) {
	first := PartitionRevision("cfg1", map[string]string{
		"workers": "v-workers",
		"core":    "v-core",
	})
	second := PartitionRevision("cfg1", map[string]string{
		"core":    "v-core",
		"workers": "v-workers",
	})
	if first != second {
		t.Fatalf("PartitionRevision mismatch: %q vs %q", first, second)
	}
}
