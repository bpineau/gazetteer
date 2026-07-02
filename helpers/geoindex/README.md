# geoindex — embedded polygon index + point-in-polygon resolve

The scaffolding shared by every contour-backed source (iris, qpv,
sensible, encadrement): a compact wire shape for shipping polygons as
gzipped JSON, and a generic in-memory index that answers "which feature
covers this point?" with a bbox prefilter.

It sits one layer above `helpers/geopoly`: geopoly owns the math
(`Covers` / `Bound` / `BBox`), geoindex owns the plumbing every source
built on top of it had hand-rolled identically.

## The two concerns

- **Build time** (a source's transform): `Compact` is the wire shape
  `[polygon][ring][vertex][lon,lat]`; `DecodeGeoJSONGeometry` converts a
  GeoJSON Polygon/MultiPolygon to it (rounding coordinates), and
  `FromMultiPolygon` / `RoundCompact` convert and round. The shape is
  deliberately identical to the historical per-source versions, so
  committed `*.json.gz` artifacts decode byte-for-byte.
- **Query time** (a source's loader): `Index[T]` is a generic,
  payload-parameterised bag of `Feature[T]`s with a bbox-prefiltered
  first-cover `Resolve` scan, `ResolveWhere` for a candidate predicate,
  and `Nearest` for the vertex-distance fallback.

The payload is a type parameter because each source carries different
per-feature data (iris `{code,nom,typ}`, qpv `{code,label}`, encadrement
`{ept,zone,insee,commune}`). Only the geometry plumbing is shared.

## Quick start

```go
import "github.com/bpineau/gazetteer/helpers/geoindex"

type zone struct{ Code, Label string }

feats := []geoindex.Feature[zone]{
    geoindex.NewFeature(zone{"A", "centre"}, mpA),
    geoindex.NewFeature(zone{"B", "nord"}, mpB),
}
idx := geoindex.New(feats)

if z, ok := idx.Resolve(48.86, 2.35); ok { // (lat, lon)
    fmt.Println(z.Code) // first feature (in input order) covering the point
}

// Vertex-distance fallback when no polygon covers the point:
if z, meters, ok := idx.Nearest(48.86, 2.35, 500); ok {
    fmt.Printf("%s at %.0f m\n", z.Code, meters)
}
```

Feature order is the tie-breaker: the source controls the slice order
before building the Index, so first-cover ties resolve
deterministically.

## Public API

See `go doc github.com/bpineau/gazetteer/helpers/geoindex`:

- `type Compact`, `func DecodeGeoJSONGeometry`, `FromMultiPolygon`,
  `RoundCompact`
- `type Feature[T any]`, `func NewFeature`
- `type Index[T any]`, `func New`, methods `Resolve`, `ResolveWhere`,
  `Nearest`

## Status

Stable. Symbols may be added but not renamed or removed without a
deprecation cycle.
