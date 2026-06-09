package cadastre

import (
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

// DefaultBatiCache is the in-process sync.Map implementation used when
// Options.BatiCache is nil. No TTL — a single gazetteer process is
// short-lived enough that the underlying cadastre data (refreshed
// monthly upstream) cannot meaningfully change during a run.
type DefaultBatiCache struct {
	m sync.Map // insee → []BatiPolygon
}

// Get returns the cached polygons for insee, or (nil, false) on miss.
func (c *DefaultBatiCache) Get(insee string) ([]BatiPolygon, bool) {
	v, ok := c.m.Load(insee)
	if !ok {
		return nil, false
	}
	polys, ok := v.([]BatiPolygon)
	if !ok {
		return nil, false
	}
	return polys, true
}

// Put stores polygons under insee. The slice header is captured by
// reference — callers must not mutate the slice after Put.
func (c *DefaultBatiCache) Put(insee string, polygons []BatiPolygon) {
	c.m.Store(insee, polygons)
}
