// Package kvcache defines a backend-agnostic persistent key/value cache
// contract used across the project: the BAN geocoder cache, the DVF
// per-commune section cache, the enrich-runner cursor, and any future
// caller that needs a memo with optional TTL.
//
// The contract is the smallest surface that lets callers do
// stale-while-revalidate (Get returns expired rows so the caller can
// decide whether to honour ExpiresAt) and gives a way to garbage-collect
// expired rows out-of-band (DeleteExpired). Concrete backends live in
// sibling packages or as adapters under internal/store; see
// pkg/kvcache/memcache for an in-memory reference implementation and
// internal/store/kvcacheadapter for the bun-backed adapter that powers
// every shipping consumer today.
package kvcache

import (
	"context"
	"errors"
	"time"
)

// Entry is one row in the cache. Value is opaque bytes; the caller picks
// the encoding (JSON / proto / gob / raw ASCII).
//
// FetchedAt is when the row was written. Backends MAY fill it in with
// time.Now() when Set is called with a zero value.
//
// ExpiresAt is optional: a nil ExpiresAt means "no TTL — keep forever
// unless Set is called again with the same Key". Get returns rows
// regardless of expiry; only DeleteExpired enforces the TTL contract.
type Entry struct {
	Key       string
	Value     []byte
	FetchedAt time.Time
	ExpiresAt *time.Time
}

// Cache is the contract every backend implements. All methods take
// context for cancellation and propagation.
//
// Lifetime expectations:
//
//   - Get returns ErrNotFound when no row exists for the given key.
//     Expired rows ARE returned (caller decides what to do with them).
//     This is intentional: it lets the cache double as a
//     stale-while-revalidate buffer without bolt-on logic.
//   - Set writes (or overwrites) one row. If Entry.FetchedAt is zero,
//     implementations should fill it in with the current time so callers
//     can do `Set(ctx, Entry{Key: k, Value: v})` without bookkeeping.
//   - DeleteExpired removes every row whose ExpiresAt is non-nil and
//     <= now, returning the count of removed rows. Rows with a nil
//     ExpiresAt are never removed by this call.
//
// Implementations MUST be safe for concurrent use from multiple
// goroutines.
type Cache interface {
	// Get returns the entry for key, or ErrNotFound if no entry exists.
	// Expired entries are returned; the caller is responsible for
	// inspecting Entry.ExpiresAt.
	Get(ctx context.Context, key string) (Entry, error)

	// Set writes an entry, replacing any existing row with the same key.
	// Implementations fill in Entry.FetchedAt when it is zero.
	Set(ctx context.Context, e Entry) error

	// DeleteExpired removes every entry whose ExpiresAt is in the past
	// relative to now. Returns the number of removed rows.
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

// ErrNotFound is returned by Cache.Get when no entry exists for the
// given key. Callers MUST use errors.Is(err, kvcache.ErrNotFound) — the
// sentinel value is part of the contract; concrete backends may wrap it.
var ErrNotFound = errors.New("kvcache: not found")

// SetOption customises a single Set call. The option pattern lets us
// add knobs (e.g. WithTTL, WithFreshness, future per-call hints) without
// breaking the Cache interface or callers that don't need them.
//
// SetOption is applied to a temporary buildSet struct; backends that
// honour an option do so via the helpers in this package (Set with
// options is provided as a free function below).
type SetOption func(*setConfig)

type setConfig struct {
	ttl       time.Duration // 0 means "no TTL"
	fetchedAt time.Time     // zero means "let backend fill it in"
}

// WithTTL is a SetOption that sets ExpiresAt to FetchedAt + ttl. ttl=0
// is treated as "no TTL" (ExpiresAt stays nil — the row is kept until
// overwritten).
//
// Use this in lieu of computing ExpiresAt yourself when the TTL is the
// only thing you care about: it keeps consumer code free of timestamp
// arithmetic.
func WithTTL(ttl time.Duration) SetOption {
	return func(c *setConfig) {
		c.ttl = ttl
	}
}

// Set is a convenience helper that builds an Entry from key+value and
// applies the given SetOptions before delegating to Cache.Set. The
// motivating use is keeping consumer code free of ExpiresAt pointer
// arithmetic:
//
//	err := kvcache.Set(ctx, c, "geocode:ban:abc", payload,
//	    kvcache.WithTTL(365*24*time.Hour))
//
// is equivalent to:
//
//	exp := time.Now().Add(365*24*time.Hour)
//	err := c.Set(ctx, kvcache.Entry{Key:"geocode:ban:abc", Value:payload, ExpiresAt: &exp})
//
// Callers that need fine-grained control (or want to set FetchedAt
// explicitly) can keep using Cache.Set directly.
func Set(ctx context.Context, c Cache, key string, value []byte, opts ...SetOption) error {
	cfg := setConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	e := Entry{
		Key:       key,
		Value:     value,
		FetchedAt: cfg.fetchedAt,
	}
	if cfg.ttl > 0 {
		base := cfg.fetchedAt
		if base.IsZero() {
			base = time.Now().UTC()
		}
		exp := base.Add(cfg.ttl)
		e.ExpiresAt = &exp
	}
	return c.Set(ctx, e)
}
