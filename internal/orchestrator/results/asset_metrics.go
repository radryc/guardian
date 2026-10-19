package results

import (
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
}, []string{"partition", "intent", "asset", "service", "status", "readiness"})

func recordAssetObservations(result *taskdomain.TaskResult) {
	if result == nil {
		return
	}
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
		assetObservationCurrent.WithLabelValues(result.Partition, result.Intent, asset, service, status, readiness).Set(1)
	}
}
