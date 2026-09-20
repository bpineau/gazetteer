package geopoly

import "math"

// Distance to a boundary, in metres.
//
// The obvious cheap answer — the distance to the nearest VERTEX — is wrong by
// up to half an edge length, and administrative boundaries are not drawn at a
// uniform density: a long straight stretch is published as two vertices
// kilometres apart. Measured over the QPV contours this repo embeds, half the
// edges are under 22 m but the longest is 3 961 m, so a point 30 m outside
// such a zone sits 1 981 m from its nearest vertex. A caller asking "is there
// a QPV within 1 000 m" is then told no.
//
// So the distance below is measured to the nearest EDGE, which is the only
// reading that matches what a perimeter means.

// metersPerDegreeLat is the meridional distance of one degree of latitude.
// Constant enough (110.57 km at the equator, 111.69 at the pole) that the
// spherical mean serves every use here.
const metersPerDegreeLat = 111_132.0

// BoundaryDistanceM returns the distance in metres from (lat, lon) to the
// nearest point of the ring's boundary, treating the ring as implicitly
// closed. It is the distance to the nearest EDGE, not to the nearest vertex.
// A ring of fewer than 2 points has no boundary and returns +Inf.
//
// Distances are computed on a local equirectangular projection centred on the
// query point: one degree of latitude is metersPerDegreeLat and one degree of
// longitude that scaled by cos(lat). Over the few kilometres these lookups
// span, that is accurate to well under a metre, and unlike a haversine per
// vertex it costs no trigonometry per edge.
//
// Note what this does NOT tell you: a point deep inside the ring is far from
// the boundary, exactly like a point far outside it. Pair it with Covers when
// inside and outside must be told apart.
func (r Ring) BoundaryDistanceM(lat, lon float64) float64 {
	if len(r) < 2 {
		return math.Inf(1)
	}
	mPerLon := metersPerDegreeLat * math.Cos(lat*math.Pi/180)
	// Near the poles the longitude scale collapses and the projection stops
	// being meaningful; no French territory is there, and the floor keeps the
	// arithmetic finite for anything that is.
	if mPerLon < 1 {
		mPerLon = 1
	}
	x := func(p Point) float64 { return (p.Lon - lon) * mPerLon }
	y := func(p Point) float64 { return (p.Lat - lat) * metersPerDegreeLat }

	best := math.Inf(1)
	j := len(r) - 1
	for i := range r {
		if d := segmentDistanceM(x(r[j]), y(r[j]), x(r[i]), y(r[i])); d < best {
			best = d
		}
		j = i
	}
	return best
}

// BoundaryDistanceM returns the distance in metres from (lat, lon) to the
// nearest edge of any of the polygon's rings, holes included: a point in a
// courtyard is close to the courtyard's own boundary, which is what a
// "distance to the perimeter" question means. +Inf for a polygon with no
// usable ring.
func (poly Polygon) BoundaryDistanceM(lat, lon float64) float64 {
	best := math.Inf(1)
	for _, r := range poly {
		if d := r.BoundaryDistanceM(lat, lon); d < best {
			best = d
		}
	}
	return best
}

// BoundaryDistanceM returns the distance in metres from (lat, lon) to the
// nearest edge of any member polygon. +Inf for an empty MultiPolygon.
func (mp MultiPolygon) BoundaryDistanceM(lat, lon float64) float64 {
	best := math.Inf(1)
	for _, poly := range mp {
		if d := poly.BoundaryDistanceM(lat, lon); d < best {
			best = d
		}
	}
	return best
}

// segmentDistanceM is the distance from the origin to the segment (ax, ay) to
// (bx, by), all in metres on a local planar projection. The classic projection
// onto the segment, clamped to its ends so a point beyond an endpoint measures
// to that endpoint.
func segmentDistanceM(ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	if dx == 0 && dy == 0 {
		return math.Hypot(ax, ay)
	}
	// t is where the foot of the perpendicular falls along the segment, 0 at
	// a and 1 at b; outside [0, 1] the nearest point is the endpoint.
	t := -(ax*dx + ay*dy) / (dx*dx + dy*dy)
	switch {
	case t < 0:
		t = 0
	case t > 1:
		t = 1
	}
	return math.Hypot(ax+t*dx, ay+t*dy)
}
