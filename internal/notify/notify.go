// Package notify delivers partition alerts to external channels.
package notify

import (
	"context"
	"time"
)

// Severity ranks an alert.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Rank returns a comparable rank for a severity.
func Rank(severity Severity) int {
	switch severity {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// Alert is a partition-level notification.
type Alert struct {
	Event      string            `json:"event"`
	Partition  string            `json:"partition"`
	Status     string            `json:"status"`
	Severity   Severity          `json:"severity"`
	Summary    string            `json:"summary"`
	Details    map[string]string `json:"details,omitempty"`
	FiredAt    time.Time         `json:"firedAt"`
	ResolvedAt *time.Time        `json:"resolvedAt,omitempty"`
}

// Notifier delivers an alert to one channel.
type Notifier interface {
	Name() string
	Send(ctx context.Context, alert Alert) error
}
