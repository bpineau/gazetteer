# geodist — great-circle distances

Haversine distances between (lat, lon) points on Earth, using the IUGG
mean radius (R = 6371.0 km). A thin numerical kernel: no allocation per
call, safe for concurrent use, no dependencies.

This is the distance function behind every "how far is the nearest
station / school / QPV boundary?" answer in the project.

## Quick start

```go
import "github.com/bpineau/gazetteer/helpers/geodist"

km := geodist.KmBetween(48.8566, 2.3522, 45.7640, 4.8357)
// km ≈ 391.5 (Paris ↔ Lyon)

m := geodist.MetersBetween(48.8566, 2.3522, 48.8606, 2.3376)
// m ≈ 1200 (Hôtel de Ville ↔ Louvre)
```

## Public API

See `go doc github.com/bpineau/gazetteer/helpers/geodist`:

- `const EarthRadiusKm = 6371.0`
- `func KmBetween(lat1, lon1, lat2, lon2 float64) float64`
- `func MetersBetween(lat1, lon1, lat2, lon2 float64) float64`

## Design notes

- Haversine is exact on a sphere; against the real (ellipsoidal) Earth
  it is accurate to ~0.3 %, far below geocoding noise for the
  walking-distance and nearest-neighbour uses this project has.
- For point-in-polygon and area questions, see the companion
  `helpers/geopoly`; for indexed containment over many polygons, see
  `helpers/geoindex`.

## Status

Stable. Symbols may be added but not renamed or removed without a
deprecation cycle.
