package runtime

import (
	"context"
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

// StartRegistration writes the pusher heartbeat immediately and then every
// 30 seconds until the context is cancelled.
func StartRegistration(ctx context.Context, store guardianapi.Store, info PusherInfo) {
	if store == nil || info.Name == "" {
		return
	}
	if info.Version == "" {
		info.Version = buildinfo.Current().Version
	}
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
			LastHeartbeat: time.Now().UTC(),
			Labels: map[string]string{
				"workerID": info.WorkerID,
			},
		}
		if err := platformregistry.New(store).PutPusher(ctx, record); err != nil {
			log.Printf("[Registry] pusher heartbeat failed: %v", err)
		}
	}
	write()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
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
