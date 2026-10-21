package runtime

import (
	"context"
	"hash/fnv"
	"log"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/buildinfo"
	platformregistry "github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

// PusherInfo is the metadata a pusher publishes into the platform registry.
type PusherInfo struct {
	Name         string
	Provider     string
	Version      string
	Cluster      string
	KubeContext  string
	Account      string
	Region       string
	ShardIndex   int
	ShardCount   int
	Capabilities []string
	OTLPEndpoint string
	CollectorURL string
	WorkerID     string
}

func registrationHeartbeatInterval(name string) time.Duration {
	const base = 30 * time.Second
	if name == "" {
		return base
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	// Spread 50,000 pushers across a 30s..39s interval window to avoid
	// synchronized heartbeat storms against MonoFS.
	jitterMs := time.Duration(h.Sum32()%9000) * time.Millisecond
	return base + jitterMs
}

// StartRegistration writes the pusher heartbeat immediately and then every
// ~30 seconds (with deterministic per-pusher jitter) until the context is cancelled.
func StartRegistration(ctx context.Context, store guardianapi.Store, info PusherInfo) {
	if store == nil || info.Name == "" {
		return
	}
	if info.Version == "" {
		info.Version = buildinfo.Current().Version
	}
	var createdAt time.Time
	regStore := platformregistry.New(store)
	write := func() {
		record := &platformregistry.Pusher{
			Name:          info.Name,
			Provider:      info.Provider,
			Version:       info.Version,
			Cluster:       info.Cluster,
			KubeContext:   info.KubeContext,
			Account:       info.Account,
			Region:        info.Region,
			ShardIndex:    info.ShardIndex,
			ShardCount:    info.ShardCount,
			Capabilities:  append([]string(nil), info.Capabilities...),
			OTLPEndpoint:  info.OTLPEndpoint,
			CollectorURL:  info.CollectorURL,
			Status:        "healthy",
			CreatedAt:     createdAt,
			LastHeartbeat: time.Now().UTC(),
			Labels: map[string]string{
				"workerID": info.WorkerID,
			},
		}
		if err := regStore.PutPusher(ctx, record); err != nil {
			log.Printf("[Registry] pusher heartbeat failed: %v", err)
			return
		}
		if createdAt.IsZero() && !record.CreatedAt.IsZero() {
			createdAt = record.CreatedAt
		}
	}
	write()
	interval := registrationHeartbeatInterval(info.Name)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				write()
			}
		}
	}()
}
