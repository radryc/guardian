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

func TestLoadConfigDefaultsInsecureFromEndpointScheme(t *testing.T) {
	cases := []struct {
		endpoint string
		want     bool
	}{
		{"https://collector.example.com:4317", false},
		{"HTTPS://collector.example.com:4317", false},
		{"http://collector.example.com:4317", true},
		{"collector.example.com:4317", true},
	}
	for _, tc := range cases {
		t.Run(tc.endpoint, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tc.endpoint)
			t.Setenv("GUARDIAN_OTEL_INSECURE", "")
			t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "")
			cfg, err := LoadConfig("guardiand", "instance-1")
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if cfg.Insecure != tc.want {
				t.Fatalf("Insecure = %v, want %v for %q", cfg.Insecure, tc.want, tc.endpoint)
			}
		})
	}
}

func TestLoadConfigHonoursExplicitInsecureOverride(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://collector.example.com:4317")
	t.Setenv("GUARDIAN_OTEL_INSECURE", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "")
	cfg, err := LoadConfig("guardiand", "instance-1")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !cfg.Insecure {
		t.Fatal("explicit GUARDIAN_OTEL_INSECURE override was ignored")
	}
}
