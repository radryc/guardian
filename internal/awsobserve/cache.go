package awsobserve

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultMaxEventsPerStack = 200

// Cache is a concurrency-safe in-memory store of observed stack state and a
// bounded per-stack event history. It is the source of truth served by the
// pusher's read API between refreshes.
type Cache struct {
	mu        sync.RWMutex
	stacks    map[string]StackState
	events    map[string][]Event
	maxEvents int
}

// NewCache creates a cache bounded to maxEvents per stack. Values <= 0 use the
// default bound.
func NewCache(maxEvents int) *Cache {
	if maxEvents <= 0 {
		maxEvents = defaultMaxEventsPerStack
	}
	return &Cache{
		stacks:    map[string]StackState{},
		events:    map[string][]Event{},
		maxEvents: maxEvents,
	}
}

func cacheKey(account, region, stackName string) string {
	return strings.Join([]string{
		strings.TrimSpace(account),
		strings.TrimSpace(region),
		strings.TrimSpace(stackName),
	}, "|")
}

// ApplyEvent merges a normalized event into the cache.
func (c *Cache) ApplyEvent(ev Event) {
	if c == nil {
		return
	}
	key := cacheKey(ev.Account, ev.Region, ev.StackName)
	if key == "||" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.stacks[key]
	if state.Account == "" {
		state.Account = ev.Account
		state.Region = ev.Region
		state.StackName = ev.StackName
	}
	if ev.StackID != "" {
		state.StackID = ev.StackID
	}
	if ev.Status != "" {
		state.StackStatus = ev.Status
	}
	if ev.StatusReason != "" {
		state.StatusReason = ev.StatusReason
	}
	if ev.DriftStatus != "" {
		state.DriftStatus = ev.DriftStatus
	}
	if ev.DetailType == detailTypeDriftDetection {
		state.DriftedResourceCount = ev.DriftedResourceCount
	}
	state.LastEventType = ev.DetailType
	state.LastEventID = ev.ID
	state.LastEventTime = ev.Time
	state.UpdatedAt = time.Now().UTC()
	state.Source = sourceEventBridge
	c.stacks[key] = state
	c.appendEventLocked(key, ev)
}

func (c *Cache) appendEventLocked(key string, ev Event) {
	events := c.events[key]
	events = append(events, ev)
	if len(events) > c.maxEvents {
		events = append([]Event(nil), events[len(events)-c.maxEvents:]...)
	}
	c.events[key] = events
}

// Upsert stores a refreshed stack state, preserving the event history.
func (c *Cache) Upsert(state StackState) {
	if c == nil {
		return
	}
	key := cacheKey(state.Account, state.Region, state.StackName)
	if key == "||" {
		return
	}
	state.Source = sourceRefresh
	state.UpdatedAt = time.Now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stacks[key] = state
}

// Get returns the cached state for one stack.
func (c *Cache) Get(account, region, stackName string) (StackState, bool) {
	if c == nil {
		return StackState{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	state, ok := c.stacks[cacheKey(account, region, stackName)]
	return state, ok
}

// List returns cached stacks, optionally filtered by account and region.
func (c *Cache) List(account, region string) []StackState {
	if c == nil {
		return nil
	}
	account = strings.TrimSpace(account)
	region = strings.TrimSpace(region)
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]StackState, 0, len(c.stacks))
	for _, state := range c.stacks {
		if account != "" && state.Account != account {
			continue
		}
		if region != "" && state.Region != region {
			continue
		}
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Account != out[j].Account {
			return out[i].Account < out[j].Account
		}
		if out[i].Region != out[j].Region {
			return out[i].Region < out[j].Region
		}
		return out[i].StackName < out[j].StackName
	})
	return out
}

// Events returns events for one stack newer than since, oldest first, capped at
// limit. A zero since returns the whole retained history.
func (c *Cache) Events(account, region, stackName string, since time.Time, limit int) []Event {
	if c == nil {
		return nil
	}
	key := cacheKey(account, region, stackName)
	c.mu.RLock()
	defer c.mu.RUnlock()
	events := c.events[key]
	out := make([]Event, 0, len(events))
	for _, ev := range events {
		if !since.IsZero() && ev.Time.Before(since) {
			continue
		}
		out = append(out, ev)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}
