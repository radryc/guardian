// Package probes implements pusher-local health probes. Probe results are
// attributed to the target service/binary and exported as ordinary OTLP
// telemetry, with a latest-state snapshot in MonoFS.
package probes

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type Config struct {
	Enabled           bool
	Pusher            string
	Cluster           string
	Provider          string
	Account           string
	Region            string
	WorkerID          string
	ShardIndex        int
	ShardCount        int
	Interval          time.Duration
	DiscoveryInterval time.Duration
	Timeout           time.Duration
	Retries           int
	Concurrency       int
	MaxTargets        int
	AllowExternal     bool
	HTTPPaths         []string
	TCPPorts          []int
	OTLPEndpoint      string
	Store             guardianapi.Store
}

type Target struct {
	ID           string
	Name         string
	Service      string
	FullName     string
	Partition    string
	Intent       string
	Asset        string
	Mode         string
	Type         string
	URL          string
	Address      string
	Port         int
	Path         string
	Method       string
	Headers      map[string]string
	ExpectStatus []int
	ExpectBody   string
	ExpectIPs    []string
	Interval     time.Duration
	Timeout      time.Duration
	Retries      int
	Insecure     bool
	Labels       map[string]string
}

type Result struct {
	ProbeID             string            `json:"probeID"`
	Service             string            `json:"service"`
	FullName            string            `json:"fullName,omitempty"`
	Pusher              string            `json:"pusher"`
	Cluster             string            `json:"cluster,omitempty"`
	Partition           string            `json:"partition"`
	Intent              string            `json:"intent"`
	Asset               string            `json:"asset"`
	Name                string            `json:"name"`
	Type                string            `json:"type"`
	Mode                string            `json:"mode"`
	Target              string            `json:"target"`
	Status              string            `json:"status"`
	Success             bool              `json:"success"`
	DurationMs          int64             `json:"durationMs"`
	StatusCode          int               `json:"statusCode,omitempty"`
	Error               string            `json:"error,omitempty"`
	CheckedAt           time.Time         `json:"checkedAt"`
	LastSuccessAt       time.Time         `json:"lastSuccessAt,omitempty"`
	LastFailureAt       time.Time         `json:"lastFailureAt,omitempty"`
	ConsecutiveFailures int               `json:"consecutiveFailures"`
	Path                string            `json:"path,omitempty"`
	Labels              map[string]string `json:"labels,omitempty"`
}

type Snapshot struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Pusher     string    `json:"pusher"`
	Cluster    string    `json:"cluster,omitempty"`
	Partition  string    `json:"partition"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Probes     []Result  `json:"probes"`
}

func ProbeID(pusher, partition, intent, asset, name, probeType, target string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{pusher, partition, intent, asset, name, probeType, target}, "|")))
	return hex.EncodeToString(sum[:8])
}

func ConfigFromEnv() Config {
	cfg := Config{
		Enabled:           envBool("GUARDIAN_PROBES_ENABLED", true),
		Interval:          envDuration("GUARDIAN_PROBE_INTERVAL", 30*time.Second),
		DiscoveryInterval: envDuration("GUARDIAN_PROBE_DISCOVERY_INTERVAL", 60*time.Second),
		Timeout:           envDuration("GUARDIAN_PROBE_TIMEOUT", 3*time.Second),
		Retries:           envInt("GUARDIAN_PROBE_RETRIES", 2),
		Concurrency:       envInt("GUARDIAN_PROBE_CONCURRENCY", 8),
		MaxTargets:        envInt("GUARDIAN_PROBE_MAX_TARGETS", 200),
		AllowExternal:     envBool("GUARDIAN_PROBE_ALLOW_EXTERNAL", false),
		HTTPPaths:         envList("GUARDIAN_PROBE_HTTP_PATHS", []string{"/healthz", "/health", "/ready", "/livez", "/"}),
		TCPPorts:          envIntList("GUARDIAN_PROBE_TCP_PORTS", []int{80, 443, 8080}),
		OTLPEndpoint:      strings.TrimSpace(os.Getenv("GUARDIAN_OTEL_HTTP_ENDPOINT")),
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.DiscoveryInterval <= 0 {
		cfg.DiscoveryInterval = 60 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.MaxTargets < 1 {
		cfg.MaxTargets = 200
	}
	return cfg
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envList(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func envIntList(key string, fallback []int) []int {
	values := envList(key, nil)
	if len(values) == 0 {
		return fallback
	}
	out := make([]int, 0, len(values))
	for _, value := range values {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			out = append(out, parsed)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
