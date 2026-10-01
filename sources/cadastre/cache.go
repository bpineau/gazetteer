package cadastre

import (
	"container/list"
	"sync"

	"github.com/bpineau/gazetteer/helpers/geopoly"
)

// BatiPolygon is the cached shape of one building footprint. Stored
// pre-parsed (typed MultiPolygon + pre-computed centroid + planar
// area) so the centroid PIP filter doesn't re-pay decode + math costs
// on subsequent Query calls for the same INSEE.
type BatiPolygon struct {
	// Geometry is the typed building footprint. Always at least one
	// polygon — empty geometries are dropped at cache-load time.
	Geometry geopoly.MultiPolygon

	// Centroid is the area-weighted centroid of the first polygon —
	// the point we test for parcel containment. Cached for cheap
	// repeated lookups.
	Centroid geopoly.Point

	// AreaM2 is the planar area of the whole MultiPolygon in m². Cached
	// for cheap sum during the in-parcel filter.
	AreaM2 float64
}

// BatiCache is the contract for the per-INSEE building polygon cache.
// Implementations MUST be safe for concurrent use — a single gazetteer
// process can run several Query calls in parallel that may hit the
// same INSEE.
//
// Callers that want a longer-lived (e.g. on-disk) cache plug their own
// implementation via Options.BatiCache; the interface is intentionally
// minimal so a drop-in replacement is straightforward.
type BatiCache interface {
	Get(insee string) (polygons []BatiPolygon, ok bool)
	Put(insee string, polygons []BatiPolygon)
}

// DefaultBatiCacheMaxCommunes is the number of communes DefaultBatiCache
// keeps before it starts evicting. Each entry is a WHOLE commune's building
// footprints (a Paris arrondissement is tens of thousands of polygons, tens
// of MB once parsed), so the ceiling is deliberately small: it holds the
// working set of a batch run over one department or city while keeping a
// long-lived server's footprint bounded and predictable.
const DefaultBatiCacheMaxCommunes = 16

// DefaultBatiCache is the in-process, bounded building-polygon cache used
// when Options.BatiCache is nil.
//
// There is no TTL: the upstream cadastre dump is refreshed monthly, so what
// is cached cannot go stale inside one run. There IS a ceiling, because the
// values are whole-commune dumps: past MaxCommunes entries the
// least-recently-used commune is dropped. A server that geocodes addresses
// all day therefore holds at most MaxCommunes dumps rather than every
// commune it has ever seen.
//
// The zero value is ready to use (and applies DefaultBatiCacheMaxCommunes).
// Safe for concurrent use.
type DefaultBatiCache struct {
	// MaxCommunes overrides DefaultBatiCacheMaxCommunes: the number of
	// per-commune dumps kept before the least-recently-used one is
	// evicted. Set it before the first Get/Put (the cache reads it when it
	// initialises). 0 means DefaultBatiCacheMaxCommunes; a negative value
	// means UNLIMITED, for a short-lived process that would rather trade
	// memory for zero refetches.
	MaxCommunes int

	mu    sync.Mutex
	byIns map[string]*list.Element // insee -> element holding *batiEntry
	lru   *list.List               // front = most recently used
	max   int                      // resolved ceiling; < 0 = unlimited
}

// batiEntry is one commune's cached dump as stored in an LRU element.
type batiEntry struct {
	insee    string
	polygons []BatiPolygon
}

// Get returns the cached polygons for insee, or (nil, false) on miss, and
// marks the commune as most-recently-used.
func (c *DefaultBatiCache) Get(insee string) ([]BatiPolygon, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.byIns[insee]
	if !ok {
		return nil, false
	}
	c.lru.MoveToFront(el)
	return el.Value.(*batiEntry).polygons, true
}

// Put stores polygons under insee, evicting the least-recently-used
// commune when the cache is at its ceiling. The slice header is captured by
// reference: callers must not mutate the slice after Put.
func (c *DefaultBatiCache) Put(insee string, polygons []BatiPolygon) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()
	if el, ok := c.byIns[insee]; ok {
		el.Value.(*batiEntry).polygons = polygons
		c.lru.MoveToFront(el)
		return
	}
	for c.max >= 0 && len(c.byIns) >= c.max {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		delete(c.byIns, oldest.Value.(*batiEntry).insee)
		c.lru.Remove(oldest)
	}
	c.byIns[insee] = c.lru.PushFront(&batiEntry{insee: insee, polygons: polygons})
}

// init lazily prepares the map, the recency list and the resolved ceiling so
// the zero value works. Caller holds mu.
func (c *DefaultBatiCache) init() {
	if c.byIns != nil {
		return
	}
	c.byIns = make(map[string]*list.Element)
	c.lru = list.New()
	c.max = c.MaxCommunes
	if c.max == 0 {
		c.max = DefaultBatiCacheMaxCommunes
	}
}
