package telemetry

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultServiceName    = "guardian-configs.guardian.guardiand"
	defaultMetricInterval = 15 * time.Second
)

type Config struct {
	Endpoint           string
	ServiceName        string
	Component          string
	InstanceID         string
	Insecure           bool
	MetricInterval     time.Duration
	ResourceAttributes map[string]string
	// ExponentialHistogramInstruments selects histogram instruments that the
	// SDK exports as OTLP exponential histograms instead of explicit buckets.
	ExponentialHistogramInstruments []string
}

func LoadConfig(component, instanceID string) (Config, error) {
	cfg := Config{
		Endpoint:  envFirst("OTEL_EXPORTER_OTLP_ENDPOINT", "GUARDIAN_OTEL_ENDPOINT"),
		Component: strings.TrimSpace(component),
	}
	if cfg.Component == "" {
		cfg.Component = filepath.Base(os.Args[0])
	}
	cfg.InstanceID = strings.TrimSpace(instanceID)
	if cfg.InstanceID == "" {
		cfg.InstanceID = cfg.Component
	}
	if cfg.Endpoint == "" {
		return cfg, nil
	}
	cfg.ServiceName = envFirst("OTEL_SERVICE_NAME", "GUARDIAN_OTEL_SERVICE_NAME")
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultServiceName
	}
	insecure := envFirst("OTEL_EXPORTER_OTLP_INSECURE", "GUARDIAN_OTEL_INSECURE")
	if insecure == "" {
		cfg.Insecure = true
	} else {
		value, err := strconv.ParseBool(insecure)
		if err != nil {
			return Config{}, fmt.Errorf("parse GUARDIAN_OTEL_INSECURE: %w", err)
		}
		cfg.Insecure = value
	}
	metricInterval := strings.TrimSpace(os.Getenv("GUARDIAN_OTEL_METRIC_INTERVAL"))
	if metricInterval == "" {
		cfg.MetricInterval = defaultMetricInterval
	} else {
		value, err := time.ParseDuration(metricInterval)
		if err != nil {
			return Config{}, fmt.Errorf("parse GUARDIAN_OTEL_METRIC_INTERVAL: %w", err)
		}
		cfg.MetricInterval = value
	}
	expHistograms := envFirst("GUARDIAN_OTEL_EXPONENTIAL_HISTOGRAMS")
	if expHistograms == "" {
		expHistograms = "guardian.pusher.task.duration.seconds"
	}
	for _, name := range strings.Split(expHistograms, ",") {
		if name = strings.TrimSpace(name); name != "" {
			cfg.ExponentialHistogramInstruments = append(cfg.ExponentialHistogramInstruments, name)
		}
	}
	return cfg, nil
}

func envFirst(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
