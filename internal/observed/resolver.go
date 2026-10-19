package observed

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/registry"
)

// EndpointResolver maps a pusher name to its observed read API.
type EndpointResolver interface {
	Resolve(pusher string) (Endpoint, bool)
}

// StaticResolver resolves endpoints from a fixed map (config-defined pushers).
type StaticResolver map[string]Endpoint

// Resolve implements EndpointResolver.
func (r StaticResolver) Resolve(pusher string) (Endpoint, bool) {
	endpoint, ok := r[pusher]
	return endpoint, ok
}

// RegistryResolver resolves endpoints from static config first, then from the
// store-backed pusher registry, refreshed on a TTL. This is how a pusher URL
// added through the UI or CLI becomes usable without restarting guardiand.
type RegistryResolver struct {
	Static   map[string]Endpoint
	Registry *registry.Store
	TTL      time.Duration

	mu        sync.Mutex
	cached    map[string]Endpoint
	fetchedAt time.Time
}

// Resolve implements EndpointResolver.
func (r *RegistryResolver) Resolve(pusher string) (Endpoint, bool) {
	if r == nil {
		return Endpoint{}, false
	}
	if endpoint, ok := r.Static[pusher]; ok {
		return endpoint, true
	}
	r.refresh()
	r.mu.Lock()
	defer r.mu.Unlock()
	endpoint, ok := r.cached[pusher]
	return endpoint, ok
}

func (r *RegistryResolver) refresh() {
	ttl := r.TTL
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	r.mu.Lock()
	fresh := r.cached != nil && time.Since(r.fetchedAt) < ttl
	r.mu.Unlock()
	if fresh {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pushers, err := r.Registry.ListPushers(ctx)
	if err != nil {
		// Keep the previous cache on error; do not stamp fetchedAt so the next
		// Resolve retries.
		return
	}
	endpoints := make(map[string]Endpoint, len(pushers))
	for _, pusher := range pushers {
		// Non-AWS pushers do not have an observed read API. Do not cache an
		// endpoint with an empty URL: callers would otherwise fail later with
		// "observed API URL is not configured".
		if strings.TrimSpace(pusher.ObservedAPIURL) == "" {
			continue
		}
		endpoints[pusher.Name] = Endpoint{
			Name:   pusher.Name,
			Client: NewClient(pusher.ObservedAPIURL, pusher.TokenValue(), 30*time.Second),
		}
	}
	r.mu.Lock()
	r.cached = endpoints
	r.fetchedAt = time.Now()
	r.mu.Unlock()
}
