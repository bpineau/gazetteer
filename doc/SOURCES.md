# Sources reference

Every shipped Source lives under `sources/<name>/` and exposes the
canonical pattern:

- `const Name`         — registry key
- `const Version`      — bumped when logic changes
- `type Options`       — constructor parameters
- `func NewSource(...) (*Source, error)` (or `*Source` when infallible)
- `type Result`        — typed payload returned via `Source.Query`
- `type Evidence`      — reproducibility sidecar (when present)
- `Result.IsEmpty()`   — satisfies `gazetteer.EmptyReporter`
- `init() { gazetteer.Register(Name, func() any { return &Result{} }) }`

The table below summarises each Source. Detailed contracts follow.

| Name             | Inputs from Listing                            | Backend                     |
|------------------|------------------------------------------------|-----------------------------|
| `ademe`          | address / zip                                  | data.gouv.fr `dpe03existant`|
| `bdnb`           | address + INSEE                                | data.gouv.fr BDNB PostgREST |
| `carteloyers`    | INSEE + property_type + rooms                  | offline ANIL / DHUP dataset |
| `dvf`            | INSEE or address + property_type (+ surface)   | data.gouv.fr Etalab DVF     |
| `encadrement`    | zip or INSEE + property_type + rooms (+ surface)| offline DRIHL JSON          |
| `filosofi`       | INSEE                                          | offline INSEE Filosofi 2021 |
| `georisques`     | lat/lon (or address)                           | georisques.gouv.fr report   |
| `locservice`     | INSEE + property_type + rooms                  | locservice.fr HTML scrape   |
| `osm`            | lat/lon + offline station catalog              | OSM Overpass (refresh only) |
| `taxefonciere`   | INSEE + surface_m2                             | offline DGFiP rates         |
| `vacance`        | INSEE                                          | offline LOVAC 2025          |

## `sources/ademe`

DPE (energy-performance certificates) from the ADEME `dpe03existant`
dataset on data.gouv.fr.

- **Needs**: a free-form address. Zip is resolved via the configured
  Geocoder when missing.
- **Result**: `ademe.Result` carries `DPELabel`, `GESLabel`, surface,
  build year, dwelling type and the picked candidate's distance + match
  score (Evidence).
- **`IsEmpty()`**: true when the API returns `results: []`.
- **Eligibility**: residential only; commercial/land returns `Skipped`
  via the typed Result rather than an error.

## `sources/bdnb`

Building-level facts from the Base de Données Nationale des Bâtiments
(year of construction, dwelling count, building type, …).

- **Needs**: address + zip (or INSEE). The PostgREST filter requires a
  5-digit INSEE; the Source resolves it via the BAN cascade.
- **Result**: `bdnb.Result` exposes building age, structure, dwellings,
  parcel surface. `Evidence` records the address pattern and number of
  candidate rows.
- **Quota**: BDNB enforces a per-key rolling 10 000-call quota.
  Operators wire a `helpers/circuit.HTTPFetcher` (see
  [CIRCUIT_BREAKERS.md](CIRCUIT_BREAKERS.md)) to trip the breaker on
  `x-quota-remaining: 0` or HTTP 429.

## `sources/carteloyers`

Reference rents from the national rent observatory (ANIL / DHUP carte
des loyers).

- **Needs**: INSEE + property type + rooms (used to pick a typology
  bucket: House, Apt 1-2 pieces, Apt 3+, generic Apt).
- **Result**: median rent €/m²/month at three typology granularities;
  satisfies `appraisal.RentEstimator`.
- **Fallback**: when the rooms-bucket dataset misses on a commune, the
  Source widens to `TypologyApartment` and stamps
  `Evidence.FallbackToGeneric=true`.

## `sources/dvf`

Demandes de Valeurs Foncières — historical mutation prices from
data.gouv.fr Etalab.

- **Needs**: property type + (INSEE OR a resolvable address); surface
  recommended for €-total estimates.
- **Result**: median, p25, p75 €/m² (in cents); satisfies
  `appraisal.PriceEstimator`.
- **Ladder**: 4-tier `helpers/fallback.Walk`:
  1. `address_radius` — 500 m disk around `(Lat, Lon)`, MinSample 12
  2. `commune` — listing's INSEE
  3. `neighborhood` — commune + its haversine neighbours
  4. `department` — entire département
  The winning tier is recorded in `Evidence.LevelUsed`.
- **Section catalog cache**: the per-INSEE cadastral section list is
  cached via a `kvcache.Cache` (`Options.SectionCache`). See
  [CACHING.md](CACHING.md).
- **Circuit breaker**: 3 consecutive transport errors OR 3 consecutive
  429s trip the breaker. Returns `dvf.ErrCircuitTripped` (matches
  `gazetteer.ErrSourceCircuitTripped`).

## `sources/encadrement`

Zoned rent caps (encadrement des loyers) for Paris, Plaine Commune
and Lyon / Villeurbanne.

- **Needs**: zip OR INSEE + property type + rooms + surface_m2.
- **Result**: a regulated rent value with low/medium/high ceilings;
  satisfies `appraisal.RentEstimator` with `Bracket` populated when a
  legal cap applies.
- **Zone identification**:
  - Paris by zip: 75001..75020, 75116
  - Lyon / Villeurbanne by INSEE: 69381..69389, 69266
  - Plaine Commune currently returns `ConfidenceNone` (no INSEE → zone
    map yet).
- **Eligibility**: residential only.

## `sources/filosofi`

Per-commune income and minima-sociaux statistics from INSEE Filosofi
(2021 vintage).

- **Needs**: INSEE.
- **Result**: median household disposable income, minima-sociaux %,
  risk flag (low / medium / high / unknown).
- Property type is irrelevant — applies to the whole commune.

## `sources/georisques`

Natural and technological hazards from georisques.gouv.fr (BRGM
rapport-risque).

- **Needs**: lat/lon (resolved via Geocoder when absent).
- **Result**: flood / soil / industrial / nuclear hazards, ICPE /
  Seveso classifications, radon level; satisfies
  `appraisal.HazardReporter`.
- **`IsEmpty()`**: true when both Adresse and Commune fields parse
  empty.

## `sources/locservice`

Rental-market tension labels from locservice.fr.

- **Needs**: INSEE + property type + rooms (for the logement keyword).
- **Result**: tension label (`tendu` / `équilibré` / `détendu`) plus
  median rent reading; `Evidence.FellBack=true` when the
  logement-specific call returned no data and the Source widened to
  the commune-wide call.

## `sources/osm`

Walking distance to the nearest métro / RER / tram / Transilien
station, from an OpenStreetMap Overpass extract.

- **Needs**: lat/lon AND a non-empty offline catalog.
- **Catalog**: installed at `Source` construction (`Options.Catalog`)
  or hot-swapped later via `Source.UpdateCatalog`. Empty catalogs are
  ignored so a failed background refresh cannot silently discard a
  loaded one.
- **Result**: nearest station name, type, lines, walk distance (m) and
  walk minutes. `SkipReasonOutOfRange` is set when the closest station
  is beyond `MaxNearestStationMeters` (5 000 m great-circle).
- **`ErrNoCatalog`**: transient — `UpdateCatalog` with a non-empty
  catalog makes the next Query succeed.
- **Refresh**: out-of-band via `osm.NewCatalogFetcher(...)` and
  `Fetcher.Fetch(ctx, dir)`; the resulting `*Catalog` is what
  `UpdateCatalog` consumes.

## `sources/taxefonciere`

Per-commune `taxe foncière` estimate.

- **Needs**: INSEE + surface_m2.
- **Result**: estimated annual `taxe foncière` in cents, broken down
  into TFPB and TEOM portions; confidence reflects whether the
  V2 (DGFiP voted rates × VLC × surface) or V1 (legacy per-m² ratio)
  path applied, and whether the lookup hit the commune or fell back to
  the department.

## `sources/vacance`

Per-commune vacancy rate from the LOVAC 2025 dataset.

- **Needs**: INSEE.
- **Result**: vacancy rate %, long-term vacancy split. Missing
  communes (secret statistique) surface as `IsEmpty()`.

## Cross-cutting conventions

### `Evidence` sidecars

Every Source's typed `Result` carries an `Evidence` field tagged
`json:"-"`. It captures resolver provenance, the winning ladder tier,
sample sizes, fallback flags and any input-side fingerprint a
downstream auditor needs. Sources that implement
`gazetteer.Evidencer` expose the same value on `Result.Evidence` of
the framework envelope.

### Appraisal contributions

A Source's typed `Result` MAY implement:

- `appraisal.PriceEstimator` — contributes to `appraisal.PricePerM2`
- `appraisal.RentEstimator`  — contributes to `appraisal.RentValue`
- `appraisal.HazardReporter` — contributes to `appraisal.HazardProfile`

Today: `dvf` → PriceEstimator; `carteloyers` + `encadrement` →
RentEstimator; `georisques` → HazardReporter.

### Tests via `Options.BaseURL`

Sources whose Options struct exposes a `BaseURL` field are wired to a
local `httptest.NewServer` in tests. The Source's `Options.BaseURL`
or the corresponding package-level `BaseURL` var (when blank) is read
at Query time, so a single change at the constructor level swaps the
upstream cleanly. See [TESTING.md](TESTING.md).
