package watcher

import (
	"context"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type Watcher struct{}

func (w *Watcher) Watch(ctx context.Context, store guardianapi.WatchStore, partitions []string, debounce time.Duration) (<-chan string, error) {
	prefixes := make([]string, 0, len(partitions))
	if len(partitions) == 0 {
		prefixes = append(prefixes, paths.PartitionsRoot())
	} else {
		for _, partition := range partitions {
			prefixes = append(prefixes, paths.PartitionRoot(partition))
		}
	}
	events, err := store.Watch(ctx, prefixes)
	if err != nil {
		return nil, err
	}
	if debounce <= 0 {
		debounce = 100 * time.Millisecond
	}
	out := make(chan string)
	ready := make(chan string, len(prefixes)+8)
	timers := map[string]*time.Timer{}

	go func() {
		defer close(out)
		defer func() {
			for _, timer := range timers {
				timer.Stop()
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case name := <-ready:
				delete(timers, name)
				select {
				case out <- name:
				case <-ctx.Done():
					return
				}
			case event, ok := <-events:
				if !ok {
					return
				}
				if !isReconcileTriggerPath(event.LogicalPath) {
					continue
				}
				partition, ok := paths.PartitionFromLogicalPath(event.LogicalPath)
				if !ok {
					continue
				}
				if timer, exists := timers[partition]; exists {
					timer.Stop()
				}
				timers[partition] = time.AfterFunc(debounce, func() {
					select {
					case ready <- partition:
					case <-ctx.Done():
					}
				})
			}
		}
	}()
	return out, nil
}

func isReconcileTriggerPath(logicalPath string) bool {
	if !strings.HasPrefix(logicalPath, paths.PartitionsRoot()+"/") {
		return false
	}
	if strings.HasSuffix(logicalPath, "/config.yaml") {
		return true
	}
	return strings.Contains(logicalPath, "/intents/") && strings.HasSuffix(logicalPath, ".yaml")
}
