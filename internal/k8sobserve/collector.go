package k8sobserve

import (
	"context"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// ResourceLister is implemented by the Kubernetes backends. It returns every
// Guardian-managed object in the cluster, tagged with its partition/intent
// labels.
type ResourceLister interface {
	ListManagedResources(ctx context.Context) ([]Resource, error)
}

// Service maintains a cached, immutable inventory per partition/intent.
type Service struct {
	Lister   ResourceLister
	Cluster  string
	Interval time.Duration

	mu          sync.RWMutex
	stacks      []Inventory
	lastRefresh time.Time
}

// NewService builds an inventory service.
func NewService(lister ResourceLister, cluster string) *Service {
	return &Service{Lister: lister, Cluster: cluster, Interval: 30 * time.Second}
}

// Run refreshes the cache until the context is cancelled.
func (s *Service) Run(ctx context.Context) {
	if s == nil || s.Lister == nil {
		return
	}
	interval := s.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if _, err := s.Refresh(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[k8sobserve] initial refresh: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.Refresh(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[k8sobserve] refresh: %v", err)
			}
		}
	}
}

// Refresh lists the cluster and replaces the cache.
func (s *Service) Refresh(ctx context.Context) ([]Inventory, error) {
	if s == nil || s.Lister == nil {
		return nil, nil
	}
	resources, err := s.Lister.ListManagedResources(ctx)
	if err != nil {
		return nil, err
	}
	stacks := Group(resources, s.Cluster)
	s.mu.Lock()
	s.stacks = stacks
	s.lastRefresh = time.Now().UTC()
	s.mu.Unlock()
	return stacks, nil
}

// List returns the cached inventories.
func (s *Service) List() []Inventory {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Inventory(nil), s.stacks...)
}

// Get returns one cached inventory by stack name (partition/intent).
func (s *Service) Get(stackName string) (Inventory, bool) {
	if s == nil {
		return Inventory{}, false
	}
	stackName = strings.Trim(strings.TrimSpace(stackName), "/")
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, stack := range s.stacks {
		if stack.Partition+"/"+stack.Intent == stackName {
			return stack, true
		}
	}
	return Inventory{}, false
}

// LastRefresh returns the cache timestamp.
func (s *Service) LastRefresh() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRefresh
}

// Group converts a flat resource list into one inventory per partition/intent.
func Group(resources []Resource, cluster string) []Inventory {
	now := time.Now().UTC()
	byStack := map[string]*Inventory{}
	for _, resource := range resources {
		partition := strings.TrimSpace(resource.Partition)
		intent := strings.TrimSpace(resource.Intent)
		if partition == "" || intent == "" {
			continue
		}
		key := partition + "/" + intent
		inventory, ok := byStack[key]
		if !ok {
			inventory = &Inventory{
				APIVersion:  "guardian/v1alpha1",
				Kind:        InventoryKind,
				Cluster:     cluster,
				Partition:   partition,
				Intent:      intent,
				GeneratedAt: now,
			}
			byStack[key] = inventory
		}
		inventory.Resources = append(inventory.Resources, resource)
	}
	out := make([]Inventory, 0, len(byStack))
	for _, inventory := range byStack {
		sort.Slice(inventory.Resources, func(i, j int) bool {
			left, right := inventory.Resources[i], inventory.Resources[j]
			if left.Kind != right.Kind {
				return left.Kind < right.Kind
			}
			if left.Namespace != right.Namespace {
				return left.Namespace < right.Namespace
			}
			return left.Name < right.Name
		})
		out = append(out, *inventory)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Partition != out[j].Partition {
			return out[i].Partition < out[j].Partition
		}
		return out[i].Intent < out[j].Intent
	})
	return out
}
