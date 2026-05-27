// Package kvcache defines a backend-agnostic persistent key/value
// cache contract used across the project: the BAN geocoder cache, the
// DVF per-commune section cache, and any caller that needs a memo
// with optional TTL.
//
// The contract is the smallest surface that supports
// stale-while-revalidate (Get returns expired rows so the caller can
// decide whether to honour ExpiresAt) and out-of-band garbage
// collection (DeleteExpired).
//
// Conformance: pass the suite under helpers/kvcache/kvcachetest to
// validate a new backend.
//
// Reference backends:
//
//   - helpers/kvcache/memcache — in-memory, concurrent-safe; fine for
//     tests and short-lived processes.
//
// Persistent backends are out-of-scope for the library; implement
// the interface against any SQL / KV store and run the conformance
// suite.
//
// Example:
//
//	c := memcache.New()
//	_ = kvcache.Set(ctx, c, "geocode:ban:abc", payload,
//	    kvcache.WithTTL(365*24*time.Hour))
//	row, err := c.Get(ctx, "geocode:ban:abc")
package kvcache
