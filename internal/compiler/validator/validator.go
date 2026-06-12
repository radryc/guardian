package validator

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/reugn/go-quartz/quartz"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	partitiondomain "github.com/rydzu/ainfra/guardian/internal/domain/partition"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func ValidatePartition(p *partitiondomain.Partition) error {
	if p == nil {
		return fmt.Errorf("partition is nil")
	}
	if !namePattern.MatchString(p.Metadata.Name) {
		return fmt.Errorf("invalid partition name %q", p.Metadata.Name)
	}
	switch p.Spec.DeletionPolicy {
	case "", "orphan", "destroy":
	default:
		return fmt.Errorf("invalid deletionPolicy %q", p.Spec.DeletionPolicy)
	}
	switch p.Spec.Reconciliation.Mode {
	case "", "auto", "manual", "readonly":
	default:
		return fmt.Errorf("invalid reconciliation mode %q", p.Spec.Reconciliation.Mode)
	}
	if p.Spec.Reconciliation.Interval != "" {
		if err := validateInterval(p.Spec.Reconciliation.Interval); err != nil {
			return err
		}
	}
	return nil
}

func ValidateIntent(i *intentdomain.Intent, knownIntents []string, knownPushers []string) error {
	if i == nil {
		return fmt.Errorf("intent is nil")
	}
	if !namePattern.MatchString(i.Metadata.Name) {
		return fmt.Errorf("invalid intent name %q", i.Metadata.Name)
	}
	if i.Spec.IntentType != "" && i.Spec.IntentType != "standard" {
		return fmt.Errorf("unsupported intentType %q", i.Spec.IntentType)
	}

	knownIntentSet := toSet(knownIntents)
	for _, join := range i.Spec.Joins {
		if _, ok := knownIntentSet[join]; !ok {
			return fmt.Errorf("join %q references unknown intent", join)
		}
	}

	if len(knownPushers) > 0 && i.Spec.TargetPusher != "" {
		if _, ok := toSet(knownPushers)[i.Spec.TargetPusher]; !ok {
			return fmt.Errorf("targetPusher %q is not known", i.Spec.TargetPusher)
		}
	}
	if err := validateTargetPlacement(i.Spec.TargetPusher, i.Spec.Target); err != nil {
		return err
	}
	return validateIntentAssets(i.Spec.Assets, i.Spec.Hints)
}

func toSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func validateTargetPlacement(targetPusher string, placement targetdomain.Placement) error {
	name := strings.ToLower(strings.TrimSpace(targetPusher))
	switch {
	case strings.Contains(name, "k8s") || strings.Contains(name, "kubernetes"):
		if strings.TrimSpace(placement.Cluster) == "" {
			return fmt.Errorf("target.cluster is required for pusher %q", targetPusher)
		}
	case strings.Contains(name, "docker"):
		if strings.TrimSpace(placement.Cluster) == "" {
			return fmt.Errorf("target.cluster is required for pusher %q", targetPusher)
		}
	case strings.Contains(name, "aws"):
		if strings.TrimSpace(placement.Region) == "" {
			return fmt.Errorf("target.region is required for pusher %q", targetPusher)
		}
		if strings.TrimSpace(placement.Account) == "" {
			return fmt.Errorf("target.account is required for pusher %q", targetPusher)
		}
	default:
		if strings.TrimSpace(placement.Cluster) == "" &&
			strings.TrimSpace(placement.Namespace) == "" &&
			strings.TrimSpace(placement.Region) == "" &&
			strings.TrimSpace(placement.Account) == "" {
			return fmt.Errorf("target placement is required")
		}
	}
	return nil
}

// validateInterval accepts either a positive Go duration ("10m", "1h30m") or a
// quartz cron expression ("0 0/10 * * * ?").
func validateInterval(s string) error {
	if _, err := quartz.NewCronTrigger(s); err == nil {
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid reconciliation interval %q: must be a Go duration or quartz cron expression", s)
	}
	if d <= 0 {
		return fmt.Errorf("reconciliation interval must be positive, got %q", s)
	}
	return nil
}
