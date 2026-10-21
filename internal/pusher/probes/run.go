package probes

import (
	"context"
	"log"
	"sync"
	"time"
)

type Runner struct {
	cfg            Config
	otel           *otelExporter
	mu             sync.Mutex
	targets        []Target
	lastRun        map[string]time.Time
	previous       map[string]Result
	snapshotHashes map[string]string
}

func New(cfg Config) *Runner {
	return &Runner{
		cfg:            cfg,
		otel:           newOTELExporter(cfg.OTLPEndpoint),
		lastRun:        map[string]time.Time{},
		previous:       map[string]Result{},
		snapshotHashes: map[string]string{},
	}
}

func (r *Runner) Run(ctx context.Context) error {
	if !r.cfg.Enabled || (r.cfg.Store == nil && r.cfg.ExternalTargets == nil) || r.cfg.Pusher == "" {
		return nil
	}
	log.Printf("[Probes] enabled pusher=%s cluster=%s interval=%s timeout=%s max=%d", r.cfg.Pusher, r.cfg.Cluster, r.cfg.Interval, r.cfg.Timeout, r.cfg.MaxTargets)
	r.refresh(ctx)
	runTicker := time.NewTicker(5 * time.Second)
	discoveryTicker := time.NewTicker(r.cfg.DiscoveryInterval)
	defer runTicker.Stop()
	defer discoveryTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-discoveryTicker.C:
			r.refresh(ctx)
		case <-runTicker.C:
			r.runDue(ctx)
		}
	}
}

func (r *Runner) refresh(ctx context.Context) {
	targets, err := r.discover(ctx)
	if err != nil {
		log.Printf("[Probes] discovery failed: %v", err)
		return
	}
	r.mu.Lock()
	r.targets = targets
	active := map[string]bool{}
	for _, target := range targets {
		active[target.ID] = true
	}
	for id := range r.lastRun {
		if !active[id] {
			delete(r.lastRun, id)
			delete(r.previous, id)
		}
	}
	r.mu.Unlock()
	log.Printf("[Probes] discovered %d probe targets", len(targets))
	r.writeSnapshots(ctx)
}

func (r *Runner) runDue(ctx context.Context) {
	r.mu.Lock()
	now := time.Now()
	due := make([]Target, 0)
	for _, target := range r.targets {
		interval := target.Interval
		if interval <= 0 {
			interval = r.cfg.Interval
		}
		if last, ok := r.lastRun[target.ID]; ok && now.Sub(last) < interval {
			continue
		}
		r.lastRun[target.ID] = now
		due = append(due, target)
	}
	r.mu.Unlock()
	if len(due) == 0 {
		return
	}

	results := make([]Result, len(due))
	var wg sync.WaitGroup
	sem := make(chan struct{}, r.cfg.Concurrency)
	for idx, target := range due {
		wg.Add(1)
		go func(idx int, target Target) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			results[idx] = r.execute(ctx, target)
		}(idx, target)
	}
	wg.Wait()

	r.mu.Lock()
	previousStatus := make(map[string]string, len(results))
	for id, result := range r.previous {
		previousStatus[id] = result.Status
	}
	for idx := range results {
		result := &results[idx]
		if result.ProbeID == "" {
			continue
		}
		prev, _ := r.previous[result.ProbeID]
		if result.Success {
			result.LastSuccessAt = result.CheckedAt
			result.ConsecutiveFailures = 0
		} else {
			result.LastFailureAt = result.CheckedAt
			result.LastSuccessAt = prev.LastSuccessAt
			result.ConsecutiveFailures = prev.ConsecutiveFailures + 1
		}
		r.previous[result.ProbeID] = *result
	}
	r.mu.Unlock()

	SortResults(results)
	r.emitTelemetry(ctx, results, previousStatus)
	r.writeSnapshots(ctx)
}

// Latest returns the most recent result for every known probe target. It is
// used by the storeless pusher read API so Guardian can pull probe state
// without MonoFS access from the customer account.
func (r *Runner) Latest() []Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Result, 0, len(r.previous))
	for _, result := range r.previous {
		out = append(out, result)
	}
	SortResults(out)
	return out
}

func (r *Runner) statusMap() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.previous))
	for id, result := range r.previous {
		out[id] = result.Status
	}
	return out
}
