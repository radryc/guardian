package registry

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/pkg/guardianapi"
)

const (
	pusherBulkReadWorkers  = 32
	pusherCacheFallbackTTL = 10 * time.Second
	// A healthy watch stream keeps the index current. The max age is a safety
	// net for a missed or silently dead watch: small fleets refresh quickly,
	// while very large fleets avoid a full 50k-record read too often.
	pusherCacheSmallFleetThreshold = 1000
	pusherCacheSmallFleetMaxAge    = 30 * time.Second
	pusherCacheLargeFleetMaxAge    = 15 * time.Minute
	staleHeartbeatWindow           = 90 * time.Second
)

// PusherListOptions configures server-side filtering, delta sync, and cursor
// pagination over the pusher registry (designed for 50,000+ AWS pushers).
type PusherListOptions struct {
	Provider      string
	Region        string
	Account       string
	Cluster       string
	Status        string
	Version       string
	Query         string
	UpdatedSince  time.Time
	OutliersFirst bool
	Limit         int
	Cursor        string
}

// PusherListPage is a paginated, filtered slice of pushers.
type PusherListPage struct {
	Pushers    []Pusher `json:"pushers"`
	Total      int      `json:"total"`
	Matched    int      `json:"matched"`
	NextCursor string   `json:"nextCursor,omitempty"`
	HasMore    bool     `json:"hasMore"`
}

// PusherFleetSummary provides O(1)/sub-millisecond fleet-wide rollups across
// 50,000+ pushers grouped by region, account, provider, status, and version.
type PusherFleetSummary struct {
	Total           int            `json:"total"`
	Healthy         int            `json:"healthy"`
	Degraded        int            `json:"degraded"`
	Offline         int            `json:"offline"`
	StaleHeartbeats int            `json:"staleHeartbeats"`
	ByStatus        map[string]int `json:"byStatus"`
	ByProvider      map[string]int `json:"byProvider"`
	ByRegion        map[string]int `json:"byRegion"`
	ByAccount       map[string]int `json:"byAccount"`
	ByVersion       map[string]int `json:"byVersion"`
}

type pusherIndexCache struct {
	mu         sync.RWMutex
	byName     map[string]Pusher
	hydratedAt time.Time
	watching   bool
	watchStart sync.Once
}

var (
	pusherIndexesMu sync.Mutex
	pusherIndexes   = make(map[guardianapi.Store]*pusherIndexCache)
)

func getPusherIndex(store guardianapi.Store) *pusherIndexCache {
	if store == nil {
		return nil
	}
	// Only use map keys for comparable store implementations.
	defer func() { _ = recover() }()
	pusherIndexesMu.Lock()
	defer pusherIndexesMu.Unlock()
	idx, ok := pusherIndexes[store]
	if !ok {
		idx = &pusherIndexCache{
			byName: make(map[string]Pusher),
		}
		pusherIndexes[store] = idx
	}
	return idx
}

func (idx *pusherIndexCache) get(name string) (Pusher, bool) {
	if idx == nil {
		return Pusher{}, false
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	p, ok := idx.byName[name]
	if !ok {
		return Pusher{}, false
	}
	return clonePusher(p), true
}

func (idx *pusherIndexCache) put(p Pusher) {
	if idx == nil || p.Name == "" {
		return
	}
	idx.mu.Lock()
	idx.byName[p.Name] = clonePusher(p)
	idx.mu.Unlock()
}

func (idx *pusherIndexCache) remove(name string) {
	if idx == nil || name == "" {
		return
	}
	idx.mu.Lock()
	delete(idx.byName, name)
	idx.mu.Unlock()
}

func (idx *pusherIndexCache) snapshotIfFresh(now time.Time) ([]Pusher, bool) {
	if idx == nil {
		return nil, false
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if idx.hydratedAt.IsZero() {
		return nil, false
	}
	if !idx.watching {
		if now.Sub(idx.hydratedAt) > pusherCacheFallbackTTL {
			return nil, false
		}
	} else {
		maxAge := pusherCacheLargeFleetMaxAge
		if len(idx.byName) < pusherCacheSmallFleetThreshold {
			maxAge = pusherCacheSmallFleetMaxAge
		}
		if now.Sub(idx.hydratedAt) > maxAge {
			return nil, false
		}
	}
	out := make([]Pusher, 0, len(idx.byName))
	for _, p := range idx.byName {
		out = append(out, clonePusher(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, true
}

func (idx *pusherIndexCache) ensureWatching(s *Store) {
	if idx == nil || s == nil || s.store == nil {
		return
	}
	ws, ok := s.store.(guardianapi.WatchStore)
	if !ok {
		return
	}
	idx.watchStart.Do(func() {
		ch, err := ws.Watch(context.Background(), []string{paths.PushersRoot()})
		if err != nil || ch == nil {
			return
		}
		idx.mu.Lock()
		idx.watching = true
		idx.mu.Unlock()
		go func() {
			for ev := range ch {
				rel := strings.TrimPrefix(ev.LogicalPath, paths.PushersRoot()+"/")
				if !strings.HasSuffix(rel, ".json") || strings.Contains(rel, "/") {
					continue
				}
				name := strings.TrimSuffix(rel, ".json")
				if ev.Type == guardianapi.ChangeDeleted {
					idx.remove(name)
					continue
				}
				if p, err := s.loadPusherDirect(context.Background(), name); err == nil && p != nil {
					idx.put(*p)
				}
			}
			idx.mu.Lock()
			idx.watching = false
			idx.hydratedAt = time.Time{}
			idx.mu.Unlock()
		}()
	})
}

func clonePusher(p Pusher) Pusher {
	out := p
	if len(p.Capabilities) > 0 {
		out.Capabilities = append([]string(nil), p.Capabilities...)
	}
	if len(p.Labels) > 0 {
		out.Labels = make(map[string]string, len(p.Labels))
		for k, v := range p.Labels {
			out.Labels[k] = v
		}
	}
	return out
}

// Store persists pusher and repo registrations.
type Store struct {
	store guardianapi.Store
	now   func() time.Time
}

// New builds a registry store.
func New(store guardianapi.Store) *Store {
	return &Store{store: store, now: func() time.Time { return time.Now().UTC() }}
}

// ListPushers returns all registered pushers sorted by name.
// At 50,000+ pushers, it serves from the watch-projected in-memory index and
// hydrates uncached records using a bounded concurrent worker pool instead of
// 50,000 sequential ReadFile RPCs.
func (s *Store) ListPushers(ctx context.Context) ([]Pusher, error) {
	idx := getPusherIndex(s.store)
	idx.ensureWatching(s)
	now := s.now()
	if cached, ok := idx.snapshotIfFresh(now); ok {
		return cached, nil
	}

	entries, err := s.store.ListDir(ctx, paths.PushersRoot())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if idx != nil {
				idx.mu.Lock()
				idx.byName = make(map[string]Pusher)
				idx.hydratedAt = now
				idx.mu.Unlock()
			}
			return nil, nil
		}
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name, ".json"))
	}

	out := make([]Pusher, 0, len(names))
	if len(names) <= 4 {
		for _, name := range names {
			pusher, err := s.loadPusherDirect(ctx, name)
			if err != nil {
				continue
			}
			out = append(out, *pusher)
		}
	} else {
		workers := pusherBulkReadWorkers
		if workers > len(names) {
			workers = len(names)
		}
		nameCh := make(chan string, len(names))
		for _, n := range names {
			nameCh <- n
		}
		close(nameCh)

		var (
			mu sync.Mutex
			wg sync.WaitGroup
		)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				local := make([]Pusher, 0, (len(names)/workers)+1)
				for name := range nameCh {
					if ctx.Err() != nil {
						return
					}
					pusher, err := s.loadPusherDirect(ctx, name)
					if err != nil || pusher == nil {
						continue
					}
					local = append(local, *pusher)
				}
				if len(local) > 0 {
					mu.Lock()
					out = append(out, local...)
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if idx != nil {
		idx.mu.Lock()
		newMap := make(map[string]Pusher, len(out))
		for _, p := range out {
			newMap[p.Name] = clonePusher(p)
		}
		idx.byName = newMap
		idx.hydratedAt = now
		idx.mu.Unlock()
	}
	return out, nil
}

// ListPushersPage returns a filtered, optionally outlier-sorted, cursor-paginated
// slice of pushers so UI and federation callers never have to transfer all 50,000
// pusher records at once.
func (s *Store) ListPushersPage(ctx context.Context, opts PusherListOptions) (PusherListPage, error) {
	all, err := s.ListPushers(ctx)
	if err != nil {
		return PusherListPage{}, err
	}
	return FilterAndPaginatePushers(all, opts, s.now()), nil
}

// SummarizePushers computes fleet-wide counts across all registered pushers.
func (s *Store) SummarizePushers(ctx context.Context) (PusherFleetSummary, error) {
	all, err := s.ListPushers(ctx)
	if err != nil {
		return PusherFleetSummary{}, err
	}
	return ComputePusherFleetSummary(all, s.now()), nil
}

// ComputePusherFleetSummary aggregates pusher counts by status, provider, region,
// account, and version.
func ComputePusherFleetSummary(pushers []Pusher, now time.Time) PusherFleetSummary {
	summary := PusherFleetSummary{
		Total:      len(pushers),
		ByStatus:   make(map[string]int),
		ByProvider: make(map[string]int),
		ByRegion:   make(map[string]int),
		ByAccount:  make(map[string]int),
		ByVersion:  make(map[string]int),
	}
	for _, p := range pushers {
		status := effectivePusherStatus(p, now)
		summary.ByStatus[status]++
		switch status {
		case "healthy":
			summary.Healthy++
		case "stale", "degraded", "warning":
			summary.Degraded++
		default:
			summary.Offline++
		}
		if !p.LastHeartbeat.IsZero() && now.Sub(p.LastHeartbeat) > staleHeartbeatWindow {
			summary.StaleHeartbeats++
		}
		provider := strings.TrimSpace(p.Provider)
		if provider == "" {
			provider = strings.TrimSpace(p.Labels["kind"])
		}
		if provider == "" {
			provider = "unknown"
		}
		summary.ByProvider[provider]++
		if r := strings.TrimSpace(p.Region); r != "" {
			summary.ByRegion[r]++
		}
		if a := strings.TrimSpace(p.Account); a != "" {
			summary.ByAccount[a]++
		}
		if v := strings.TrimSpace(p.Version); v != "" {
			summary.ByVersion[v]++
		}
	}
	return summary
}

func effectivePusherStatus(p Pusher, now time.Time) string {
	status := strings.ToLower(strings.TrimSpace(p.Status))
	if status == "" {
		status = "healthy"
	}
	if status == "healthy" && !p.LastHeartbeat.IsZero() && now.Sub(p.LastHeartbeat) > staleHeartbeatWindow {
		return "stale"
	}
	return status
}

func pusherSeverityRank(p Pusher, now time.Time) int {
	switch effectivePusherStatus(p, now) {
	case "offline", "error", "failed", "unhealthy":
		return 0
	case "stale", "degraded", "warning":
		return 1
	default:
		return 2
	}
}

// FilterAndPaginatePushers applies server-side filters, delta-sync timestamp
// filtering, outlier-first sorting, and cursor pagination.
func FilterAndPaginatePushers(all []Pusher, opts PusherListOptions, now time.Time) PusherListPage {
	provider := strings.ToLower(strings.TrimSpace(opts.Provider))
	region := strings.ToLower(strings.TrimSpace(opts.Region))
	account := strings.ToLower(strings.TrimSpace(opts.Account))
	cluster := strings.ToLower(strings.TrimSpace(opts.Cluster))
	status := strings.ToLower(strings.TrimSpace(opts.Status))
	version := strings.ToLower(strings.TrimSpace(opts.Version))
	query := strings.ToLower(strings.TrimSpace(opts.Query))

	matched := make([]Pusher, 0, len(all))
	for _, p := range all {
		if !opts.UpdatedSince.IsZero() && !p.UpdatedAt.After(opts.UpdatedSince) && !p.LastHeartbeat.After(opts.UpdatedSince) {
			continue
		}
		pProv := strings.ToLower(strings.TrimSpace(p.Provider))
		if pProv == "" {
			pProv = strings.ToLower(strings.TrimSpace(p.Labels["kind"]))
		}
		if provider != "" && pProv != provider {
			continue
		}
		if region != "" && strings.ToLower(strings.TrimSpace(p.Region)) != region {
			continue
		}
		if account != "" && strings.ToLower(strings.TrimSpace(p.Account)) != account {
			continue
		}
		if cluster != "" && strings.ToLower(strings.TrimSpace(p.Cluster)) != cluster {
			continue
		}
		if version != "" && strings.ToLower(strings.TrimSpace(p.Version)) != version {
			continue
		}
		effStatus := effectivePusherStatus(p, now)
		if status != "" {
			if status == "unhealthy" || status == "outlier" {
				if effStatus == "healthy" {
					continue
				}
			} else if effStatus != status && strings.ToLower(strings.TrimSpace(p.Status)) != status {
				continue
			}
		}
		if query != "" {
			haystack := strings.ToLower(p.Name + " " + p.Region + " " + p.Account + " " + p.Cluster + " " + p.Provider + " " + p.Version)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		matched = append(matched, p)
	}

	if opts.OutliersFirst {
		sort.SliceStable(matched, func(i, j int) bool {
			ri, rj := pusherSeverityRank(matched[i], now), pusherSeverityRank(matched[j], now)
			if ri != rj {
				return ri < rj
			}
			return matched[i].Name < matched[j].Name
		})
	}

	offset := 0
	if c := strings.TrimSpace(opts.Cursor); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n >= 0 {
			offset = n
		}
	}
	if offset >= len(matched) {
		return PusherListPage{
			Pushers: []Pusher{},
			Total:   len(all),
			Matched: len(matched),
		}
	}

	end := len(matched)
	if opts.Limit > 0 && offset+opts.Limit < end {
		end = offset + opts.Limit
	}
	nextCursor := ""
	hasMore := false
	if end < len(matched) {
		hasMore = true
		nextCursor = strconv.Itoa(end)
	}
	return PusherListPage{
		Pushers:    matched[offset:end],
		Total:      len(all),
		Matched:    len(matched),
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}
}

func (s *Store) loadPusherDirect(ctx context.Context, name string) (*Pusher, error) {
	data, err := s.store.ReadFile(ctx, paths.PusherRecord(name))
	if err != nil {
		return nil, err
	}
	var pusher Pusher
	if err := json.Unmarshal(data, &pusher); err != nil {
		return nil, err
	}
	pusher.Name = strings.TrimSpace(pusher.Name)
	if pusher.Name == "" {
		pusher.Name = name
	}
	return &pusher, nil
}

// GetPusher loads one pusher registration.
func (s *Store) GetPusher(ctx context.Context, name string) (*Pusher, error) {
	idx := getPusherIndex(s.store)
	if p, ok := idx.get(name); ok {
		return &p, nil
	}
	pusher, err := s.loadPusherDirect(ctx, name)
	if err != nil {
		return nil, err
	}
	idx.put(*pusher)
	return pusher, nil
}

// PutPusher creates or updates a pusher registration.
// When CreatedAt is already populated on the caller's struct or cached in the
// in-memory index, PutPusher skips the pre-write ReadFile RPC completely,
// halving KVS RPC load across 50,000 heartbeating pushers.
func (s *Store) PutPusher(ctx context.Context, pusher *Pusher) error {
	if err := pusher.Validate(); err != nil {
		return err
	}
	idx := getPusherIndex(s.store)
	if pusher.CreatedAt.IsZero() {
		if cached, ok := idx.get(pusher.Name); ok && !cached.CreatedAt.IsZero() {
			pusher.CreatedAt = cached.CreatedAt
		} else {
			existing, err := s.loadPusherDirect(ctx, pusher.Name)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if existing != nil {
				pusher.CreatedAt = existing.CreatedAt
			}
		}
	}
	now := s.now()
	if pusher.CreatedAt.IsZero() {
		pusher.CreatedAt = now
	}
	pusher.UpdatedAt = now
	if err := s.write(ctx, paths.PusherRecord(pusher.Name), pusher, "put pusher registration"); err != nil {
		return err
	}
	idx.put(*pusher)
	return nil
}

// DeletePusher removes a pusher registration.
func (s *Store) DeletePusher(ctx context.Context, name string) error {
	_, err := s.store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: paths.PusherRecord(name)}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand", Reason: "delete pusher registration"},
	})
	if err == nil {
		getPusherIndex(s.store).remove(name)
	}
	return err
}

// ListRepos returns all registered repos sorted by name.
func (s *Store) ListRepos(ctx context.Context) ([]Repo, error) {
	entries, err := s.store.ListDir(ctx, paths.ReposRoot())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Repo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name, ".json")
		repo, err := s.GetRepo(ctx, name)
		if err != nil {
			continue
		}
		out = append(out, *repo)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GetRepo loads one repo registration.
func (s *Store) GetRepo(ctx context.Context, name string) (*Repo, error) {
	data, err := s.store.ReadFile(ctx, paths.RepoRecord(name))
	if err != nil {
		return nil, err
	}
	var repo Repo
	if err := json.Unmarshal(data, &repo); err != nil {
		return nil, err
	}
	repo.Name = strings.TrimSpace(repo.Name)
	if repo.Name == "" {
		repo.Name = name
	}
	repo.Normalize()
	return &repo, nil
}

// PutRepo creates or updates a repo registration. The referenced pusher must
// exist.
func (s *Store) PutRepo(ctx context.Context, repo *Repo) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	if repo.Pusher != "" {
		if _, err := s.GetPusher(ctx, repo.Pusher); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return &UnknownPusherError{Name: repo.Pusher}
			}
			return err
		}
	}
	existing, err := s.GetRepo(ctx, repo.Name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	now := s.now()
	if existing != nil {
		repo.CreatedAt = existing.CreatedAt
		repo.LastSyncedAt = existing.LastSyncedAt
		repo.LastSyncCommit = existing.LastSyncCommit
		repo.LastSyncError = existing.LastSyncError
		repo.LastNotifiedAt = existing.LastNotifiedAt
		repo.LastNotifiedCommit = existing.LastNotifiedCommit
	}
	if repo.CreatedAt.IsZero() {
		repo.CreatedAt = now
	}
	repo.UpdatedAt = now
	return s.write(ctx, paths.RepoRecord(repo.Name), repo, "put repo registration")
}

// RecordSync stores the outcome of a repo sync.
func (s *Store) RecordSync(ctx context.Context, name, commit string, syncErr error) error {
	repo, err := s.GetRepo(ctx, name)
	if err != nil {
		return err
	}
	now := s.now()
	repo.UpdatedAt = now
	repo.LastSyncCommit = strings.TrimSpace(commit)
	if syncErr != nil {
		repo.LastSyncError = syncErr.Error()
	} else {
		repo.LastSyncedAt = now
		repo.LastSyncError = ""
	}
	return s.write(ctx, paths.RepoRecord(repo.Name), repo, "record repo sync")
}

// RecordNotification stores a CI merge notification for a repo.
func (s *Store) RecordNotification(ctx context.Context, name, commit string) error {
	repo, err := s.GetRepo(ctx, name)
	if err != nil {
		return err
	}
	now := s.now()
	repo.UpdatedAt = now
	repo.LastNotifiedAt = now
	repo.LastNotifiedCommit = strings.TrimSpace(commit)
	return s.write(ctx, paths.RepoRecord(repo.Name), repo, "record repo notification")
}

// DeleteRepo removes a repo registration.
func (s *Store) DeleteRepo(ctx context.Context, name string) error {
	_, err := s.store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: paths.RepoRecord(name)}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand", Reason: "delete repo registration"},
	})
	return err
}

func (s *Store) write(ctx context.Context, logicalPath string, value any, reason string) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = s.store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: payload}},
		Context: guardianapi.MutationContext{
			PrincipalID: "guardiand",
			Reason:      reason,
		},
	})
	return err
}

// UnknownPusherError reports a repo referencing a missing pusher.
type UnknownPusherError struct {
	Name string
}

func (e *UnknownPusherError) Error() string {
	return "pusher " + e.Name + " is not registered"
}
