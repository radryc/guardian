package metrics

import "testing"

func TestGaugeSnapshotTracksLatestSeries(t *testing.T) {
	gauge := NewGauge("guardian_test_gauge", "test gauge", "guardian_target_partition", "status")
	gauge.WithLabelValues("demo", "Healthy").Set(1)
	gauge.WithLabelValues("demo", "Unhealthy").Set(1)
	gauge.WithLabelValues("demo", "Unhealthy").Dec()

	gauge.DeleteLabelValues("demo", "Unhealthy")
	samples := Snapshot("guardian_test_gauge")
	if len(samples) != 1 {
		t.Fatalf("samples = %d, want 1: %#v", len(samples), samples)
	}
	if got := samples[0].Labels["guardian_target_partition"]; got != "demo" {
		t.Fatalf("partition label = %q, want demo", got)
	}
	if got := samples[0].Labels["status"]; got != "Healthy" {
		t.Fatalf("status label = %q, want Healthy", got)
	}
	if samples[0].Value != 1 {
		t.Fatalf("value = %v, want 1", samples[0].Value)
	}

	gauge.Reset()
	if got := Snapshot("guardian_test_gauge"); len(got) != 0 {
		t.Fatalf("after Reset samples = %#v, want none", got)
	}
}

func TestCounterIsSafeWithoutProvider(t *testing.T) {
	counter := NewCounter("guardian_test_counter", "test counter", "operation")
	counter.WithLabelValues("write").Add(3)
	counter.Inc()
}
