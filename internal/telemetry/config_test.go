package telemetry

import "testing"

func TestLoadConfigParsesExponentialHistograms(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317")
	t.Setenv("GUARDIAN_OTEL_EXPONENTIAL_HISTOGRAMS", " guardian.pusher.task.duration.seconds , guardian.other.duration ")

	cfg, err := LoadConfig("guardian-pusher-k8s", "instance-1")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.ExponentialHistogramInstruments) != 2 {
		t.Fatalf("instruments = %#v", cfg.ExponentialHistogramInstruments)
	}
	if cfg.ExponentialHistogramInstruments[0] != "guardian.pusher.task.duration.seconds" ||
		cfg.ExponentialHistogramInstruments[1] != "guardian.other.duration" {
		t.Fatalf("instruments = %#v", cfg.ExponentialHistogramInstruments)
	}
}
