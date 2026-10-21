package main

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/config"
)

// applyGuardianEnvConfig applies environment overrides that are not part of
// the config file, so a deployment can add pushers or enable the observed
// collector without rebuilding the mounted config:
//
//	GUARDIAN_PUSHERS            comma list of name:queueDir entries
//	GUARDIAN_OBSERVED_INTERVAL  observed collector interval (empty disables)
//	GUARDIAN_OBSERVED_TIMEOUT   observed collector request timeout
//	GUARDIAN_OBSERVED_DETECT_DRIFT  run CloudFormation drift detection each pass
//	GUARDIAN_ONBOARD_INTERVAL   customer onboarding controller interval (empty disables)
//	GUARDIAN_REPO_IMPORT_ENABLED    run the repo partition import loop
//	GUARDIAN_REPO_IMPORT_INTERVAL   repo import loop interval
//	GUARDIAN_REPO_IMPORT_SHARDS     number of import shards
//	GUARDIAN_REPO_IMPORT_LEASE      use Kubernetes Lease leader election
//	GUARDIAN_REPO_IMPORT_LEASE_NAME Lease name prefix
//
// It is how the multi-account demo wires aws-<account> queues and the observed
// collector into a guardiand that was started from the default config.
func applyGuardianEnvConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}
	if raw := strings.TrimSpace(os.Getenv("GUARDIAN_PUSHERS")); raw != "" {
		if pushers := parseGuardianPushers(raw); len(pushers) > 0 {
			cfg.Pushers = pushers
		}
	}
	if value := strings.TrimSpace(os.Getenv("GUARDIAN_OBSERVED_INTERVAL")); value != "" {
		cfg.Guardian.ObservedInterval = value
	}
	if value := strings.TrimSpace(os.Getenv("GUARDIAN_OBSERVED_TIMEOUT")); value != "" {
		cfg.Guardian.ObservedTimeout = value
	}
	if value := strings.TrimSpace(os.Getenv("GUARDIAN_OBSERVED_DETECT_DRIFT")); value != "" {
		if enabled, err := strconv.ParseBool(value); err == nil {
			cfg.Guardian.ObservedDetectDrift = enabled
		}
	}
	if value := strings.TrimSpace(os.Getenv("GUARDIAN_ONBOARD_INTERVAL")); value != "" {
		cfg.Guardian.OnboardInterval = value
	}
	if value := strings.TrimSpace(os.Getenv("GUARDIAN_REPO_IMPORT_ENABLED")); value != "" {
		if enabled, err := strconv.ParseBool(value); err == nil {
			cfg.Guardian.RepoImportEnabled = enabled
		}
	}
	if value := strings.TrimSpace(os.Getenv("GUARDIAN_REPO_IMPORT_INTERVAL")); value != "" {
		cfg.Guardian.RepoImportInterval = value
	}
	if value := strings.TrimSpace(os.Getenv("GUARDIAN_REPO_IMPORT_SHARDS")); value != "" {
		if shards, err := strconv.Atoi(value); err == nil && shards > 0 {
			cfg.Guardian.RepoImportShards = shards
		}
	}
	if value := strings.TrimSpace(os.Getenv("GUARDIAN_REPO_IMPORT_LEASE")); value != "" {
		if enabled, err := strconv.ParseBool(value); err == nil {
			cfg.Guardian.RepoImportLease = enabled
		}
	}
	if value := strings.TrimSpace(os.Getenv("GUARDIAN_REPO_IMPORT_LEASE_NAME")); value != "" {
		cfg.Guardian.RepoImportLeaseName = value
	}
}

// parseGuardianPushers parses "name:queueDir,name2:queueDir2". Entries without
// a name or queue dir are skipped.
func parseGuardianPushers(raw string) []config.PusherConfig {
	out := make([]config.PusherConfig, 0, 4)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, queueDir, ok := strings.Cut(entry, ":")
		name = strings.TrimSpace(name)
		queueDir = strings.TrimSpace(queueDir)
		if !ok || name == "" || queueDir == "" {
			continue
		}
		out = append(out, config.PusherConfig{Name: name, QueueDir: queueDir})
	}
	return out
}

// guardianAccessInterval parses GUARDIAN_ACCESS_INTERVAL (Go duration). An
// empty or invalid value returns 0, which makes the access service fall back
// to its default refresh interval.
func guardianAccessInterval() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("GUARDIAN_ACCESS_INTERVAL")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return 0
}
