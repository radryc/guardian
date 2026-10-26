package awsobserve

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Provider is the read surface exposed over the pusher HTTP API.
type Provider interface {
	List(account, region string) []StackState
	Get(account, region, stackName string) (StackState, bool)
	Events(account, region, stackName string, since time.Time, limit int) []Event
	Refresh(ctx context.Context, account, region string, stackNames []string, detectDrift bool) ([]StackState, error)
	Template(ctx context.Context, account, region, stackName string) (string, error)
	StackResources(ctx context.Context, account, region, stackName string) ([]StackResource, error)
	DiffSource(ctx context.Context, req SourceDiffRequest) (*SourceDiffResult, error)
}

// Service combines the observed cache with on-demand live refreshes.
type Service struct {
	Cache     *Cache
	Refresher *Refresher
	// StateDir is where synth workspaces are staged for source diffs.
	StateDir string
}

// NewService builds a service from a cache and refresher.
func NewService(cache *Cache, refresher *Refresher) *Service {
	if cache == nil {
		cache = NewCache(0)
	}
	return &Service{Cache: cache, Refresher: refresher}
}

func (s *Service) List(account, region string) []StackState {
	if s == nil {
		return nil
	}
	return s.Cache.List(account, region)
}

func (s *Service) Get(account, region, stackName string) (StackState, bool) {
	if s == nil {
		return StackState{}, false
	}
	return s.Cache.Get(account, region, stackName)
}

func (s *Service) Events(account, region, stackName string, since time.Time, limit int) []Event {
	if s == nil {
		return nil
	}
	return s.Cache.Events(account, region, stackName, since, limit)
}

func (s *Service) Refresh(ctx context.Context, account, region string, stackNames []string, detectDrift bool) ([]StackState, error) {
	if s == nil || s.Refresher == nil {
		return nil, fmt.Errorf("observed refresh is not configured")
	}
	if requested := strings.TrimSpace(account); requested != "" && s.Refresher.Account != "" && requested != s.Refresher.Account {
		return nil, fmt.Errorf("account %s is not handled by this pusher (account %s)", requested, s.Refresher.Account)
	}
	if strings.TrimSpace(region) == "" {
		return nil, fmt.Errorf("region is required")
	}
	states, err := s.Refresher.Refresh(ctx, region, stackNames, detectDrift)
	if err != nil {
		return nil, err
	}
	for _, state := range states {
		s.Cache.Upsert(state)
	}
	return states, nil
}

func (s *Service) Template(ctx context.Context, account, region, stackName string) (string, error) {
	if s == nil || s.Refresher == nil {
		return "", fmt.Errorf("observed refresh is not configured")
	}
	if requested := strings.TrimSpace(account); requested != "" && s.Refresher.Account != "" && requested != s.Refresher.Account {
		return "", fmt.Errorf("account %s is not handled by this pusher (account %s)", requested, s.Refresher.Account)
	}
	if strings.TrimSpace(stackName) == "" {
		return "", fmt.Errorf("stack name is required")
	}
	return s.Refresher.Template(ctx, region, stackName)
}

// StackResources returns the live logical-to-physical mapping for one stack.
func (s *Service) StackResources(ctx context.Context, account, region, stackName string) ([]StackResource, error) {
	if s == nil || s.Refresher == nil {
		return nil, fmt.Errorf("observed refresh is not configured")
	}
	if requested := strings.TrimSpace(account); requested != "" && s.Refresher.Account != "" && requested != s.Refresher.Account {
		return nil, fmt.Errorf("account %s is not handled by this pusher (account %s)", requested, s.Refresher.Account)
	}
	if strings.TrimSpace(region) == "" {
		return nil, fmt.Errorf("region is required")
	}
	if strings.TrimSpace(stackName) == "" {
		return nil, fmt.Errorf("stack name is required")
	}
	return s.Refresher.StackResources(ctx, region, stackName)
}
