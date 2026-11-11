package gcpobserve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	statePrefix     = "guardian/"
	stateSuffix     = "/default.tfstate"
	cacheTTL        = 30 * time.Second
	maxStateObjects = 1000
)

// Provider is the read surface exposed by the pusher observed API.
type Provider interface {
	List(project, region string) []StackState
	Get(project, region, stackName string) (StackState, bool)
	Refresh(ctx context.Context, project, region string, stackNames []string) ([]StackState, error)
	StackResources(ctx context.Context, project, region, stackName string) ([]StackResource, error)
}

// Cache holds the latest observed stacks.
type Cache struct {
	mu          sync.RWMutex
	stacks      map[string]StackState
	lastRefresh time.Time
}

// NewCache builds an empty cache.
func NewCache() *Cache { return &Cache{stacks: map[string]StackState{}} }

func cacheKey(project, stackName string) string { return project + "/" + stackName }

// Put stores stacks grouped by stack name.
func (c *Cache) Put(project string, stacks []StackState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, stack := range stacks {
		c.stacks[cacheKey(project, stack.StackName)] = stack
	}
	c.lastRefresh = time.Now()
}

// PutSnapshot replaces every cached stack for a project. A full refresh must
// evict stacks whose state object disappeared, otherwise deleted intents stay
// visible forever.
func (c *Cache) PutSnapshot(project string, stacks []StackState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := project + "/"
	for key := range c.stacks {
		if strings.HasPrefix(key, prefix) {
			delete(c.stacks, key)
		}
	}
	for _, stack := range stacks {
		c.stacks[cacheKey(project, stack.StackName)] = stack
	}
	c.lastRefresh = time.Now()
}

// List returns cached stacks for a project (Region empty matches everything).
func (c *Cache) List(project, region string) []StackState {
	c.mu.RLock()
	out := make([]StackState, 0, len(c.stacks))
	for _, stack := range c.stacks {
		if project != "" && !strings.EqualFold(stack.Project, project) {
			continue
		}
		if region != "" && stack.Region != "" && !strings.EqualFold(stack.Region, region) {
			continue
		}
		out = append(out, stack)
	}
	c.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StackName < out[j].StackName })
	return out
}

// Get returns one cached stack.
func (c *Cache) Get(project, stackName string) (StackState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	stack, ok := c.stacks[cacheKey(project, stackName)]
	return stack, ok
}

// LastRefresh returns the cache timestamp.
func (c *Cache) LastRefresh() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastRefresh
}

// Refresher reads GCS state objects and Cloud Asset Inventory.
type Refresher struct {
	StateBucket string
	Region      string
	HTTPClient  *http.Client
	TokenSource oauth2.TokenSource
	StorageURL  string
	AssetURL    string
	Now         func() time.Time

	clientMu sync.Mutex
	client   *http.Client
	tokens   oauth2.TokenSource
}

// NewRefresher builds a refresher.
func NewRefresher(stateBucket, region string) *Refresher {
	return &Refresher{StateBucket: stateBucket, Region: region}
}

// Service combines the cache with on-demand refreshes.
type Service struct {
	Cache     *Cache
	Refresher *Refresher

	mu         sync.Mutex
	refreshKey string
	refresh    *refreshCall

	// refreshSerial prevents a filtered refresh (one stack) and a full refresh
	// from scanning Cloud Asset Inventory at the same time.
	refreshSerial sync.Mutex
}

type refreshCall struct {
	done   chan struct{}
	stacks []StackState
	err    error
}

// NewService builds a service.
func NewService(cache *Cache, refresher *Refresher) *Service {
	if cache == nil {
		cache = NewCache()
	}
	return &Service{Cache: cache, Refresher: refresher}
}

// List returns cached stacks.
func (s *Service) List(project, region string) []StackState {
	if s == nil || s.Cache == nil {
		return nil
	}
	return s.Cache.List(project, region)
}

// Get returns one cached stack.
func (s *Service) Get(project, region, stackName string) (StackState, bool) {
	if s == nil || s.Cache == nil {
		return StackState{}, false
	}
	stack, ok := s.Cache.Get(project, stackName)
	if !ok {
		return StackState{}, false
	}
	if region != "" && stack.Region != "" && !strings.EqualFold(stack.Region, region) {
		return StackState{}, false
	}
	return stack, true
}

// Refresh re-reads GCS state objects and CAL resources. Concurrent refreshes
// with the same scope share one in-flight pass so a burst of HTTP requests
// cannot trigger a dozen simultaneous Cloud Asset Inventory scans.
func (s *Service) Refresh(ctx context.Context, project, region string, stackNames []string) ([]StackState, error) {
	if s == nil || s.Refresher == nil {
		return nil, fmt.Errorf("observed refresh is not configured")
	}
	region = firstNonEmpty(region, s.Refresher.Region)
	key := refreshKey(project, region, stackNames)

	s.mu.Lock()
	if s.refresh != nil && s.refreshKey == key {
		call := s.refresh
		s.mu.Unlock()
		select {
		case <-call.done:
			return call.stacks, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	s.refresh, s.refreshKey = call, key
	s.mu.Unlock()

	stacks, err := func() ([]StackState, error) {
		s.refreshSerial.Lock()
		defer s.refreshSerial.Unlock()
		return s.Refresher.Refresh(ctx, project, region, stackNames)
	}()
	if err == nil {
		s.mergePreviousResources(project, stacks)
		if len(stackNames) == 0 {
			s.Cache.PutSnapshot(project, stacks)
		} else {
			s.Cache.Put(project, stacks)
		}
	}
	call.stacks, call.err = stacks, err

	s.mu.Lock()
	if s.refresh == call {
		s.refresh = nil
		s.refreshKey = ""
	}
	s.mu.Unlock()
	close(call.done)
	return stacks, err
}

// mergePreviousResources keeps the last good resource inventory when a refresh
// could not enumerate Cloud Asset Inventory. Publishing an empty resource list
// would make the observed collector delete every synthetic topology asset.
func (s *Service) mergePreviousResources(project string, stacks []StackState) {
	for i := range stacks {
		if stacks[i].ResourcesComplete || len(stacks[i].Resources) > 0 {
			continue
		}
		if previous, ok := s.Cache.Get(project, stacks[i].StackName); ok {
			stacks[i].Resources = previous.Resources
		}
	}
}

func refreshKey(project, region string, stackNames []string) string {
	names := append([]string(nil), stackNames...)
	sort.Strings(names)
	return project + "|" + region + "|" + strings.Join(names, ",")
}

// StackResources returns the observed resources for one stack.
func (s *Service) StackResources(ctx context.Context, project, region, stackName string) ([]StackResource, error) {
	stack, ok := s.Get(project, region, stackName)
	if ok && len(stack.Resources) > 0 {
		return stack.Resources, nil
	}
	stacks, err := s.Refresh(ctx, project, region, []string{stackName})
	if err != nil {
		return nil, err
	}
	for _, candidate := range stacks {
		if candidate.StackName == stackName {
			return candidate.Resources, nil
		}
	}
	return nil, nil
}

// Refresh lists state objects and matches CAL resources to them.
func (r *Refresher) Refresh(ctx context.Context, project, region string, stackNames []string) ([]StackState, error) {
	if strings.TrimSpace(project) == "" {
		return nil, fmt.Errorf("project is required")
	}
	client, err := r.httpClient(ctx)
	if err != nil {
		return nil, err
	}
	states, err := r.listStates(ctx, client)
	if err != nil {
		return nil, err
	}
	resources, resourcesErr := r.listResources(ctx, client, project)
	resourcesComplete := resourcesErr == nil
	if resourcesErr != nil {
		// CAL is best-effort: state objects still provide a useful view, but
		// callers must not interpret the missing resources as "all deleted".
		log.Printf("[gcpobserve] cloud asset inventory refresh failed (keeping previous resources): %v", resourcesErr)
		resources = nil
	}

	wanted := map[string]struct{}{}
	for _, name := range stackNames {
		if name = strings.TrimSpace(name); name != "" {
			wanted[name] = struct{}{}
		}
	}
	out := make([]StackState, 0, len(states))
	for _, state := range states {
		if len(wanted) > 0 {
			if _, ok := wanted[state.StackName]; !ok {
				continue
			}
		}
		state.Project = project
		state.ResourcesComplete = resourcesComplete
		prefixes := []string{
			fmt.Sprintf("guardian-%s-%s-", state.Partition, state.Intent),
			fmt.Sprintf("guardian-%s-%s-", sanitizeName(state.Partition), sanitizeName(state.Intent)),
		}
		for _, resource := range resources {
			if matchesAnyResourcePrefix(resource, prefixes) {
				state.Resources = append(state.Resources, resource)
			}
		}
		out = append(out, state)
	}
	return out, nil
}

func matchesAnyResourcePrefix(resource StackResource, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		if strings.HasPrefix(resource.DisplayName, prefix) {
			return true
		}
		for _, segment := range strings.Split(resource.Name, "/") {
			if strings.HasPrefix(segment, prefix) {
				return true
			}
		}
	}
	return false
}

// sanitizeName mirrors the GCP driver naming rules so state objects whose
// partition or intent contains dots/underscores still match their resources.
func sanitizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, ".", "-")
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (r *Refresher) httpClient(ctx context.Context) (*http.Client, error) {
	if r.HTTPClient != nil {
		return r.HTTPClient, nil
	}
	r.clientMu.Lock()
	defer r.clientMu.Unlock()
	if r.client != nil {
		return r.client, nil
	}
	tokens := r.TokenSource
	if tokens == nil {
		tokens = r.tokens
	}
	if tokens == nil {
		var err error
		tokens, err = google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return nil, fmt.Errorf("application default credentials: %w", err)
		}
	}
	r.tokens = tokens
	r.client = oauth2.NewClient(ctx, tokens)
	return r.client, nil
}

type stateObject struct {
	Name    string `json:"name"`
	Updated string `json:"updated"`
	Size    string `json:"size"`
}

func (r *Refresher) listStates(ctx context.Context, client *http.Client) ([]StackState, error) {
	if strings.TrimSpace(r.StateBucket) == "" {
		return nil, fmt.Errorf("state bucket is required")
	}
	base := firstNonEmpty(r.StorageURL, "https://storage.googleapis.com")

	byStack := map[string]StackState{}
	pageToken := ""
	for page := 0; page < 20; page++ {
		params := url.Values{}
		params.Set("prefix", statePrefix)
		params.Set("maxResults", fmt.Sprintf("%d", maxStateObjects))
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		endpoint := fmt.Sprintf("%s/storage/v1/b/%s/o?%s", base, url.PathEscape(r.StateBucket), params.Encode())

		var response struct {
			Items         []stateObject `json:"items"`
			NextPageToken string        `json:"nextPageToken"`
		}
		if err := getJSON(ctx, client, endpoint, &response); err != nil {
			return nil, err
		}
		for _, item := range response.Items {
			partition, intent, ok := parseStateName(item.Name)
			if !ok {
				continue
			}
			updated, _ := time.Parse(time.RFC3339Nano, item.Updated)
			key := partition + "/" + intent
			// One intent can own several state objects (one per asset). Keep
			// the newest object as the stack's state pointer.
			if previous, ok := byStack[key]; ok && !updated.After(previous.UpdatedAt) {
				continue
			}
			byStack[key] = StackState{
				Project:   "",
				Region:    r.Region,
				StackName: key,
				Partition: partition,
				Intent:    intent,
				Status:    "StatePresent",
				StatePath: item.Name,
				UpdatedAt: updated,
				Source:    "gcs-state",
			}
		}
		if strings.TrimSpace(response.NextPageToken) == "" {
			break
		}
		pageToken = response.NextPageToken
	}

	out := make([]StackState, 0, len(byStack))
	for _, stack := range byStack {
		out = append(out, stack)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StackName < out[j].StackName })
	return out, nil
}

// parseStateName extracts partition/intent from a state object. Both the legacy
// guardian/<p>/<i>/default.tfstate and the per-asset
// guardian/<p>/<i>/<asset>/default.tfstate layout are accepted.
func parseStateName(name string) (string, string, bool) {
	if !strings.HasPrefix(name, statePrefix) || !strings.HasSuffix(name, stateSuffix) {
		return "", "", false
	}
	trimmed := strings.TrimSuffix(strings.TrimPrefix(name, statePrefix), stateSuffix)
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 && len(parts) != 3 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (r *Refresher) listResources(ctx context.Context, client *http.Client, project string) ([]StackResource, error) {
	base := firstNonEmpty(r.AssetURL, "https://cloudasset.googleapis.com")
	out := make([]StackResource, 0)
	pageToken := ""
	for page := 0; page < 20; page++ {
		params := url.Values{}
		params.Set("pageSize", "500")
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		endpoint := fmt.Sprintf("%s/v1/projects/%s:searchAllResources?%s", base, url.PathEscape(project), params.Encode())

		var response struct {
			Results []struct {
				Name                   string `json:"name"`
				AssetType              string `json:"assetType"`
				Location               string `json:"location"`
				DisplayName            string `json:"displayName"`
				State                  string `json:"state"`
				ParentFullResourceName string `json:"parentFullResourceName"`
				CreateTime             string `json:"createTime"`
				UpdateTime             string `json:"updateTime"`
			} `json:"results"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := getJSON(ctx, client, endpoint, &response); err != nil {
			return out, err
		}
		for _, result := range response.Results {
			resource := StackResource{
				Name:                   result.Name,
				AssetType:              result.AssetType,
				Location:               result.Location,
				DisplayName:            result.DisplayName,
				State:                  result.State,
				ParentFullResourceName: result.ParentFullResourceName,
			}
			if parsed, err := time.Parse(time.RFC3339Nano, result.CreateTime); err == nil {
				resource.CreateTime = parsed
			}
			if parsed, err := time.Parse(time.RFC3339Nano, result.UpdateTime); err == nil {
				resource.UpdateTime = parsed
			}
			out = append(out, resource)
		}
		if strings.TrimSpace(response.NextPageToken) == "" {
			break
		}
		pageToken = response.NextPageToken
	}
	return out, nil
}

func getJSON(ctx context.Context, client *http.Client, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gcpobserve: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("gcpobserve: decode: %w", err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
