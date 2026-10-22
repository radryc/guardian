package results

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
)

func TestRecordAssetObservationsReplacesHistoricStatus(t *testing.T) {
	assetObservationMu.Lock()
	assetObservationCurrent.Reset()
	assetObservationLabels = map[string][]string{}
	assetObservationMu.Unlock()
	t.Cleanup(func() {
		assetObservationMu.Lock()
		assetObservationCurrent.Reset()
		assetObservationLabels = map[string][]string{}
		assetObservationMu.Unlock()
	})

	result := &taskdomain.TaskResult{
		Partition: "doctor",
		Intent:    "query",
		AssetObservations: map[string]*taskdomain.AssetObservation{
			"query": {
				Health:         &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy},
				ApplyReadiness: &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady},
			},
		},
	}
	recordAssetObservations(result)
	result.AssetObservations["query"].Health.Status = taskdomain.HealthHealthy
	recordAssetObservations(result)

	statuses := observationStatuses(t, "doctor", "query", "query")
	if got := statuses["Healthy"]; got != 1 {
		t.Fatalf("Healthy series = %d, want 1 (all statuses: %#v)", got, statuses)
	}
	if got := statuses["Unhealthy"]; got != 0 {
		t.Fatalf("Unhealthy series = %d, want 0 (all statuses: %#v)", got, statuses)
	}
}

func observationStatuses(t *testing.T, partition, intent, asset string) map[string]int {
	t.Helper()
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	statuses := map[string]int{}
	for _, family := range metricFamilies {
		if family.GetName() != "guardian_asset_observation_current" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["guardian_partition"] != partition || labels["guardian_intent"] != intent || labels["guardian_asset"] != asset {
				continue
			}
			statuses[labels["status"]]++
		}
	}
	return statuses
}
