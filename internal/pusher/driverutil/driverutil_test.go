package driverutil

import "testing"

func TestLimitLabelValueTruncatesOverlongValues(t *testing.T) {
	value := "1234567890123456789012345678901234567890123456789012345678901234"
	got := limitLabelValue(value)
	if len(got) != 63 {
		t.Fatalf("limitLabelValue() len = %d, want 63", len(got))
	}
	if got != value[:63] {
		t.Fatalf("limitLabelValue() = %q, want %q", got, value[:63])
	}
}

func TestLimitLabelValueKeepsShortValues(t *testing.T) {
	value := "short-value"
	if got := limitLabelValue(value); got != value {
		t.Fatalf("limitLabelValue() = %q, want %q", got, value)
	}
}
