package gazetteer

import (
	"github.com/bpineau/gazetteer/helpers/kvcache"
	"github.com/bpineau/gazetteer/helpers/kvcache/memcache"
)

// NewKVMemCache returns a fresh in-memory kvcache.Cache. Convenience
// re-export of helpers/kvcache/memcache.New so callers wiring a Source
// that consumes kvcache.Cache (DVF, banx, any rental-ladder plugin)
// don't need a second import line just to obtain a sensible default
// backend.
//
// Persistence: none. Suitable for one-shot tools, tests, and short
// processes. Long-running batch consumers should plug a persistent
// backend (e.g. a SQL-backed adapter) and pass its kvcache.Cache
// here instead.
func NewKVMemCache() kvcache.Cache {
	return memcache.New()
}
