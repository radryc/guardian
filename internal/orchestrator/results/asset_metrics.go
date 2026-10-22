package results

import (
	"slices"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/observability"
)

var assetObservationCurrent = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "guardian",
	Subsystem: "asset",
	Name:      "observation_current",
	Help:      "Latest asset health/readiness observation; value is always 1.",
}, []string{"guardian_partition", "guardian_intent", "guardian_asset", "service", "status", "readiness"})

var (
	assetObservationMu     sync.Mutex
	assetObservationLabels = map[string][]string{}
)

func recordAssetObservations(result *taskdomain.TaskResult) {
	if result == nil {
		return
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
		service := observability.CanonicalServiceIdentity(result.Partition, result.Intent, asset).Name
		labels := []string{result.Partition, result.Intent, asset, service, status, readiness}
		key := result.Partition + "\x00" + result.Intent + "\x00" + asset
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
