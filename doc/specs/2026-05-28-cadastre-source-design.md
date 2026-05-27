# Cadastre source — design

Date: 2026-05-28
Status: approved (Ben), ready for implementation

## Goal

Add a new `cadastre` gazetteer Source that, given a Listing with usable
lat/lon, returns the cadastral parcel containing the address point and
— opt-in — a footprint analysis of the buildings sitting on that
parcel.

Primary user: a real-estate investor (locatif or marchand de bien)
studying a property. Parcel surface (contenance), parcel ID and a
direct link to the Etalab cadastre viewer are the load-bearing
signals. Building footprint and emprise ratio are a secondary
deliverable, useful to gauge unbuilt land available on the parcel.

## Scope

In scope
- Identify the parcel under the listing's lat/lon via the public
  API Carto IGN endpoint (`apicarto.ign.fr/api/cadastre/parcelle`).
- Expose contenance in m², ares and hectares.
- Expose a clickable link to the Etalab cadastre viewer
  (`https://cadastre.data.gouv.fr/map?style=ortho&parcelleId=…`).
- Opt-in: count and total footprint area of buildings on the parcel,
  computed locally against the PCI bâtiments dump from
  `cadastre.data.gouv.fr/bundler/cadastre-etalab/communes/{insee}/geojson/batiments`.

Out of scope (v2 candidates)
- Adjacent parcels (marchand-de-bien view).
- Multi-parcels for a single building spread across parcels.
- Persistent (disk) cache of bâtiment dumps — in-memory per process
  for v1.
- Polygon-intersection-based bâti area (v1 uses centroid PIP + raw
  building area, which is a small but acceptable over-estimate when a
  building straddles a parcel boundary).
- Livre Foncier areas (67/68/57) and Mayotte where API Carto
  coverage is partial or absent — handled gracefully as
  `StatusOKEmpty`.

## Inputs / outputs

Required from Listing
- `Listing.Lat`, `Listing.Lon` non-nil and not both zero. Else
  `gazetteer.ErrInsufficientInputs`.

Optional from Listing
- `Listing.INSEE` — when present, skips a re-lookup of the commune
  from the parcel response (the API Carto response carries
  `code_insee` so this is purely an optimization for evidence logging).

## API endpoints

### API Carto IGN — parcelle

`GET https://apicarto.ign.fr/api/cadastre/parcelle?geom={GeoJSON-Point}`

The `geom` parameter takes a URL-encoded GeoJSON geometry, e.g.
`{"type":"Point","coordinates":[2.3522,48.8566]}`. Returns a GeoJSON
FeatureCollection with zero, one or rarely several features. We use the
single feature that contains the point (in practice always exactly one
when the point falls on cadastered land).

Each feature's `properties` carries (relevant fields):
- `code_insee` (5)
- `prefixe` (3, typically `"000"`)
- `section` (1–2 chars, e.g. `"A"`, `"BB"`)
- `numero` (1–4 chars, zero-padded only in the composed id)
- `contenance` (int, m²)
- `code_dep`, `code_com` — redundant with INSEE; not used.
- `idu` — 14-char parcel id, when present (sometimes absent on older
  records). When absent we recompose it from INSEE + prefixe + section + numero.

Failure modes
- HTTP 5xx / transport / parse → `ErrUpstreamUnavailable`.
- HTTP 4xx other than 404 → `ErrUpstreamPermanent`.
- HTTP 404 or empty FeatureCollection → empty Result + `StatusOKEmpty`.

### cadastre.data.gouv.fr — bâtiments dump (opt-in)

`GET https://cadastre.data.gouv.fr/bundler/cadastre-etalab/communes/{insee}/geojson/batiments`

Returns a GeoJSON FeatureCollection of building polygons for the commune.
Each feature's geometry is `Polygon` or `MultiPolygon`. Properties are
sparse (`commune`, dates) and not used.

Failure modes
- HTTP 404 → no buildings; Result keeps `BatiM2 = nil`, log debug.
- HTTP 5xx / transport / parse → bâti enrichment skipped, parcel data
  is still returned; Evidence records `BatiError`.

The dump is several MB per commune and is fetched at most once per
process per INSEE — see Cache below.

## Result schema

```go
type Result struct {
    Parcels      []Parcel `json:"parcels"`
    BatiM2       *float64 `json:"bati_m2_on_parcel,omitempty"`
    BatiCount    *int     `json:"bati_count_on_parcel,omitempty"`
    EmpriseRatio *float64 `json:"emprise_ratio,omitempty"`
    Evidence     Evidence `json:"-"`
}

type Parcel struct {
    ID             string  `json:"id"`              // "78005000BB0285"
    INSEE          string  `json:"insee"`           // "78005"
    Prefixe        string  `json:"prefixe"`         // "000"
    Section        string  `json:"section"`         // "BB"
    Numero         string  `json:"numero"`          // "0285"
    ContenanceM2   int     `json:"contenance_m2"`   // 432
    ContenanceAres float64 `json:"contenance_ares"` // 4.32
    ContenanceHa   float64 `json:"contenance_ha"`   // 0.0432
    MapURL         string  `json:"map_url"`         // https://cadastre.data.gouv.fr/map?style=ortho&parcelleId=78005000BB0285
}

type Evidence struct {
    Lat            float64 `json:"lat"`
    Lon            float64 `json:"lon"`
    ParcelleAPIURL string  `json:"parcelle_api_url"`
    BatiBaseURL    string  `json:"bati_base_url,omitempty"`
    BatiCount      int     `json:"bati_raw_count,omitempty"`  // before parcel filter
    BatiCached     bool    `json:"bati_cached,omitempty"`     // hit on in-memory cache
    BatiError      string  `json:"bati_error,omitempty"`      // soft-failed bâti enrichment
}
```

- `Result.IsEmpty()` ⇔ `len(Parcels) == 0`.
- `Parcels` is a slice with 0 or 1 element in v1 — leaves the door open
  for multi-parcels in a future version without changing the wire
  contract.
- `ID` is the 14-char Etalab id format: `INSEE(5) + prefixe(3) +
  section(2, left-zero-padded) + numero(4, left-zero-padded)`.
- The triple m² / ares / ha is exposed redundantly per Q5 — investor
  ergonomics over normalization purity.
- `MapURL` is built unconditionally when we have a Parcel.

## Options

```go
type Options struct {
    BaseURL     string         // default: package var BaseURL → apicarto.ign.fr
    BatiBaseURL string         // default: package var BatiBaseURL → cadastre.data.gouv.fr/bundler/...
    IncludeBati bool           // opt-in: B (building footprint) computation
    HTTPClient  *http.Client   // nil → gazetteer.HTTPClientFrom(ctx)
    BatiCache   BatiCache      // nil → in-process sync.Map keyed by INSEE
}

type BatiCache interface {
    Get(insee string) (polygons []BatiPolygon, ok bool)
    Put(insee string, polygons []BatiPolygon)
}
```

Convention mirrored on every existing source (`georisques`, `bdnb`):
zero value is usable; `BaseURL` is a `var` so `httptest.NewServer.URL`
can override it via `Options.BaseURL` without touching package state
(race-safe under `-race`).

## Query flow

```
1.  Validate lat/lon — else ErrInsufficientInputs.
2.  Build the GeoJSON Point, call API Carto IGN parcelle endpoint.
3.  Parse FeatureCollection.
3a.   0 features → empty *Result + nil → StatusOKEmpty.
3b.   1+ feature → pick the first whose polygon contains (lon,lat);
       fall back to the first feature if none claim the point.
4.  Build Parcel{ID, INSEE, ..., contenance trio, MapURL}.
5.  If !IncludeBati → return.
6.  Get bâti polygons for Parcel.INSEE:
6a.   Cache hit (in-mem) → reuse, set Evidence.BatiCached.
6b.   Miss → fetch dump, parse, store. On any error, log debug,
       stamp Evidence.BatiError, return with bâti fields nil.
7.  Filter buildings whose centroid is inside the parcel polygon.
8.  Sum their planar areas (Shoelace on lat/lon, with a small
       equirectangular projection to keep m² accurate at French
       latitudes). EmpriseRatio = BatiM2 / ContenanceM2.
9.  Return *Result.
```

### Note on geometry math

We do not pull a heavy geo dependency. The package ships a tiny
internal geom package with:
- `pointInRing(point, ring []Point) bool` — ray-casting.
- `pointInPolygon(point, polygon Polygon) bool` — outer ring inside,
  no inner rings (cadastre parcels never have holes — confirmed by
  cadastre.data.gouv.fr docs).
- `pointInMultiPolygon(point, mp MultiPolygon) bool`.
- `centroid(polygon Polygon) Point` — area-weighted centroid via
  Shoelace.
- `polygonAreaM2(polygon Polygon) float64` — equirectangular
  projection at the polygon centroid's latitude, then Shoelace.

These are ~150 lines of straightforward Go, fully unit-tested.

## Error mapping

| Condition                                       | Wrapped sentinel              | Framework Status          |
|-------------------------------------------------|-------------------------------|---------------------------|
| Listing.Lat/Lon nil or both 0                   | ErrInsufficientInputs         | StatusSkippedPrereq       |
| API Carto URL build (NaN, out-of-range coords)  | ErrInsufficientInputs         | StatusSkippedPrereq       |
| API Carto HTTP 5xx / transport / parse          | ErrUpstreamUnavailable        | StatusFailedTransient     |
| API Carto HTTP 404 / empty FC                   | (nil err, IsEmpty Result)     | StatusOKEmpty             |
| API Carto HTTP 4xx other than 404               | ErrUpstreamPermanent          | StatusFailedPermanent     |
| Bâti enrichment HTTP/parse failure              | (no err, Evidence.BatiError)  | StatusOK (no degradation) |

The bâti path is intentionally soft-failing: a parcel without bâti is
still a useful answer; we never let a heavy optional sub-fetch break
the cheap primary fetch.

## Cache

`DefaultBatiCache` is a private `sync.Map[string][]BatiPolygon`. No
TTL — a single gazetteer process is short-lived enough that the
underlying cadastre data (refresh monthly) cannot meaningfully change
during a run.

Callers that want a longer-lived cache wire their own implementation
via `Options.BatiCache`. The interface is intentionally minimal so an
out-of-tree disk cache can drop in.

## Source registration & factory wiring

- `init()` calls `gazetteer.Register(Name, func() any { return &Result{} })`
  per existing convention.
- `factory.BuilderDefault` appends
  `cadastre.NewSource(cadastre.Options{IncludeBati: false})` to the
  default builder. Bâti is opt-in even at the framework level — a
  caller that wants it constructs its own Builder and adds a
  `cadastre.NewSource(cadastre.Options{IncludeBati: true})` in place
  of the default.

## CLI

The CLI auto-discovers sources via the factory. Existing
`gazetteer query` and `gazetteer appraise` calls will start returning
the `cadastre` entry in their Dossier without code changes.

## Testing plan

Unit tests, all `t.Parallel()` (I/O-bound; lines up with memory
`test_parallel_only_io_bound.md`):

- `url_test.go` — Point GeoJSON encoding, percent-encoding, error
  cases for NaN/out-of-range/0,0 coords. Lock `geom=` as the param
  name and the order `[lon, lat]`.
- `parser_test.go` — fixture-driven, decodes a recorded API Carto
  response (Paris 1er + a small commune with 1-char section).
- `id_test.go` — `ParcelID(INSEE, prefixe, section, numero)` padding
  matrix; `MapURL(id)`.
- `geom/*_test.go` — point-in-ring, point-in-polygon, polygon area
  (golden-tested against known geometries: a 100×100 m square at
  latitude 48.85 must yield ~10 000 m² within 0.5 %).
- `bati_test.go` — synthetic FeatureCollection with 4 polygons, only
  2 with centroid inside the test parcel; assert filtered count and
  summed area.
- `source_test.go` — end-to-end, dual `httptest.Server` (parcelle +
  bâtiments) wired via `Options.BaseURL` + `Options.BatiBaseURL`.
  Covers: happy path (no bâti), happy path with bâti, 404 → empty,
  500 → ErrUpstreamUnavailable, missing lat/lon → ErrInsufficientInputs,
  bâti soft-failure stamps Evidence.BatiError.
- `cache_test.go` — concurrent Get/Put under `-race`, no allocation
  on hit.

Acceptance criteria
- `go vet ./...` clean.
- `go test ./... -race -timeout 120s` green.
- `gofmt -w . && goimports -w .` no diff.
- README + doc/sources.md updated to list the new source.

## Out-of-tree compatibility

The Source uses only stdlib + `helpers/banx` (for the geocoder
interface) + `helpers/httpx` (for the shared client). No new
dependency in go.mod.

## Migration notes

None — purely additive. No existing Source touches the same Dossier
key.
