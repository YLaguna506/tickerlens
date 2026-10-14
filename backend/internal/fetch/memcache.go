package fetch

import (
	"context"
	"sync"
	"time"
)

// MemoryCache is an in-process Cache, used in tests.
type MemoryCache struct {
	mu      sync.Mutex // guards entries; Go maps are not safe for concurrent use
	entries map[string]memEntry
	now     func() time.Time
}

type memEntry struct {
	body    []byte
	expires time.Time
}

// NewMemoryCache returns an empty MemoryCache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{entries: map[string]memEntry{}, now: time.Now}
}

// Get implements Cache.
func (m *MemoryCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key]
	if !ok || m.now().After(e.expires) {
		return nil, false, nil
	}
	return e.body, true, nil
}

// Set implements Cache.
func (m *MemoryCache) Set(_ context.Context, key string, body []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[key] = memEntry{body: body, expires: m.now().Add(ttl)}
	return nil
}
