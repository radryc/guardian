package observed

import (
	"fmt"
	"strings"

	"github.com/radryc/guardian/internal/cdktemplate"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
)

// CompareTemplates diffs the desired CloudFormation template against the live
// observed template at resource granularity.
func CompareTemplates(desiredJSON, observedJSON string) ([]string, error) {
	changed, err := cdktemplate.Compare(desiredJSON, observedJSON)
	if err != nil {
		return nil, fmt.Errorf("compare templates: %w", err)
	}
	return changed, nil
}

// TemplateHash returns a stable content hash for a template.
func TemplateHash(raw string) string {
	return cdktemplate.Hash(raw)
}

// HealthFor maps a CloudFormation stack status to an observed health
// observation and apply readiness.
func HealthFor(stackStatus, driftStatus string, driftedCount int) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness) {
	status := strings.ToUpper(strings.TrimSpace(stackStatus))
	health := &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "stack " + status}
	switch {
	case status == "":
		health = &taskdomain.HealthObservation{Status: taskdomain.HealthDegraded, Summary: "stack state unknown"}
	case strings.HasSuffix(status, "_FAILED"),
		strings.Contains(status, "ROLLBACK_IN_PROGRESS"),
		strings.Contains(status, "ROLLBACK_COMPLETE"),
		strings.Contains(status, "ROLLBACK_FAILED"):
		health = &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "stack status " + status}
	case strings.HasSuffix(status, "_IN_PROGRESS"):
		health = &taskdomain.HealthObservation{Status: taskdomain.HealthDegraded, Summary: "stack status " + status}
	}
	if strings.EqualFold(driftStatus, "DRIFTED") {
		health.Summary = fmt.Sprintf("%s; drift detected on %d resource(s)", health.Summary, driftedCount)
		if health.Status == taskdomain.HealthHealthy {
			health.Status = taskdomain.HealthDegraded
		}
	}
	readiness := &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "report-only intent"}
	return health, readiness
}
