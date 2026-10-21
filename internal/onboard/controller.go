package onboard

import (
	"context"
	"log"
	"time"
)

// Controller periodically pulls onboarding descriptors and materializes them.
// The store write wakes the partition watcher, so the controller does not need
// to trigger reconciliation itself.
type Controller struct {
	Service  *Service
	Interval time.Duration
	Options  Options
}

func (c *Controller) Run(ctx context.Context) error {
	if c == nil || c.Service == nil {
		return nil
	}
	interval := c.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	run := func() {
		results, err := c.Service.Run(ctx, c.Options)
		if err != nil && ctx.Err() == nil {
			log.Printf("[onboard] %v", err)
		}
		for _, result := range results {
			if result.Error != "" {
				log.Printf("[onboard] %s/%s: %s", result.Pusher, result.Stack, result.Error)
				continue
			}
			log.Printf("[onboard] %s/%s -> %s/%s probes=%d", result.Pusher, result.Stack, result.Partition, result.Intent, result.Probes)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			run()
		}
	}
}
