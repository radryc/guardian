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
	defaultServiceName    = "guardian"
	defaultMetricInterval = 15 * time.Second
)

type Config struct {
	Endpoint       string
	ServiceName    string
	Component      string
	InstanceID     string
	Insecure       bool
	MetricInterval time.Duration
}

func LoadConfig(component, instanceID string) (Config, error) {
	cfg := Config{
		Endpoint:  strings.TrimSpace(os.Getenv("GUARDIAN_OTEL_ENDPOINT")),
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
	cfg.ServiceName = strings.TrimSpace(os.Getenv("GUARDIAN_OTEL_SERVICE_NAME"))
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultServiceName
	}
	insecure := strings.TrimSpace(os.Getenv("GUARDIAN_OTEL_INSECURE"))
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
	return cfg, nil
}
