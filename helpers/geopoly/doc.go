// Package geopoly is a small, dependency-free geometry kernel for
// GeoJSON-style geometries in (lon, lat) decimal degrees.
//
// Its primary question is "is this point inside this area?", answered with
// the even-odd (ray-casting) rule, with holes handled naturally by
// accumulating ring crossings. Rings are treated as implicitly closed, so the
// first and last vertex need not be repeated. Alongside containment it
// carries the two measurement helpers spatial sources need: Shoelace
// centroids (Ring/Polygon/MultiPolygon Centroid — a representative point,
// not a holed-shape mass centroid) and equirectangular planar areas in m²
// (AreaM2, honest to ~0.5 % for parcel-sized shapes at French latitudes).
//
// The package treats coordinates as a flat Cartesian plane. Over a city-sized
// footprint the planar approximation is well within geocoding precision, so it
// is the right tool for "which administrative zone contains this address?"
// joins (rent-control zones, copro perimeters, noise carreaux). It is NOT a
// geodesic library: do not use it for areas spanning many degrees or straddling
// the antimeridian.
//
// Boundary points (a point lying exactly on an edge or vertex) have undefined
// membership — acceptable because real geocoded coordinates never land exactly
// on a polygon boundary.
//
// Example:
//
//	zone := geopoly.MultiPolygon{{{{2.0, 48.9}, {2.1, 48.9}, {2.1, 49.0}, {2.0, 49.0}}}}
//	zone.Covers(geopoly.Point{Lon: 2.05, Lat: 48.95}) // true
//
// All types are plain slices, so they marshal to and from JSON transparently
// and are safe for concurrent reads.
package geopoly
