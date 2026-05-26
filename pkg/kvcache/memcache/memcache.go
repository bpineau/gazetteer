// Package memcache provides an in-memory kvcache.Cache implementation
// suitable for tests and short-lived processes that don't need
// persistence. It is intentionally minimal: a map guarded by a
// sync.RWMutex, with the same TTL semantics as the canonical bun-backed
// cache (Get returns expired rows; only DeleteExpired enforces TTL).
//
// Use New() to obtain a fresh instance. Instances are safe for concurrent
// use from multiple goroutines.
package memcache

import (
	"context"
	"sync"
	"time"

	"github.com/bpineau/gazetteer/pkg/kvcache"
)

// New returns a fresh in-memory kvcache.Cache. Safe for concurrent use.
func New() kvcache.Cache {
	return &cache{m: make(map[string]kvcache.Entry)}
}

type cache struct {
	mu sync.RWMutex
	m  map[string]kvcache.Entry
}

// Get returns the entry for key, or kvcache.ErrNotFound if missing.
// Expired rows ARE returned — callers inspect Entry.ExpiresAt themselves
// (mirrors the bun backend's stale-while-revalidate semantics).
func (c *cache) Get(_ context.Context, key string) (kvcache.Entry, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.m[key]
	if !ok {
		return kvcache.Entry{}, kvcache.ErrNotFound
	}
	// Defensive copy of ExpiresAt so callers cannot mutate the stored row.
	if e.ExpiresAt != nil {
		exp := *e.ExpiresAt
		e.ExpiresAt = &exp
	}
	return e, nil
}

// Set writes (or overwrites) a row. FetchedAt is filled in with the
// current UTC time when zero, mirroring the bun backend.
func (c *cache) Set(_ context.Context, e kvcache.Entry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.FetchedAt.IsZero() {
		e.FetchedAt = time.Now().UTC()
	}
	if e.ExpiresAt != nil {
		exp := *e.ExpiresAt
		e.ExpiresAt = &exp
	}
	c.m[e.Key] = e
	return nil
}

// DeleteExpired removes every entry whose ExpiresAt is non-nil and
// strictly before-or-equal-to now. Returns the number of removed rows.
// Rows with a nil ExpiresAt are kept forever (matching the bun backend's
// `WHERE expires_at IS NOT NULL AND expires_at <= ?` predicate).
func (c *cache) DeleteExpired(_ context.Context, now time.Time) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var n int64
	for k, e := range c.m {
		if e.ExpiresAt == nil {
			continue
		}
		if !e.ExpiresAt.After(now) {
			delete(c.m, k)
			n++
		}
	}
	return n, nil
}
