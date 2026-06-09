package dvf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bpineau/gazetteer/helpers/geopoly"
	"github.com/bpineau/gazetteer/helpers/kvcache"
	"github.com/bpineau/gazetteer/helpers/safejson"
)

// SectionTTL is the cache TTL for the per-commune section list. Sections
// almost never change so we keep them for 90 days.
const SectionTTL = 90 * 24 * time.Hour

// CacheKeyPrefix is the kv_cache key prefix for the per-commune section
// list. The `v2:` segment exists to invalidate legacy "all 676 sections
// exist" caches built before the API contract change was detected.
// Bump this constant whenever the discovery semantics change.
const CacheKeyPrefix = "dvf:sections:v2:"

// GeoCacheKeyPrefix is the kv_cache key prefix for the per-commune
// reduced section-geometry list ([]SectionGeo: code + bbox — a few
// hundred bytes, versus the hundreds-of-KB raw GeoJSON it is derived
// from). Built like the SectionDiscoverer's code-list key
// (prefix + insee), but the version segment embeds the Source Version
// so any logic bump also invalidates persisted geo entries.
var GeoCacheKeyPrefix = fmt.Sprintf("dvf:sectiongeos:v%d:", Version)

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

// sectionGeoWire is the JSON encoding of one SectionGeo in the kv_cache.
// A box of unknown extent (the inverted-infinity emptyBBox — ±Inf is not
// representable in JSON) is flagged via Empty instead of serialising the
// coordinates, and restored to emptyBBox() on read so the "unknown
// extent ⇒ keep the section" prefilter semantics survive the round-trip.
type sectionGeoWire struct {
	Code   string  `json:"code"`
	Empty  bool    `json:"empty,omitempty"`
	MinLon float64 `json:"min_lon"`
	MinLat float64 `json:"min_lat"`
	MaxLon float64 `json:"max_lon"`
	MaxLat float64 `json:"max_lat"`
}

// GeosForCommune returns the cached reduced section geometries (code +
// bbox) for the commune with `insee` from the KV cache. Returns
// (nil, nil) on a cache miss, an expired row or an undecodable value —
// the caller should re-fetch via FetchCadastreSectionGeos and re-prime
// with PrimeGeos. Mirrors SectionsForCommune's contract.
func (d *SectionDiscoverer) GeosForCommune(ctx context.Context, insee string) ([]SectionGeo, error) {
	row, err := d.cache.Get(ctx, GeoCacheKeyPrefix+insee)
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
	var wire []sectionGeoWire
	if err := json.Unmarshal(row.Value, &wire); err != nil {
		return nil, nil
	}
	out := make([]SectionGeo, 0, len(wire))
	for _, w := range wire {
		g := SectionGeo{Code: w.Code}
		if w.Empty {
			g.Box = emptyBBox()
		} else {
			g.Box = geopoly.BBox{MinLon: w.MinLon, MinLat: w.MinLat, MaxLon: w.MaxLon, MaxLat: w.MaxLat}
		}
		out = append(out, g)
	}
	return out, nil
}

// PrimeGeos seeds the cache with the reduced section geometries for the
// commune (typically from FetchCadastreSectionGeos). Same TTL as the
// code list — section geometry changes as rarely as the section set.
func (d *SectionDiscoverer) PrimeGeos(ctx context.Context, insee string, geos []SectionGeo) error {
	wire := make([]sectionGeoWire, 0, len(geos))
	for _, g := range geos {
		w := sectionGeoWire{Code: g.Code}
		if bboxEmpty(g.Box) {
			w.Empty = true
		} else {
			w.MinLon, w.MinLat, w.MaxLon, w.MaxLat = g.Box.MinLon, g.Box.MinLat, g.Box.MaxLon, g.Box.MaxLat
		}
		wire = append(wire, w)
	}
	exp := d.now().Add(SectionTTL)
	return d.cache.Set(ctx, kvcache.Entry{
		Key:       GeoCacheKeyPrefix + insee,
		Value:     safejson.MustMarshal(wire),
		FetchedAt: d.now(),
		ExpiresAt: &exp,
	})
}
