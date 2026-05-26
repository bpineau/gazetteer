// Package kvgaz bridges a kvcache.Cache to the gazetteer.Cache contract.
//
// Two Cache interfaces coexist in the library: gazetteer.Cache
// (callable from any Source's Options) and kvcache.Cache (the
// persistence contract used by helpers/banx, sources/dvf, and
// the bun-backed adapter every shipping consumer wires in). They
// differ on the Get return shape (kvcache: (Entry, error) with
// ErrNotFound sentinel; gazetteer: (value, hit, err) tuple) and on
// the TTL surface (kvcache: ExpiresAt pointer / WithTTL option;
// gazetteer: time.Duration argument to Set).
//
// New wraps a kvcache.Cache so it satisfies gazetteer.Cache,
// collapsing the ~70-LOC adapter that consumers had to ship per
// Source whose Options exposed a gazetteer.Cache field (today:
// the bienici plugin's ZoneCache backend).
//
// Typical wiring:
//
//	import (
//	    "github.com/bpineau/gazetteer/helpers/kvcache/kvgaz"
//	    bienici "github.com/bpineau/gazetteer-fr-plugins/bienici"
//	)
//
//	src := bienici.NewSource(bienici.Options{
//	    Cache: kvgaz.New(myKVCache),
//	    ...
//	})
//
// Behaviour:
//   - Cache misses (kvcache.ErrNotFound) translate to (nil, false, nil).
//   - Other backend errors are propagated unchanged.
//   - Set with ttl=0 stores a row with no expiry (ExpiresAt nil).
//   - Set with ttl>0 stores ExpiresAt = now() + ttl.
//   - Get returns (nil, false, nil) when the row's ExpiresAt is in
//     the past. kvcache's stale-while-revalidate semantics — which
//     keep expired rows reachable — are NOT exposed through
//     gazetteer.Cache, so the adapter enforces the TTL wall clock
//     here. Callers that need stale-while-revalidate should consume
//     kvcache.Cache directly.
//
// Concurrency: as safe as the underlying kvcache.Cache backend.
package kvgaz

import (
	"context"
	"errors"
	"time"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/kvcache"
)

// New wraps backend so it satisfies gazetteer.Cache.
func New(backend kvcache.Cache) gazetteer.Cache {
	return &adapter{
		backend: backend,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

type adapter struct {
	backend kvcache.Cache
	now     func() time.Time
}

// Get implements gazetteer.Cache.
func (a *adapter) Get(ctx context.Context, key string) ([]byte, bool, error) {
	entry, err := a.backend.Get(ctx, key)
	if err != nil {
		if errors.Is(err, kvcache.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if entry.ExpiresAt != nil && !entry.ExpiresAt.IsZero() && a.now().After(*entry.ExpiresAt) {
		return nil, false, nil
	}
	return entry.Value, true, nil
}

// Set implements gazetteer.Cache.
func (a *adapter) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	now := a.now()
	entry := kvcache.Entry{
		Key:       key,
		Value:     value,
		FetchedAt: now,
	}
	if ttl > 0 {
		exp := now.Add(ttl)
		entry.ExpiresAt = &exp
	}
	return a.backend.Set(ctx, entry)
}
