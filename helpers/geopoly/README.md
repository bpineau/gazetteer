# geopoly — point-in-polygon geometry kernel

A small, dependency-free geometry kernel for GeoJSON-style geometries in
(lon, lat) decimal degrees. Its primary question is "is this point
inside this area?", answered with the even-odd (ray-casting) rule, holes
handled naturally. Alongside containment it carries the two measurement
helpers spatial sources need: Shoelace centroids and equirectangular
planar areas in m².

This kernel powers every administrative-zone join in the project:
rent-control zones, QPV and QRR perimeters, IRIS contours, noise
carreaux.

## Quick start

```go
import "github.com/bpineau/gazetteer/helpers/geopoly"

zone := geopoly.MultiPolygon{{{{2.0, 48.9}, {2.1, 48.9}, {2.1, 49.0}, {2.0, 49.0}}}}

zone.Covers(geopoly.Point{Lon: 2.05, Lat: 48.95}) // true
c := zone.Centroid()                              // representative point
a := zone.AreaM2()                                // planar m², ~0.5 % honest
```

All types are plain slices (`Ring []Point`, `Polygon []Ring`,
`MultiPolygon []Polygon`), so they marshal to and from JSON
transparently and are safe for concurrent reads. Rings are implicitly
closed: the first and last vertex need not be repeated.

## Scope and non-goals

- Coordinates are treated as a flat Cartesian plane. Over a city-sized
  footprint the planar approximation is well within geocoding
  precision, which is exactly the "which zone contains this address?"
  regime.
- It is NOT a geodesic library: do not use it for areas spanning many
  degrees or straddling the antimeridian.
- Boundary points (exactly on an edge or vertex) have undefined
  membership; real geocoded coordinates never land there.
- Centroids are Shoelace representative points, not mass centroids of
  holed shapes.

## Public API

See `go doc github.com/bpineau/gazetteer/helpers/geopoly`:

- `type Point / Ring / Polygon / MultiPolygon` with `Covers`,
  `Centroid`, `AreaM2`, `Bound`
- `type BBox` for cheap prefiltering
- `const EarthRadiusM`

For the "embedded gzipped index of many polygons" layer above this
kernel, see `helpers/geoindex`.

## Status

Stable. Symbols may be added but not renamed or removed without a
deprecation cycle.
