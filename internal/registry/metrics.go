package registry

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

const maxPerPusherMetricSeries = 1000

var (
	pusherInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "guardian",
		Subsystem: "registry",
		Name:      "pusher_info",
		Help:      "Registered pusher metadata; value is always 1 (emitted when fleet <= 1000 pushers).",
	}, []string{"guardian_pusher", "guardian_provider", "guardian_cluster", "guardian_account", "guardian_region", "version", "status"})

	pusherHeartbeatAge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "guardian",
		Subsystem: "registry",
		Name:      "pusher_heartbeat_age_seconds",
		Help:      "Seconds since the last pusher heartbeat (emitted when fleet <= 1000 pushers).",
	}, []string{"guardian_pusher", "guardian_cluster"})

	pushersTotalByDimension = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "guardian",
		Subsystem: "registry",
		Name:      "pushers",
		Help:      "Total registered pushers grouped by provider, region, status, and version (bounded cardinality for 50K+ fleets).",
	}, []string{"guardian_provider", "guardian_region", "status", "version"})

	pusherMaxHeartbeatAgeByRegion = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "guardian",
		Subsystem: "registry",
		Name:      "pusher_max_heartbeat_age_seconds",
		Help:      "Maximum seconds since the last pusher heartbeat per provider and region.",
	}, []string{"guardian_provider", "guardian_region"})
)

// StartMetrics periodically exports registered pusher metadata.
func StartMetrics(ctx context.Context, store guardianapi.Store) {
	if store == nil {
		return
	}
	refresh := func() {
		pushers, err := New(store).ListPushers(ctx)
		if err != nil {
			return
		}
		pusherInfo.Reset()
		pusherHeartbeatAge.Reset()
		pushersTotalByDimension.Reset()
		pusherMaxHeartbeatAgeByRegion.Reset()

		now := time.Now().UTC()
		emitPerPusher := len(pushers) <= maxPerPusherMetricSeries

		type dimKey struct {
			provider, region, status, version string
		}
		type regionKey struct {
			provider, region string
		}
		counts := make(map[dimKey]int)
		maxAges := make(map[regionKey]float64)

		for _, pusher := range pushers {
			provider := pusher.Provider
			if provider == "" {
				provider = pusher.Labels["kind"]
			}
			if provider == "" {
				provider = "unknown"
			}
			region := pusher.Region
			if region == "" {
				region = "global"
			}
			version := pusher.Version
			if version == "" {
				version = "unknown"
			}
			status := effectivePusherStatus(pusher, now)

			counts[dimKey{provider: provider, region: region, status: status, version: version}]++

			age := 0.0
			if !pusher.LastHeartbeat.IsZero() {
				age = now.Sub(pusher.LastHeartbeat).Seconds()
			}
			rk := regionKey{provider: provider, region: region}
			if age > maxAges[rk] {
				maxAges[rk] = age
			}

			if emitPerPusher {
				pusherInfo.WithLabelValues(pusher.Name, provider, pusher.Cluster, pusher.Account, pusher.Region, pusher.Version, status).Set(1)
				pusherHeartbeatAge.WithLabelValues(pusher.Name, pusher.Cluster).Set(age)
			}
		}

		for k, count := range counts {
			pushersTotalByDimension.WithLabelValues(k.provider, k.region, k.status, k.version).Set(float64(count))
		}
		for rk, maxAge := range maxAges {
			pusherMaxHeartbeatAgeByRegion.WithLabelValues(rk.provider, rk.region).Set(maxAge)
		}
	}
	refresh()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()
}
