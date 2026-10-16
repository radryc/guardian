package main

import (
	"os"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/config"
)

// applyGuardianEnvConfig applies environment overrides that are not part of
// the config file, so a deployment can add pushers or enable the observed
// collector without rebuilding the mounted config:
//
//	GUARDIAN_PUSHERS            comma list of name:queueDir entries
//	GUARDIAN_OBSERVED_INTERVAL  observed collector interval (empty disables)
//	GUARDIAN_OBSERVED_TIMEOUT   observed collector request timeout
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
