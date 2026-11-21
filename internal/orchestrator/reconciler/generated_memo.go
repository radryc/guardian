package reconciler

import "sync"

// cdkMemo remembers the last generated CDK fingerprint per partition/intent so
// unchanged intents skip regeneration. It is separate from the reconcile
// scheduler state and safe for concurrent reconcile workers.
type cdkMemo struct {
	mu     sync.RWMutex
	hashes map[string]string
}

func newCDKMemo() *cdkMemo {
	return &cdkMemo{hashes: map[string]string{}}
}

func (m *cdkMemo) get(key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.hashes[key]
}

func (m *cdkMemo) set(key, fingerprint string) {
	m.mu.Lock()
	m.hashes[key] = fingerprint
	m.mu.Unlock()
}
