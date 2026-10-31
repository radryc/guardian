package runtime

import (
	"context"
	"errors"
	"hash/fnv"
	"log"
	"os"
	"time"

	"github.com/radryc/guardian/internal/buildinfo"
	platformregistry "github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/pkg/guardianapi"
)

// PusherInfo is the metadata a pusher publishes into the platform registry.
type PusherInfo struct {
	Name           string
	Provider       string
	Version        string
	Cluster        string
	KubeContext    string
	Account        string
	Region         string
	ShardIndex     int
	ShardCount     int
	Capabilities   []string
	OTLPEndpoint   string
	CollectorURL   string
	WorkerID       string
	ObservedAPIURL string
	Description    string
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
		existing, existingErr := regStore.GetPusher(ctx, info.Name)
		if existingErr != nil && !errors.Is(existingErr, os.ErrNotExist) {
			log.Printf("[Registry] read pusher registration failed: %v", existingErr)
			return
		}
		if existing == nil && info.ObservedAPIURL == "" && info.Provider != "kubernetes" {
			// AWS pushers normally start before guardianctl registers them with
			// an observed read API URL and token. Do not spam heartbeat errors
			// while waiting for that registration. Kubernetes pushers have no
			// observed read API, so they self-register instead of waiting for a
			// guardianctl registration that may never happen.
			return
		}
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
			ObservedAPIURL: info.ObservedAPIURL,
			Description:    info.Description,
		}
		if existing != nil {
			if record.ObservedAPIURL == "" {
				record.ObservedAPIURL = existing.ObservedAPIURL
			}
			if record.Token == "" {
				record.Token = existing.Token
			}
			if record.TokenEnv == "" {
				record.TokenEnv = existing.TokenEnv
			}
			if record.Description == "" {
				record.Description = existing.Description
			}
			if record.CreatedAt.IsZero() {
				record.CreatedAt = existing.CreatedAt
			}
			if existing.Labels != nil {
				record.Labels = make(map[string]string, len(existing.Labels)+1)
				for key, value := range existing.Labels {
					record.Labels[key] = value
				}
				record.Labels["workerID"] = info.WorkerID
			}
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
