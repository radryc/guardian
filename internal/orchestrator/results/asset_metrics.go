package results

import (
	"slices"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/observability"
)

var assetObservationCurrent = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "guardian",
	Subsystem: "asset",
	Name:      "observation_current",
	Help:      "Latest asset health/readiness observation; value is always 1.",
}, []string{
	"guardian_target_partition",
	"guardian_target_intent",
	"guardian_target_asset",
	"guardian_asset_type",
	"service",
	"status",
	"readiness",
})

var (
	assetObservationMu     sync.Mutex
	assetObservationLabels = map[string][]string{}
)

func recordAssetObservations(result *taskdomain.TaskResult, task *taskdomain.Task) {
	if result == nil {
		return
	}
	assetTypes := make(map[string]string, len(result.AssetObservations))
	if task != nil {
		for _, asset := range task.Assets {
			if asset.Name != "" {
				assetTypes[asset.Name] = asset.Type
			}
		}
	}

	assetObservationMu.Lock()
	defer assetObservationMu.Unlock()
	for asset, observation := range result.AssetObservations {
		if observation == nil {
			continue
		}
		status := "Unknown"
		if observation.Health != nil && observation.Health.Status != "" {
			status = string(observation.Health.Status)
		}
		readiness := "Unknown"
		if observation.ApplyReadiness != nil && observation.ApplyReadiness.Status != "" {
			readiness = string(observation.ApplyReadiness.Status)
		}
		assetType := assetTypes[asset]
		if assetType == "" {
			assetType = "Unknown"
		}
		service := observability.CanonicalServiceIdentity(result.Partition, result.Intent, asset).Name
		labels := []string{result.Partition, result.Intent, asset, assetType, service, status, readiness}
		key := result.Partition + "\x00" + result.Intent + "\x00" + asset + "\x00" + assetType
		if previous, ok := assetObservationLabels[key]; ok && !slices.Equal(previous, labels) {
			// The status/readiness labels change as an asset transitions. Remove
			// the previous series so the metric reflects only the latest
			// observation instead of accumulating one series per historic status.
			assetObservationCurrent.DeleteLabelValues(previous...)
		}
		assetObservationLabels[key] = labels
		assetObservationCurrent.WithLabelValues(labels...).Set(1)
	}
}
