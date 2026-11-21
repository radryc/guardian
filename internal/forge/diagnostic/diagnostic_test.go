package diagnostic

import (
	"strings"
	"testing"
)

func TestListCounts(t *testing.T) {
	list := List{
		{Severity: SeverityError, Code: "FGE1001"},
		{Severity: SeverityWarning, Code: "FGE7001"},
		{Severity: SeverityWarning, Code: "FGE3010"},
		{Severity: SeverityInfo, Code: "FGE0000"},
	}
	if got := list.Errors(); got != 1 {
		t.Fatalf("Errors() = %d", got)
	}
	if got := list.Warnings(); got != 2 {
		t.Fatalf("Warnings() = %d", got)
	}
	if !list.HasErrors() {
		t.Fatal("HasErrors() = false")
	}
}

func TestDiagnosticErrorString(t *testing.T) {
	d := Diagnostic{Severity: SeverityError, Code: "FGE3003", File: "partitions/x/intents/a.yaml", Message: "missing payload"}
	got := d.Error()
	for _, want := range []string{"error", "FGE3003", "partitions/x/intents/a.yaml", "missing payload"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Error() = %q, missing %q", got, want)
		}
	}
}
