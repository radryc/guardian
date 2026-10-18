package registry

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

var (
	pusherInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "guardian",
		Subsystem: "registry",
		Name:      "pusher_info",
		Help:      "Registered pusher metadata; value is always 1.",
	}, []string{"pusher", "provider", "cluster", "account", "region", "version", "status"})

	pusherHeartbeatAge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "guardian",
		Subsystem: "registry",
		Name:      "pusher_heartbeat_age_seconds",
		Help:      "Seconds since the last pusher heartbeat.",
	}, []string{"pusher", "cluster"})
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
		now := time.Now().UTC()
		for _, pusher := range pushers {
			provider := pusher.Provider
			if provider == "" {
				provider = pusher.Labels["kind"]
			}
			status := pusher.Status
			if status == "" {
				status = "unknown"
			}
			pusherInfo.WithLabelValues(pusher.Name, provider, pusher.Cluster, pusher.Account, pusher.Region, pusher.Version, status).Set(1)
			age := 0.0
			if !pusher.LastHeartbeat.IsZero() {
				age = now.Sub(pusher.LastHeartbeat).Seconds()
			}
			pusherHeartbeatAge.WithLabelValues(pusher.Name, pusher.Cluster).Set(age)
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
