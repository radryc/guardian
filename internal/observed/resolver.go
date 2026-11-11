package observed

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/radryc/guardian/internal/registry"
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

	mu          sync.Mutex
	cached      map[string]Endpoint
	fetchedAt   time.Time
	refreshing  bool
	refreshDone chan struct{}
}

// Resolve returns the observed API for a pusher. A cache miss falls back to a
// direct read of that pusher record: the store-wide index can lag behind
// MonoFS when a watch event is missed, and a single record read is cheap.
func (r *RegistryResolver) Resolve(pusher string) (Endpoint, bool) {
	if r == nil {
		return Endpoint{}, false
	}
	if endpoint, ok := r.Static[pusher]; ok {
		return endpoint, true
	}
	if r.Registry == nil {
		return Endpoint{}, false
	}
	r.refresh()
	r.mu.Lock()
	endpoint, ok := r.cached[pusher]
	r.mu.Unlock()
	if ok {
		return endpoint, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	record, err := r.Registry.GetPusher(ctx, pusher)
	if err != nil || record == nil || strings.TrimSpace(record.ObservedAPIURL) == "" {
		return Endpoint{}, false
	}
	endpoint = Endpoint{
		Name:     record.Name,
		Provider: observedProvider(record.Provider, record.Name),
		Client:   NewClient(record.ObservedAPIURL, record.TokenValue(), 30*time.Second),
	}
	endpoint.Client.Provider = endpoint.Provider
	r.mu.Lock()
	if r.cached == nil {
		r.cached = make(map[string]Endpoint)
	}
	r.cached[pusher] = endpoint
	r.mu.Unlock()
	return endpoint, true
}

func (r *RegistryResolver) refresh() {
	ttl := r.TTL
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	r.mu.Lock()
	if r.cached != nil && time.Since(r.fetchedAt) < ttl {
		r.mu.Unlock()
		return
	}
	if r.refreshing {
		done := r.refreshDone
		r.mu.Unlock()
		<-done
		return
	}
	r.refreshing = true
	r.refreshDone = make(chan struct{})
	done := r.refreshDone
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.refreshing = false
		r.mu.Unlock()
		close(done)
	}()

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
		// Pushers without an observed read API are not cached: callers would
		// otherwise fail later with "observed API URL is not configured".
		if strings.TrimSpace(pusher.ObservedAPIURL) == "" {
			continue
		}
		endpoints[pusher.Name] = Endpoint{
			Name:     pusher.Name,
			Provider: observedProvider(pusher.Provider, pusher.Name),
			Client:   NewClient(pusher.ObservedAPIURL, pusher.TokenValue(), 30*time.Second),
		}
		endpoints[pusher.Name].Client.Provider = endpoints[pusher.Name].Provider
	}
	r.mu.Lock()
	r.cached = endpoints
	r.fetchedAt = time.Now()
	r.mu.Unlock()
}

// ProviderForName normalizes the provider recorded on a pusher. Older config
// entries may omit it, so infer from the conventional pusher name prefix.
func ProviderForName(provider, name string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "" {
		return provider
	}
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(name, "gcp-"):
		return "gcp"
	case strings.HasPrefix(name, "k8s-"), strings.HasPrefix(name, "kubernetes-"):
		return "kubernetes"
	default:
		return "aws"
	}
}

func observedProvider(provider, name string) string {
	return ProviderForName(provider, name)
}
