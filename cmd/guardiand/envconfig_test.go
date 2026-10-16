package main

import (
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/config"
)

func TestParseGuardianPushers(t *testing.T) {
	pushers := parseGuardianPushers("k8s-main:/.queues/k8s-main, aws-111:/.queues/aws-111 ,bad,aws-222:")
	if len(pushers) != 2 {
		t.Fatalf("pushers = %+v, want 2 valid entries", pushers)
	}
	if pushers[0].Name != "k8s-main" || pushers[0].QueueDir != "/.queues/k8s-main" {
		t.Fatalf("pusher[0] = %+v", pushers[0])
	}
	if pushers[1].Name != "aws-111" || pushers[1].QueueDir != "/.queues/aws-111" {
		t.Fatalf("pusher[1] = %+v", pushers[1])
	}
}

func TestApplyGuardianEnvConfigPushers(t *testing.T) {
	t.Setenv("GUARDIAN_PUSHERS", "aws-111:/.queues/aws-111,aws-222:/.queues/aws-222")
	t.Setenv("GUARDIAN_OBSERVED_INTERVAL", "1m")
	t.Setenv("GUARDIAN_OBSERVED_TIMEOUT", "15s")

	cfg := config.Default()
	applyGuardianEnvConfig(cfg)

	if len(cfg.Pushers) != 2 {
		t.Fatalf("pushers = %+v", cfg.Pushers)
	}
	if cfg.Pushers[1].Name != "aws-222" || cfg.Pushers[1].QueueDir != "/.queues/aws-222" {
		t.Fatalf("pusher[1] = %+v", cfg.Pushers[1])
	}
	if cfg.Guardian.ObservedInterval != "1m" {
		t.Fatalf("observed interval = %q", cfg.Guardian.ObservedInterval)
	}
	if cfg.Guardian.ObservedTimeout != "15s" {
		t.Fatalf("observed timeout = %q", cfg.Guardian.ObservedTimeout)
	}
}

func TestApplyGuardianEnvConfigDefaultsUntouched(t *testing.T) {
	t.Setenv("GUARDIAN_PUSHERS", "")
	t.Setenv("GUARDIAN_OBSERVED_INTERVAL", "")
	t.Setenv("GUARDIAN_OBSERVED_TIMEOUT", "")

	cfg := config.Default()
	defaultPushers := len(cfg.Pushers)
	defaultInterval := cfg.Guardian.ObservedInterval
	applyGuardianEnvConfig(cfg)

	if len(cfg.Pushers) != defaultPushers {
		t.Fatalf("pushers changed: %+v", cfg.Pushers)
	}
	if cfg.Guardian.ObservedInterval != defaultInterval {
		t.Fatalf("observed interval changed: %q", cfg.Guardian.ObservedInterval)
	}
}

func TestApplyGuardianEnvConfigInvalidPushersIgnored(t *testing.T) {
	t.Setenv("GUARDIAN_PUSHERS", ",,bad,")
	t.Setenv("GUARDIAN_OBSERVED_INTERVAL", "")
	t.Setenv("GUARDIAN_OBSERVED_TIMEOUT", "")

	cfg := config.Default()
	defaultPushers := len(cfg.Pushers)
	applyGuardianEnvConfig(cfg)
	if len(cfg.Pushers) != defaultPushers {
		t.Fatalf("invalid pushers must be ignored: %+v", cfg.Pushers)
	}
}

func TestApplyGuardianEnvConfigNilSafe(t *testing.T) {
	applyGuardianEnvConfig(nil)
}
