// Package memcache provides an in-memory kvcache.Cache implementation:
// the reference backend for tests, and the default memo the sources keep
// for a process lifetime (the DVF per-commune section cache, the BAN
// geocode cache).
//
// It is deliberately small: one map plus an LRU list under a single mutex,
// with the same TTL semantics as a persistent backend (Get returns expired
// rows; only DeleteExpired enforces the TTL).
//
// It is also BOUNDED, which a long-lived server depends on. Every instance
// carries an entry ceiling (DefaultMaxEntries, or WithMaxEntries): once it
// is reached, an insertion first sweeps the expired rows and, if that frees
// nothing, evicts the least-recently-used row. Nothing sweeps on a timer,
// so a cache below its ceiling keeps expired rows exactly as before, ready
// for stale-while-revalidate reads; DeleteExpired stays available for an
// operator loop that wants to reclaim memory on its own schedule.
//
// Use New() to obtain a fresh instance. Instances are safe for concurrent
// use from multiple goroutines.
package memcache

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/bpineau/gazetteer/helpers/kvcache"
)

// DefaultMaxEntries is the entry ceiling New applies when the caller passes
// no WithMaxEntries option. The rows this cache holds are small (a geocode
// answer, one commune's cadastral section list), so 10 000 of them is a few
// MB: high enough that no realistic run evicts anything, low enough that a
// server running for weeks cannot grow without bound.
const DefaultMaxEntries = 10_000

// Option customises a cache built by New.
type Option func(*cache)

// WithMaxEntries overrides DefaultMaxEntries: the number of rows the cache
// keeps before an insertion starts reclaiming (expired rows first, then the
// least-recently-used one).
//
// n <= 0 means UNLIMITED: the cache then grows with its key space, which is
// only safe for a short-lived process or a known-bounded set of keys.
func WithMaxEntries(n int) Option {
	return func(c *cache) { c.max = n }
}

// New returns a fresh in-memory kvcache.Cache bounded to DefaultMaxEntries
// rows unless WithMaxEntries says otherwise. Safe for concurrent use.
func New(opts ...Option) kvcache.Cache {
	c := &cache{
		m:   make(map[string]*list.Element),
		lru: list.New(),
		max: DefaultMaxEntries,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// cache is the map plus the recency list, both guarded by mu. The map's
// values are the list elements, so a hit repositions its row in O(1); the
// list's values are *entry, front = most recently used.
//
// mu is a plain Mutex rather than an RWMutex because Get mutates the
// recency order: a read-only fast path would either lose LRU accuracy or
// need a second lock, and the critical sections here are one map lookup
// long.
type cache struct {
	mu  sync.Mutex
	m   map[string]*list.Element
	lru *list.List
	max int
}

// entry is one row as stored in an LRU element.
type entry struct {
	e kvcache.Entry
}

// Get returns the entry for key, or kvcache.ErrNotFound if missing, and
// marks the row as most-recently-used.
//
// Expired rows ARE returned: callers inspect Entry.ExpiresAt themselves
// (mirrors a persistent backend's stale-while-revalidate semantics).
func (c *cache) Get(_ context.Context, key string) (kvcache.Entry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.m[key]
	if !ok {
		return kvcache.Entry{}, kvcache.ErrNotFound
	}
	c.lru.MoveToFront(el)
	return detachExpiry(el.Value.(*entry).e), nil
}

// Set writes (or overwrites) a row and marks it most-recently-used.
// FetchedAt is filled in with the current UTC time when zero, mirroring a
// persistent backend.
//
// A new key inserted into a full cache triggers the reclaim described in
// the package doc; overwriting an existing key never evicts anything.
func (c *cache) Set(_ context.Context, e kvcache.Entry) error {
	if e.FetchedAt.IsZero() {
		e.FetchedAt = time.Now().UTC()
	}
	e = detachExpiry(e)

	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[e.Key]; ok {
		el.Value.(*entry).e = e
		c.lru.MoveToFront(el)
		return nil
	}
	c.makeRoom()
	c.m[e.Key] = c.lru.PushFront(&entry{e: e})
	return nil
}

// DeleteExpired removes every entry whose ExpiresAt is non-nil and
// before-or-equal-to now. Returns the number of removed rows. Rows with a
// nil ExpiresAt are kept forever (matching a SQL backend's
// `WHERE expires_at IS NOT NULL AND expires_at <= ?` predicate).
func (c *cache) DeleteExpired(_ context.Context, now time.Time) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deleteExpired(now), nil
}

// makeRoom enforces the entry ceiling ahead of one insertion. It sweeps the
// expired rows first, so dead weight goes before live data, and evicts
// least-recently-used rows only when the sweep freed nothing. Caller holds
// mu.
//
// A cache with max <= 0 (unlimited) or with room to spare does nothing:
// that is what keeps expired rows readable for stale-while-revalidate until
// the cache is actually under pressure.
func (c *cache) makeRoom() {
	if c.max <= 0 || len(c.m) < c.max {
		return
	}
	c.deleteExpired(time.Now().UTC())
	for len(c.m) >= c.max {
		oldest := c.lru.Back()
		if oldest == nil {
			return
		}
		c.remove(oldest)
	}
}

// deleteExpired drops every row that expired at or before now and returns
// how many went. Caller holds mu.
func (c *cache) deleteExpired(now time.Time) int64 {
	var n int64
	for el := c.lru.Front(); el != nil; {
		next := el.Next()
		if exp := el.Value.(*entry).e.ExpiresAt; exp != nil && !exp.After(now) {
			c.remove(el)
			n++
		}
		el = next
	}
	return n
}

// remove drops one row from both the map and the recency list. Caller holds
// mu.
func (c *cache) remove(el *list.Element) {
	delete(c.m, el.Value.(*entry).e.Key)
	c.lru.Remove(el)
}

// detachExpiry returns e with its own copy of *ExpiresAt, so a stored row
// and its caller cannot mutate each other's expiry instant. Entry.Value is
// deliberately shared (the rows are opaque payloads their callers treat as
// read-only), as it always has been.
func detachExpiry(e kvcache.Entry) kvcache.Entry {
	if e.ExpiresAt != nil {
		exp := *e.ExpiresAt
		e.ExpiresAt = &exp
	}
	return e
}
