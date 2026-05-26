package dvf

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/bpineau/gazetteer/helpers/kvcache"
	"github.com/bpineau/gazetteer/helpers/safejson"
)

// SectionTTL is the cache TTL for the per-commune section list. Sections
// almost never change so we keep them for 90 days.
const SectionTTL = 90 * 24 * time.Hour

// CacheKeyPrefix is the kv_cache key prefix for the per-commune section
// list. Bumped to v2 (2026-05-02) to invalidate the legacy "all
// 676 sections exist" caches built before the API contract change was
// detected. Bump this constant whenever the discovery semantics change.
const CacheKeyPrefix = "dvf:sections:v2:"

// SectionDiscoverer manages the per-commune cadastral section cache.
// Sections are populated by FetchCadastreSections (cadastre.data.gouv.fr)
// and persisted via a kvcache.Cache (in production: the bun-backed
// kv_cache table). The legacy 000AA..000ZZ brute-force walker has been
// removed — the cadastre primer covers 100 % of communes.
type SectionDiscoverer struct {
	cache  kvcache.Cache
	logger *slog.Logger
	now    func() time.Time
}

// NewSectionDiscoverer builds a discoverer over the given cache. The
// cache backend is opaque; callers wire either the in-memory
// kvcache/memcache (tests) or a bun-backed adapter (prod).
func NewSectionDiscoverer(c kvcache.Cache, logger *slog.Logger) *SectionDiscoverer {
	if logger == nil {
		logger = slog.Default()
	}
	return &SectionDiscoverer{
		cache:  c,
		logger: logger.With(slog.String("comp", "dvf.sections")),
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// SectionsForCommune returns the cadastral section codes (e.g. "000AD",
// "000BJ") known for the commune with `insee` from the KV cache.
// Returns nil, nil on a cache miss (caller should prime via
// FetchCadastreSections + PrimeFromList).
func (d *SectionDiscoverer) SectionsForCommune(ctx context.Context, insee string) ([]string, error) {
	key := CacheKeyPrefix + insee
	row, err := d.cache.Get(ctx, key)
	if err != nil {
		if errors.Is(err, kvcache.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if row.ExpiresAt != nil && !d.now().Before(*row.ExpiresAt) {
		// Expired — treat as a miss so the caller re-fetches from cadastre.
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(row.Value, &out); err != nil {
		return nil, nil
	}
	return out, nil
}

// PrimeFromList seeds the cache with a known list of sections
// (e.g. from FetchCadastreSections or a fixture file).
func (d *SectionDiscoverer) PrimeFromList(ctx context.Context, insee string, sections []string) error {
	key := CacheKeyPrefix + insee
	exp := d.now().Add(SectionTTL)
	return d.cache.Set(ctx, kvcache.Entry{
		Key:       key,
		Value:     safejson.MustMarshal(sections),
		FetchedAt: d.now(),
		ExpiresAt: &exp,
	})
}
