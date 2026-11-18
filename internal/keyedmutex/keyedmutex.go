// Package keyedmutex provides per-key mutual exclusion without a single global
// lock held for the duration of the operation.
package keyedmutex

import "sync"

// Locks hands out one mutex per key. Entries are never removed: callers use a
// small, stable key space (partitions, task archives).
type Locks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func New() *Locks {
	return &Locks{locks: map[string]*sync.Mutex{}}
}

// Lock acquires the mutex for key and returns its unlock function.
func (l *Locks) Lock(key string) func() {
	l.mu.Lock()
	lock, ok := l.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		l.locks[key] = lock
	}
	l.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}
