package cadastre

import (
	"container/list"
	"sync"

	"github.com/bpineau/gazetteer/helpers/geopoly"
)

// BatiPolygon is the cached shape of one building footprint, stored pre-parsed
// so the in-parcel filter doesn't re-pay decode + math costs on subsequent
// Query calls for the same INSEE.
//
// It is kept PER PART rather than as one centroid and one total area, because
// a building is not always one blob: a feature with two wings used to be
// tested for containment on the FIRST wing's centroid and then credited with
// BOTH wings' area. Whether a wing outside the parcel counted therefore
// depended on the order the wings appear in the GeoJSON — same geometry,
// different BatiM2.
type BatiPolygon struct {
	// Geometry is the typed building footprint. Always at least one
	// polygon — empty geometries are dropped at cache-load time.
	Geometry geopoly.MultiPolygon

	// Parts carries one entry per member polygon of Geometry, in the same
	// order. Empty only for a geometry that encloses nothing.
	Parts []BatiPart
}

// BatiPart is one member polygon of a building footprint: a point known to be
// inside it, and its own planar area.
type BatiPart struct {
	// Inside is a point GUARANTEED to lie within this part (via
	// geopoly.Polygon.RepresentativePoint, not its centroid — an L-shaped
	// footprint sits around its centroid, not on it).
	Inside geopoly.Point

	// AreaM2 is this part's planar area in m².
	AreaM2 float64
}

// AreaM2 is the footprint's whole planar area in m², every part summed.
func (b BatiPolygon) AreaM2() float64 {
	var total float64
	for _, p := range b.Parts {
		total += p.AreaM2
	}
	return total
}

// AreaInM2 is the planar area of the parts that lie inside parcel, in m².
// Zero when no part does — which is how the caller tells a building on this
// parcel from one merely nearby in the commune dump.
func (b BatiPolygon) AreaInM2(parcel geopoly.MultiPolygon) float64 {
	var total float64
	for _, p := range b.Parts {
		if parcel.Covers(p.Inside) {
			total += p.AreaM2
		}
	}
	return total
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
