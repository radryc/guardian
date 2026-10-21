package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/observability"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/probes"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type uiProjectionCache struct {
	store   guardianapi.Store
	ttl     time.Duration
	watchCh <-chan guardianapi.ChangeEvent

	mu                  sync.RWMutex
	identities          []observability.IdentityRecord
	identitiesValid     bool
	identitiesExpiresAt time.Time
	probes              []probes.Result
	probesValid         bool
	probesExpiresAt     time.Time
}

func newUIProjectionCache(store guardianapi.Store, ttl time.Duration) *uiProjectionCache {
	if store == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = defaultUIDataCacheTTL
	}
	c := &uiProjectionCache{
		store: store,
		ttl:   ttl,
	}
	if ch, err := store.Watch(context.Background(), []string{paths.PartitionsRoot(), paths.RegistryRoot()}); err == nil && ch != nil {
		c.watchCh = ch
		go c.consumeWatch(ch)
	}
	return c
}

func (c *uiProjectionCache) consumeWatch(ch <-chan guardianapi.ChangeEvent) {
	for ev := range ch {
		c.applyWatchEvent(ev)
	}
}

func (c *uiProjectionCache) drainPendingWatchEvents() {
	if c == nil || c.watchCh == nil {
		return
	}
	for {
		select {
		case ev, ok := <-c.watchCh:
			if !ok {
				return
			}
			c.applyWatchEvent(ev)
		default:
			return
		}
	}
}

func (c *uiProjectionCache) applyWatchEvent(ev guardianapi.ChangeEvent) {
	p := ev.LogicalPath
	c.mu.Lock()
	if strings.HasPrefix(p, paths.ProbesRoot()) {
		c.probesValid = false
	}
	if strings.HasPrefix(p, paths.PartitionsRoot()) || strings.HasPrefix(p, paths.PushersRoot()) {
		c.identitiesValid = false
	}
	c.mu.Unlock()
}

func (c *uiProjectionCache) getIdentities(ctx context.Context) ([]observability.IdentityRecord, error) {
	if c == nil {
		return nil, fmt.Errorf("projection cache not initialized")
	}
	c.drainPendingWatchEvents()
	now := time.Now()
	c.mu.RLock()
	if c.identitiesValid && now.Before(c.identitiesExpiresAt) {
		out := append([]observability.IdentityRecord(nil), c.identities...)
		c.mu.RUnlock()
		return out, nil
	}
	c.mu.RUnlock()

	records, err := observability.Identities(ctx, c.store)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.identities = append([]observability.IdentityRecord(nil), records...)
	c.identitiesValid = true
	c.identitiesExpiresAt = time.Now().Add(c.ttl)
	c.mu.Unlock()
	return records, nil
}

func (c *uiProjectionCache) getAllProbes(ctx context.Context) ([]probes.Result, error) {
	if c == nil {
		return nil, fmt.Errorf("projection cache not initialized")
	}
	c.drainPendingWatchEvents()
	now := time.Now()
	c.mu.RLock()
	if c.probesValid && now.Before(c.probesExpiresAt) {
		out := append([]probes.Result(nil), c.probes...)
		c.mu.RUnlock()
		return out, nil
	}
	c.mu.RUnlock()

	entries, err := c.store.ListDir(ctx, paths.ProbesRoot())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	all := make([]probes.Result, 0)
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		snapshots, err := c.store.ListDir(ctx, paths.ProbesDir(entry.Name))
		if err != nil {
			continue
		}
		for _, snapshotEntry := range snapshots {
			if snapshotEntry.IsDir || !strings.HasSuffix(snapshotEntry.Name, ".json") {
				continue
			}
			partition := strings.TrimSuffix(snapshotEntry.Name, ".json")
			raw, err := c.store.ReadFile(ctx, paths.ProbeSnapshot(entry.Name, partition))
			if err != nil {
				continue
			}
			var snapshot probes.Snapshot
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				continue
			}
			for _, result := range snapshot.Probes {
				if result.Pusher == "" {
					result.Pusher = entry.Name
				}
				if result.Partition == "" {
					result.Partition = partition
				}
				all = append(all, result)
			}
		}
	}
	probes.SortResults(all)

	c.mu.Lock()
	c.probes = append([]probes.Result(nil), all...)
	c.probesValid = true
	c.probesExpiresAt = time.Now().Add(c.ttl)
	c.mu.Unlock()
	return all, nil
}

func writeJSONWithETag(w http.ResponseWriter, r *http.Request, status int, value any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		writeJSON(w, status, value)
		return
	}
	payload := buf.Bytes()
	h := fnv.New64a()
	_, _ = h.Write(payload)
	etag := fmt.Sprintf(`"%016x"`, h.Sum64())
	w.Header().Set("ETag", etag)
	if r != nil && status == http.StatusOK && strings.TrimSpace(r.Header.Get("If-None-Match")) == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func (s *Server) handleObservabilityIdentities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	var (
		records []observability.IdentityRecord
		err     error
	)
	if s.projectionCache != nil {
		records, err = s.projectionCache.getIdentities(r.Context())
	} else {
		records, err = observability.Identities(r.Context(), s.store)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONWithETag(w, r, http.StatusOK, map[string]any{"identities": records})
}
