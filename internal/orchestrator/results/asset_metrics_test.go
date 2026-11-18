package results

import (
	"testing"

	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/metrics"
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
	task := &taskdomain.Task{
		Assets: []taskdomain.AbstractAsset{{Type: "Compute", Name: "query"}},
	}
	recordAssetObservations(result, task)
	result.AssetObservations["query"].Health.Status = taskdomain.HealthHealthy
	recordAssetObservations(result, task)

	statuses := observationStatuses(t, "doctor", "query", "query", "Compute")
	if got := statuses["Healthy"]; got != 1 {
		t.Fatalf("Healthy series = %d, want 1 (all statuses: %#v)", got, statuses)
	}
	if got := statuses["Unhealthy"]; got != 0 {
		t.Fatalf("Unhealthy series = %d, want 0 (all statuses: %#v)", got, statuses)
	}
}

func observationStatuses(t *testing.T, partition, intent, asset, assetType string) map[string]int {
	t.Helper()
	statuses := map[string]int{}
	for _, sample := range metrics.Snapshot("guardian_asset_observation_current") {
		if sample.Labels["guardian_target_partition"] != partition ||
			sample.Labels["guardian_target_intent"] != intent ||
			sample.Labels["guardian_target_asset"] != asset ||
			sample.Labels["guardian_asset_type"] != assetType {
			continue
		}
		statuses[sample.Labels["status"]]++
	}
	return statuses
}
