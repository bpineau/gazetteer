package gazetteer

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/bpineau/gazetteer/helpers/kvcache"
	"github.com/bpineau/gazetteer/helpers/kvcache/memcache"
)

// NewKVMemCache returns a fresh in-memory kvcache.Cache. Convenience
// re-export of helpers/kvcache/memcache.New so callers wiring a Source
// that consumes kvcache.Cache (DVF, banx, the rental ladder) don't need
// a second import line just to obtain a sensible default backend.
//
// Persistence: none. Suitable for one-shot tools, tests, and short
// processes. Long-running batch consumers should plug a persistent
// backend (e.g. the bun-backed adapter) and pass its kvcache.Cache
// here instead.
func NewKVMemCache() kvcache.Cache {
	return memcache.New()
}

// Cache is the persistence-agnostic key/value cache that Sources use for
// expensive intermediate computations (BAN geocodes, DVF section
// catalogs, MA street URLs).
//
// The default backend is MemCache (bounded in-memory LRU). Callers that
// need persistence across processes — e.g. encheridor's re-walks — plug
// their own backend via gazetteer.Builder.WithCache.
type Cache interface {
	Get(ctx context.Context, key string) (value []byte, hit bool, err error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

// MemCache is a thread-safe bounded LRU cache with per-entry TTL.
// Suitable for one-shot tools (CLI / investment-locatif). For batch
// workloads that re-walk the same listings, use a persistent backend.
type MemCache struct {
	mu    sync.Mutex
	max   int
	order *list.List               // front = MRU
	items map[string]*list.Element // key → element holding *memEntry
}

type memEntry struct {
	key     string
	value   []byte
	expires time.Time
}

// NewMemCache returns an LRU bounded to maxEntries. maxEntries <= 0 is
// treated as 1 (the smallest legal cache).
func NewMemCache(maxEntries int) *MemCache {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &MemCache{
		max:   maxEntries,
		order: list.New(),
		items: make(map[string]*list.Element, maxEntries),
	}
}

// Get returns (value, true, nil) on a hit, (nil, false, nil) on miss or
// TTL expiry.
func (m *MemCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.items[key]
	if !ok {
		return nil, false, nil
	}
	e := el.Value.(*memEntry)
	if !e.expires.IsZero() && time.Now().After(e.expires) {
		m.order.Remove(el)
		delete(m.items, key)
		return nil, false, nil
	}
	m.order.MoveToFront(el)
	// Defensive copy: callers must not be able to mutate the cache's
	// internal byte slice.
	out := make([]byte, len(e.value))
	copy(out, e.value)
	return out, true, nil
}

// Set writes the value with the given TTL (0 = no expiry). A defensive
// copy of value is stored.
func (m *MemCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[key]; ok {
		e := el.Value.(*memEntry)
		e.value = append(e.value[:0], value...)
		if ttl > 0 {
			e.expires = time.Now().Add(ttl)
		} else {
			e.expires = time.Time{}
		}
		m.order.MoveToFront(el)
		return nil
	}
	buf := make([]byte, len(value))
	copy(buf, value)
	entry := &memEntry{key: key, value: buf}
	if ttl > 0 {
		entry.expires = time.Now().Add(ttl)
	}
	el := m.order.PushFront(entry)
	m.items[key] = el
	for m.order.Len() > m.max {
		oldest := m.order.Back()
		if oldest == nil {
			break
		}
		m.order.Remove(oldest)
		delete(m.items, oldest.Value.(*memEntry).key)
	}
	return nil
}
