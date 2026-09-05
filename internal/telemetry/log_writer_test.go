package telemetry

import (
	"context"
	"testing"

	apilog "go.opentelemetry.io/otel/log"
)

func TestStdLogWriterEmitsCompletedLines(t *testing.T) {
	var messages []string
	writer := &stdLogWriter{
		scope: "guardian/test",
		emit: func(_ context.Context, _ string, _ apilog.Severity, message string) {
			messages = append(messages, message)
		},
	}

	if _, err := writer.Write([]byte("first line\nsecond")); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}
	if got := len(messages); got != 1 {
		t.Fatalf("messages after first chunk = %d, want 1", got)
	}
	if _, err := writer.Write([]byte(" line\n")); err != nil {
		t.Fatalf("write second chunk: %v", err)
	}
	if got := len(messages); got != 2 {
		t.Fatalf("messages after second chunk = %d, want 2", got)
	}
	if messages[0] != "first line" {
		t.Fatalf("first message = %q, want %q", messages[0], "first line")
	}
	if messages[1] != "second line" {
		t.Fatalf("second message = %q, want %q", messages[1], "second line")
	}
}

func TestSeverityForLine(t *testing.T) {
	cases := []struct {
		line     string
		severity apilog.Severity
	}{
		{line: "apply succeeded", severity: apilog.SeverityInfo},
		{line: "warn drift detected", severity: apilog.SeverityWarn},
		{line: "operation failed", severity: apilog.SeverityError},
		{line: "fatal: unrecoverable", severity: apilog.SeverityFatal},
	}

	for _, tc := range cases {
		if got := severityForLine(tc.line); got != tc.severity {
			t.Fatalf("severityForLine(%q) = %v, want %v", tc.line, got, tc.severity)
		}
	}
}
