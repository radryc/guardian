package dispatcher

import (
	"sync"

	releasedomain "github.com/radryc/guardian/internal/domain/release"
	statedomain "github.com/radryc/guardian/internal/domain/state"
)

// writeTracker remembers the hash of the last bytes written to each logical
// path so identical serialisations can skip a store round-trip.
type writeTracker struct {
	mu     sync.RWMutex
	hashes map[string]uint64
}

func newWriteTracker() *writeTracker {
	return &writeTracker{hashes: map[string]uint64{}}
}

func (t *writeTracker) get(logicalPath string) (uint64, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	h, ok := t.hashes[logicalPath]
	return h, ok
}

func (t *writeTracker) set(logicalPath string, hash uint64) {
	t.mu.Lock()
	t.hashes[logicalPath] = hash
	t.mu.Unlock()
}

func (t *writeTracker) delete(logicalPath string) {
	t.mu.Lock()
	delete(t.hashes, logicalPath)
	t.mu.Unlock()
}

// runtimeCache holds the in-memory partition aggregate. The pointer returned by
// get is the live cache entry; callers must hold the partition write lock and
// update it through with() before mutating.
type runtimeCache struct {
	mu          sync.RWMutex
	byPartition map[string]*statedomain.PartitionRuntime
}

func newRuntimeCache() *runtimeCache {
	return &runtimeCache{byPartition: map[string]*statedomain.PartitionRuntime{}}
}

func (c *runtimeCache) get(partition string) (*statedomain.PartitionRuntime, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	runtime, ok := c.byPartition[partition]
	return runtime, ok
}

func (c *runtimeCache) put(runtime *statedomain.PartitionRuntime) {
	if runtime == nil {
		return
	}
	c.mu.Lock()
	c.byPartition[runtime.Partition] = runtime
	c.mu.Unlock()
}

// swap installs runtime and returns a clone of the entry it replaced.
func (c *runtimeCache) swap(runtime *statedomain.PartitionRuntime) *statedomain.PartitionRuntime {
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := statedomain.ClonePartitionRuntime(c.byPartition[runtime.Partition])
	c.byPartition[runtime.Partition] = runtime
	return previous
}

// take removes and returns the entry for partition.
func (c *runtimeCache) take(partition string) *statedomain.PartitionRuntime {
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.byPartition[partition]
	delete(c.byPartition, partition)
	return previous
}

func (c *runtimeCache) with(partition string, mutate func(*statedomain.PartitionRuntime)) (*statedomain.PartitionRuntime, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	runtime, ok := c.byPartition[partition]
	if !ok {
		return nil, false
	}
	mutate(runtime)
	return runtime, true
}

func (c *runtimeCache) clone(partition string) *statedomain.PartitionRuntime {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return statedomain.ClonePartitionRuntime(c.byPartition[partition])
}

func (c *runtimeCache) remove(partition string) {
	c.mu.Lock()
	delete(c.byPartition, partition)
	c.mu.Unlock()
}

func (c *runtimeCache) stale(seen map[string]struct{}) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	stale := make([]string, 0, len(c.byPartition))
	for partition := range c.byPartition {
		if _, ok := seen[partition]; !ok {
			stale = append(stale, partition)
		}
	}
	return stale
}

func (c *runtimeCache) len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.byPartition)
}

func (c *runtimeCache) status(partition string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if cached := c.byPartition[partition]; cached != nil && cached.PartitionState != nil {
		return cached.PartitionState.Status
	}
	return ""
}

// releaseLogCache holds the per-partition release history.
type releaseLogCache struct {
	mu          sync.RWMutex
	byPartition map[string]*releasedomain.PartitionReleaseLog
}

func newReleaseLogCache() *releaseLogCache {
	return &releaseLogCache{byPartition: map[string]*releasedomain.PartitionReleaseLog{}}
}

func (c *releaseLogCache) get(partition string) (*releasedomain.PartitionReleaseLog, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	log, ok := c.byPartition[partition]
	return log, ok
}

func (c *releaseLogCache) put(log *releasedomain.PartitionReleaseLog) {
	if log == nil {
		return
	}
	c.mu.Lock()
	c.byPartition[log.Partition] = releasedomain.ClonePartitionReleaseLog(log)
	c.mu.Unlock()
}

func (c *releaseLogCache) remove(partition string) {
	c.mu.Lock()
	delete(c.byPartition, partition)
	c.mu.Unlock()
}

func (c *releaseLogCache) stale(seen map[string]struct{}) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	stale := make([]string, 0, len(c.byPartition))
	for partition := range c.byPartition {
		if _, ok := seen[partition]; !ok {
			stale = append(stale, partition)
		}
	}
	return stale
}
