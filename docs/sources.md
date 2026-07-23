# Sources reference

The Sources are what this library exists to provide: each returns a fully-typed
`Result` for one dimension of a property's investment evaluation. **This page +
the per-source `go doc` are the data dictionary** — for field-by-field meanings
and units, read `go doc github.com/bpineau/gazetteer/sources/<name> Result`, or
`gazetteer sources catalog --json` for the machine-readable map.

This page documents the typed `Result` of each Source at the field level: every
entry names the **load-bearing fields with their unit** so you can consume a
Source directly, and `go doc` has the exhaustive list. Convention: the unit
lives in the field name (cents vs euros, `…EURPerM2` vs `…EURPerM2HC`
€/m²/month, `…Pct` %, `…M2`, counts).

Every shipped Source lives under `sources/<name>/` and exposes the
canonical pattern:

- `const Name`         — registry key
- `const Version`      — bumped when logic changes
- `type Options`       — constructor parameters
- `func NewSource(Options) *Source` — infallible, zero-value Options usable
- `type Result`        — typed payload returned via `Source.Query`
- `type Evidence`      — reproducibility sidecar (when present)
- `Result.IsEmpty()`   — satisfies `gazetteer.EmptyReporter`
- `init() { gazetteer.Register(Name, func() any { return &Result{} }) }`

The table below summarises each Source. Detailed contracts follow.

| Name             | Inputs from Listing                            | Backend                     |
|------------------|------------------------------------------------|-----------------------------|
| `ademe`          | address / zip                                  | data.gouv.fr `dpe03existant`|
| `anct`           | INSEE                                          | offline ANCT programme list |
| `bdnb`           | address + INSEE                                | data.gouv.fr BDNB PostgREST |
| `bpe`            | INSEE                                          | offline INSEE BPE 2024 subset |
| `cadastre`       | lat/lon                                        | apicarto.ign.fr + cadastre.data.gouv.fr |
| `carteloyers`    | INSEE + property_type + rooms                  | offline ANIL / DHUP dataset |
| `dvfagg`         | INSEE                                          | offline geo-dvf price aggregate |
| `cartofriches`   | INSEE                                          | offline Cerema brownfields  |
| `chomage`        | INSEE                                          | offline INSEE chômage ZE2020|
| `delinquance`    | INSEE                                          | offline SSMSI État 4001     |
| `iris`           | lat/lon (or pre-resolved Listing.IRIS)         | offline IDF IRIS contours     |
| `catnat`         | INSEE                                          | offline GASPAR CatNat agg.   |
| `nuisances`      | lat/lon                                        | offline IDF 500m nuisance grid|
| `cdsr`           | lat/lon                                        | offline IDF CDSR snapshot    |
| `oll`            | INSEE (+ rooms)                                | offline OLL observed rents   |
| `dpedist`        | INSEE                                          | data.ademe.fr values_agg API|
| `dvf`            | INSEE or address + property_type (+ surface)   | data.gouv.fr Etalab DVF     |
| `education`      | INSEE                                          | data.education.gouv.fr API  |
| `encadrement`    | zip/INSEE + property_type + rooms (+ lat/lon for 93)| offline barème + zonage |
| `filosofi`       | INSEE                                          | offline INSEE Filosofi 2021 |
| `filoiris`       | `Listing.IRIS`                                 | offline INSEE Filosofi 2021 IRIS |
| `logiris`        | `Listing.IRIS`                                 | offline INSEE RP 2021 logement IRIS (IDF) |
| `georisques`     | lat/lon (or address)                           | georisques.gouv.fr report   |
| `ips_ecoles`     | INSEE (arrondissement-aware)                   | offline DEPP IPS 2024-2025  |
| `locservice`     | INSEE + property_type + rooms                  | locservice.fr HTML scrape   |
| `osm_transit`    | lat/lon + offline station catalog              | OSM Overpass (refresh only) |
| `gpe`            | lat/lon                                        | offline Grand Paris Express stations (SGP) |
| `qpv`            | INSEE + lat/lon (point-in-polygon)             | offline ANCT QPV 2024 contours |
| `rpls`           | INSEE                                          | offline data.gouv SRU 2024  |
| `sensible`       | lat/lon                                        | offline QRR contours (min. Intérieur) + ORCOD-IN décrets |
| `sitadel`        | INSEE (arrondissement-folding)                 | offline SDES Sitadel 2026-06 |
| `taxefonciere`   | INSEE + surface_m2                             | offline DGFiP rates         |
| `lovac`          | INSEE                                          | offline LOVAC 2025 (fiscal) |
| `vacance`        | INSEE (arrondissement-aware)                   | offline INSEE RP 2021       |
| `zonageabc`      | INSEE                                          | offline arrêté 2025-09-05   |
| `zonetendue`     | INSEE                                          | offline décret 2013-392     |
| `links`          | lat/lon (or INSEE / address)                   | built in-process (no backend)|

## `sources/ademe`

DPE (energy-performance certificates) from the ADEME `dpe03existant`
dataset on data.gouv.fr.

- **Needs**: a free-form address. Zip is resolved via the configured
  Geocoder when missing.
- **Result**: `DPE` (sub-struct: DPE letter + GES letter), `Logement`
  (surface, build year, dwelling type), `Adresse` (BAN-normalised picked
  candidate), `Confidence` and `SampleSize` (1 when a row was picked, 0 on a
  skipped/empty result). The picked candidate's distance + match score live in
  `Evidence`.
- **`IsEmpty()`**: true when the API returns `results: []`.
- **Eligibility**: residential only; commercial/land returns `Skipped`
  via the typed Result rather than an error.

## `sources/bdnb`

Building-level facts from the Base de Données Nationale des Bâtiments
(year of construction, dwelling count, building type, …).

- **Needs**: address + zip (or INSEE). The PostgREST filter requires a
  5-digit INSEE; the Source resolves it via the BAN cascade.
- **Result**: `Building` (sub-struct: year of construction, dwelling count,
  floors, emprise-sol surface, height), `DPE` (class, conso, emissions,
  isolation), `Risks` (monument-historique proximity, ABF perimeter, PLU
  patrimonial, QPV), `Identity` (batiment_groupe id + normalised address),
  `Confidence` and `SampleSize`. Each sub-struct is a nil pointer when the
  picked row carried none of its fields. `Evidence` records the address pattern
  and number of candidate rows.
- **Quota**: BDNB enforces a per-key rolling 10 000-call quota.
  Callers wire a `helpers/circuit.HTTPFetcher` (see
  [circuit_breakers.md](circuit_breakers.md)) to trip the breaker on
  `x-quota-remaining: 0` or HTTP 429.

## `sources/cadastre`

French cadastral parcel under a listing's lat/lon, with an optional
building-footprint analysis (count + total emprise + ratio).

- **Needs**: lat/lon (resolved via Geocoder when absent).
- **Result**: `cadastre.Result` carries a one-element `Parcels` slice
  with the 14-char Etalab id, contenance in m² / ares / hectares and
  a deeplink to the Etalab cadastre viewer. When `IncludeBati: true`,
  also carries `BatiM2`, `BatiCount`, `EmpriseRatio`.
- **`IsEmpty()`**: true when the API returns zero features (typical
  when the point falls on unsurveyed land or in a Livre-Foncier area
  not covered by the cadastre tradition).
- **Backends**:
  - `apicarto.ign.fr/api/cadastre/parcelle` for the parcel under the
    point — public, no auth, GeoJSON FeatureCollection.
  - `cadastre.data.gouv.fr/bundler/cadastre-etalab/communes/{insee}/
    geojson/batiments` for the opt-in bâti dump (gzipped, several MB
    per commune). The Source caches the dump in-process per INSEE.
- **Bâti soft-fail**: when `IncludeBati: true` and the bâti dump
  fetch / parse fails, the parcel data is still returned with bâti
  fields nil and `Evidence.BatiError` populated.
- **Bâti emprise**: filter is centroid-PIP + raw building area. Small
  over-estimate when a building overhangs the parcel boundary,
  acceptable for an UI footprint readout (parcel-intersection area
  is a v2 candidate).
- **Default factory wiring**: `IncludeBati: false` — opt-in via
  `BuilderDefault` + custom `cadastre.NewSource(...)` when callers want
  the building analysis.

## `sources/carteloyers`

Reference rents from the national rent observatory (ANIL / DHUP carte
des loyers).

- **Needs**: INSEE + property type + rooms (used to pick a typology
  bucket: House, Apt 1-2 pieces, Apt 3+, generic Apt).
- **Result**: `LoyerMedEURPerM2CC` (median rent, **€/m²/month charges
  comprises** — apply a CC→HC factor ~0.90 if you need hors-charges),
  `LoyerLowEURPerM2CC` / `LoyerHighEURPerM2CC` (80 % prediction interval),
  `Typology` (which bucket was used), `NbObservations` (commune sample) and
  `Confidence`. Also satisfies `appraisal.RentEstimator` — its `RentEstimate`
  applies the CC→HC factor so it blends hors-charges with `oll` / `encadrement`.
- **Fallback**: when the rooms-bucket dataset misses on a commune, the
  Source widens to `TypologyApartment` and stamps
  `Evidence.FallbackToGeneric=true`.

## `sources/dvfagg`

Per-commune DVF sale-price aggregate (median €/m² + dispersion), the
offline batch complement to the live, per-address `dvf` source.

- **Needs**: INSEE.
- **Result**: `PriceMedianEURM2` / `PriceP25EURM2` / `PriceP75EURM2`
  (dispersion over single-lot apartment sales; a wide spread flags a
  bimodal commune), `PriceMedianSmallEURM2` (18–55 m², to pair with a
  T1–T2 rent), `N` / `NSmall` (sample sizes), `Dept`. Empty when the
  commune had no qualifying sale.
- **Feeds `appraisal.PricePerM2`**: `PriceEstimate()` contributes the
  commune median (confidence per sample size + dispersion), so the price
  synthesis can clear its MinSources=2 floor from embedded data alone —
  pairing with the live `dvf` reading instead of forcing `price_confidence`
  structurally Low.
- **Build**: `gazetteer refresh -go-embed-update dvfagg` downloads the
  geo-dvf bulk files (dept × last 3 years, currently 2023-2025), keeps
  single-lot apartment *Vente* mutations, and writes the embedded
  `dvf_communes.csv` (~9 k communes). Excludes 57/67/68 (Livre Foncier)
  and 976 (not in DVF).

## `sources/dvf`

Demandes de Valeurs Foncières — historical mutation prices from
data.gouv.fr Etalab.

- **Needs**: property type + (INSEE OR a resolvable address); surface
  recommended for €-total estimates.
- **Result**: `ValueEURPerM2Cents` (median price, **centimes** — ÷100 for
  €/m²), `P25EURPerM2Cents` / `P75EURPerM2Cents` (the quartile band),
  `ValueEURCents` (total = per-m² × surface, when surface is known),
  `SampleSize` (mutations behind the median) and `Confidence`
  (`high`/`medium`/`low`). Also satisfies `appraisal.PriceEstimator`.
- **Ladder**: 4-tier `helpers/fallback.Walk`:
  1. `address_radius` — 500 m disk around `(Lat, Lon)`, MinSample 12
  2. `commune` — listing's INSEE
  3. `neighborhood` — commune + its haversine neighbours
  4. `department` — entire département
  The winning tier is recorded in `Evidence.LevelUsed`.
- **Section catalog cache**: the per-INSEE cadastral section list is
  cached via a `kvcache.Cache` (`Options.SectionCache`). See
  [caching.md](caching.md).
- **Performance**: DVF fetches mutations one HTTP call per cadastral
  section. The `address_radius` tier **spatially prefilters** the
  commune's sections (via their cadastre bounding boxes) to the handful
  whose box falls within the disk, instead of all ~50 in a dense
  arrondissement; the surviving sections are then fetched concurrently.
  Wire `dvf.HostRateLimits()` into `httpx.Options.PerHost` so the
  data.gouv.fr DVF + cadastre hosts get 10 req/s instead of the polite
  2 req/s default — the `factory` and the CLI do this for you. Together
  these take a dense-arrondissement lookup from ~24 s to ~2 s.
- **Circuit breaker**: 3 consecutive transport errors OR 3 consecutive
  429s trip the breaker. Returns `dvf.ErrCircuitTripped` (matches
  `gazetteer.ErrSourceCircuitTripped`).

## `sources/dpedist`

Distribution of DPE energy-performance classes (A..G + sentinel N
for non évalué) across every DPE the ADEME indexes in the commune
since July 2021.

- **Needs**: INSEE.
- **Result**: `dpedist.Result` with per-class counts and shares,
  total volume, headline `PassoireSharePct` (F + G combined) and
  `EfficientSharePct` (A + B combined).
- **Backend**: HTTP GET on ADEME's data-fair `values_agg` endpoint
  (`data.ademe.fr`). No auth, no documented quota. One request per
  Listing. `Options.BaseURL` lets tests redirect to httptest.
- **Confidence**: `high` ≥ 50 DPE observed, `low` when 1..49 (thin
  sample — single passoire can move headline by ≥ 2 pp), `none` when
  the commune carries zero DPE.
- **Why this matters**: Loi Climat already excludes G-class from the
  legal-rental scope (2025) ; F-class follows in 2028, E in 2034. The
  per-commune passoire share is the leading proxy for how much of the
  housing stock is about to leave the rental market.

## `sources/encadrement`

Zoned rent caps (encadrement des loyers) for Paris, the two
Seine-Saint-Denis EPTs (Plaine Commune, Est Ensemble) and
Lyon / Villeurbanne.

- **Needs**: zip OR INSEE + property type + rooms; coordinates
  (lat/lon) for the precise Seine-Saint-Denis zone.
- **Result**: `LoyerRefMajEURPerM2HC` (the **legal max**, loyer de référence
  majoré, €/m²/month HC), `LoyerRefEURPerM2HC` (the published reference, ~20 %
  below), `Zone` + `ZoneSource` (resolved zone, e.g. `Paris 11e` /
  `plaine_commune`) and `Confidence`. Also satisfies `appraisal.RentEstimator`.
- **Zone identification**:
  - Paris by zip: 75001..75020, 75116 (arrondissement-median).
  - Lyon / Villeurbanne by INSEE: 69381..69389, 69266.
  - Plaine Commune (9 communes) & Est Ensemble (9 communes) by
    point-in-polygon over an embedded zonage GeoJSON: the listing's
    coordinates resolve the exact sub-communal zone. Without
    coordinates it falls back to the commune — single-zone communes
    resolve at `ConfidenceMedium`; the two multi-zone communes
    (Saint-Denis, Montreuil) collapse across their zones at
    `ConfidenceLow`. `Evidence.ZoneID` records the resolved zone(s).
- **Eligibility**: residential only.
- **Vintages**: Paris 2025, Lyon 2025-2026, Plaine Commune & Est Ensemble
  barème "du 01 juin 2026" (DRIHL référence-loyer KML; zonage 2022). Zone
  numbers are not unique across EPTs, so lookups are scoped by
  `(zone_source, zone)`.

## `sources/catnat`

Per-commune history of recognised natural-disaster decrees ("arrêtés de
catastrophe naturelle", GASPAR, 1982→present). Where `georisques` reports the
modelled hazard, `catnat` reports the realised sinistralité.

- **Needs**: INSEE (a PLM arrondissement folds to its mother commune — decrees
  are issued at commune level).
- **Result**: `TotalArretes` (all-time decree count since 1982), `RecentCount`
  (decrees in the last 10 years vs the dataset vintage), `ByCategory` (map →
  count: inondation / sécheresse / mouvement_terrain / tempête),
  `LastEventYear` and `Tier` (recent-frequency tier).
  Satisfies `appraisal.HazardReporter`, so the confirmed categories flow into
  `appraisal.HazardProfile` alongside `georisques`. `IsEmpty` (StatusOKEmpty)
  for a commune with no recorded decree.
- **Coverage**: national (~34 700 communes). The sécheresse (clay-shrinkage)
  decrees are the cracking-risk signal the zoning often misses.
- **Refresh**: from the Géorisques GASPAR ZIP export; the per-commune aggregate
  is gzip-embedded (~220 KB).

## `sources/iris`

Resolves a coordinate to its INSEE IRIS — the sub-commune statistical zone
(≈ 2 000 inhabitants), the finest official mesh for census/income data.

- **Needs**: lat/lon (or a pre-resolved `Listing.IRIS`, which it reuses).
- **Result**: `code_iris` (9-digit), `nom_iris`, `typ_iris` (H/A/D/Z).
- **Dual role**: the Source also implements `gazetteer.IRISResolver`. The
  factory (and CLI) wire it into the BANNormalizer via `WithIRIS`, so every
  normalized address carries `Listing.IRIS` — the hook for future IRIS-keyed
  sources. The Source's own Query reuses a pre-resolved `Listing.IRIS` to avoid
  a second point-in-polygon pass.
- **Resolution**: point-in-polygon over the embedded IRIS contours
  (`helpers/geopoly` + bbox pre-filter).
- **Scope (v1)**: Île-de-France (~5 300 IRIS), gzip-embedded (~1.3 MB). Outside
  it → StatusOKEmpty. National coverage would exceed the embed budget
  (download-only datadir, future work).
- **Refresh**: from the region's Opendatasoft IRIS-contours GeoJSON.

## `sources/nuisances`

Île-de-France cumulative environmental-nuisance grid (Institut Paris Région /
Bruitparif, 500 m cells): how many nuisances (road/rail/air-traffic noise + air
pollution) overlap the listing's cell — a cadre-de-vie signal that drives
property decotes.

- **Needs**: lat/lon.
- **Result**: nuisance count (0–4), a "point noir environnemental" flag, and an
  exposure tier (calme / modéré / exposé / très exposé). `IsEmpty`
  (StatusOKEmpty) outside the IDF grid; a resolved count-0 cell is reported (a
  real "calm here" reading, not empty).
- **Resolution**: nearest 500 m cell centre within `MaxCellMeters` (400 m), via
  a spatial-hash grid index over ~49 000 cells.
- **Refresh**: from the region's Opendatasoft portal; the grid is
  gzip-embedded (~760 KB).

## `sources/cdsr`

Proximity to Île-de-France condominiums labelled "en difficulté soutenue par
la Région" (CDSR) — a small, curated, high-precision copro-risk red-flag.

- **Needs**: lat/lon (the Source is purely spatial — no name matching).
- **Result**: nearest labelled copro within `MaxNearestMeters` (3 km), counts
  within 500 m and 3 km, and the nearby copros (name, address, commune, lot
  count, label year, distance). `IsEmpty` (StatusOKEmpty) when none is in
  range — the common, reassuring case.
- **Coverage**: IDF only, intentionally sparse (the most severe,
  region-intervened cases). Not gated on property type — a distressed copro
  nearby is a neighbourhood signal for any property.
- **Refresh**: from the region's Opendatasoft portal (`exports/json`).

## `sources/oll`

Observed market rents from the Observatoires Locaux des Loyers (OLL) — the
field-measured median €/m²/month (hors charges), the most representative rent
signal, complementing `encadrement` (legal cap) and `carteloyers` (model).

- **Needs**: INSEE; rooms optional (refines the bucket, else the zone-level
  all-sizes median is used).
- **Result**: `ObservedMedianEURPerM2` (observed median, **€/m²/month hors
  charges**), `ObservedQ1EURPerM2` / `ObservedQ3EURPerM2` (inter-quartile band),
  `AvgSurfaceM2` (mean dwelling surface — sanity-checks the bucket),
  `SampleSize`, `Zone` + `Agglo` + `Pieces` (resolved cell) and `Confidence`.
  Satisfies `appraisal.RentEstimator` (weight 0.95 in `DefaultRentWeights`,
  above the modelled `carteloyers`). `IsEmpty` (StatusOKEmpty) outside the
  perimeter.
- **Resolution**: INSEE → OLL zone (embedded commune→zone map) → the cell for
  the rooms bucket. Zone numbers join the rent table via
  `Zone_calcul = "L<agglo>.4."+zeropad2(zone)`.
- **Scope**: 17 major French agglomerations (Paris petite/grande couronne, Lyon,
  Lille, Toulouse, Bordeaux, Nantes, Strasbourg, Montpellier, Grenoble, Rennes,
  Nice, Clermont, Nancy, Tours, La Rochelle, Besançon, La Réunion), vintage
  2024–2025. Paris intra-muros excluded (encadrement serves it). Appartements
  only. Curated in `aggloSpecs`; extensible to more perimeters.
- **Refresh**: one per-agglo ZIP archive each (heterogeneous CSV encodings and
  column names; a malformed agglo is skipped, the rest still build).

## `sources/filosofi`

Per-commune income and minima-sociaux statistics from INSEE Filosofi
(2021 vintage).

- **Needs**: INSEE.
- **Result**: `MedianEUR` (revenu disponible médian par UC, **€/an**),
  `MinimaPct` (part des minima sociaux, **%** — poverty proxy), `Flag`
  (low / medium / high / unknown risk bucket) and `Confidence`.
- Property type is irrelevant — applies to the whole commune.

## `sources/filoiris`

Per-**IRIS** (sub-commune) income statistics from INSEE Filosofi (2021),
the neighbourhood-level counterpart of `filosofi`. Answers "how wealthy is
this *quartier*" where `filosofi` answers "how wealthy is this town" — the
distinction that matters most in dense, socially-mixed communes (Paris,
petite couronne), where a single commune can span a 2× spread across its
IRIS.

- **Needs**: `Listing.IRIS` (populated by the `iris` Source / the BAN
  normalizer's IRIS resolver). Only answers inside INSEE's IRIS perimeter
  (communes ≥ 5000 inhabitants, secret statistique permitting).
- **Result**: `MedianEUR` (IRIS revenu disponible médian par UC, **€/an**),
  `PovertyRatePct` (taux de pauvreté au seuil de 60 %, **%**), `Gini` (income
  Gini index, **0..1** — a "socially mixed" signal even when the median looks
  comfortable), `Flag` and `Confidence`.
- **Backend**: gzipped JSON embedded under `data/` (~14 490 IRIS, ~133 KB).
- **Appraisal**: `appraisal/zonescore`'s solvabilité axis prefers this
  IRIS-level reading over the commune-level `filosofi`.

## `sources/logiris`

Per-**IRIS** census housing structure from INSEE RP 2021 (base-ic-logement):
the share of renters, the share of social housing, and the vacancy rate of
the *neighbourhood*. A high renter share + low vacancy marks a deep, tight,
easy-to-let rental market — and these diverge sharply across the IRIS of a
single dense commune.

- **Needs**: `Listing.IRIS`. **Île-de-France only** (the embedded artifact is
  IDF-scoped, matching the `iris` resolver; the national base is ~49 000 IRIS).
  IRIS below 50 dwellings are dropped (suppression-prone / statistically thin).
- **Result**: `RenterSharePct`, `SocialHousingSharePct`, `VacancyRatePct`,
  dwelling count.
- **Backend**: gzipped JSON embedded under `data/` (~5 100 IDF IRIS, ~66 KB).
- **Appraisal**: `appraisal/zonescore`'s tension axis prefers this IRIS-level
  vacancy + rental-market depth over the commune-level `vacance`.

## `sources/georisques`

Natural and technological hazards from georisques.gouv.fr (BRGM
rapport-risque).

- **Needs**: lat/lon (resolved via Geocoder when absent).
- **Result**: `Naturels` (map of 12 natural-hazard keys → `RiskBlob`:
  flood, soil/clay, seismic…) and `Technos` (6 technological keys: ICPE,
  Seveso, nuclear…), `Summary` (operator-actionable counters + red flags),
  `ReportURL` (georisques.gouv.fr permalink), `LevelUsed`
  (`address`/`commune` granularity) and `Confidence`. Satisfies
  `appraisal.HazardReporter`.
- **`IsEmpty()`**: true when both Adresse and Commune fields parse
  empty.

## `sources/locservice`

Rental-market tension labels from locservice.fr.

- **Needs**: INSEE + property type + rooms (for the logement keyword).
- **Result**: `TensionLabel` (`tendu` / `équilibré` / `détendu`),
  `TensionScore` / `SupplyScore` (raw **0..8** "facilité à trouver une
  location" gauge — high = landlord-friendly), `BudgetScore` (**0..8** tenant
  solvency gauge), `ScoreScale` + `Description` (rendered context) and
  `Confidence`. No rent value — these are demand/solvency gauges, not a price.
  `Evidence.FellBack=true` when the logement-specific call returned no data and
  the Source widened to the commune-wide call.

## `sources/osm`

Walking distance to the nearest métro / RER / tram / Transilien
station, from an OpenStreetMap Overpass extract. Registered under the
canonical name `osm_transit`.

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

## `sources/gpe`

Nearest **future** Grand Paris Express station — the new IDF metro
(lines 14 ext / 15 / 16 / 17 / 18). Proximity to a coming GPE station is
a major rental-demand and capital-appreciation driver for the "near a
station, not Paris" thesis (pairs with the `transport` ZoneScore profile).

- **Needs**: lat/lon.
- **Result**: nearest station name + line (verbatim SGP label, e.g. `L15`
  or `L16/L17` at an interchange) + distance, plus station counts within
  1.5 km / 3 km. Empty beyond 6 km.
- **No opening year**: deliberately omitted — the calendar shifts and one
  line label spans sections opening years apart, so a per-station date
  would be guesswork. It is **informational**, not folded into ZoneScore
  (future transit must not distort the yield-first-today score).
- **Backend**: the Société du Grand Paris station catalog (~68 stations)
  embedded under `data/` (plain JSON, ~10 KB).

## `sources/taxefonciere`

Per-commune `taxe foncière` estimate.

- **Needs**: INSEE + surface_m2.
- **Result**: `EstimatedEURPerYear` (the TFPB the landlord pays
  out-of-pocket, **€/year**), `TEOMEURPerYear` (the recoverable TEOM, **€/year**
  — surfaced separately so it doesn't pollute net cashflow), `TauxTFPBApplied` /
  `TauxTEOMApplied` (voted rates, **%**), `VLEURPerM2` (the per-m² valeur
  locative applied), `UsedDeptFallback` / `UsedV1Fallback` flags and
  `Confidence`. Confidence reflects whether the V2 (DGFiP voted rates × VLC ×
  surface) or V1 (legacy per-m² ratio) path applied, and whether the lookup hit
  the commune or fell back to the department.
- **Limitation (read before trusting the €)**: this is an *order-of-magnitude*
  estimate, not the exact bill. The voted taux are real, but they multiply a
  per-m² valeur-locative **proxy**, not the dwelling's actual cadastral base, so
  the figure **systematically understates** the tax in high-value communes —
  notably **Paris, where it can be ~half** the real amount. Use it to *compare*
  communes, not as the precise sum due. (A per-commune REI base would fix it;
  deferred.)

## `sources/lovac`

Per-commune FISCAL vacancy status from the LOVAC 2025 dataset (TLV
2013 / THRS perimeter — the dataset Bercy uses to assess the Taxe sur
les Logements Vacants).

- **Needs**: INSEE.
- **Result**: vacancy rate %, long-term vacancy split. Missing
  communes (secret statistique) surface as `IsEmpty()`.
- **Disambiguation**: for the DEMOGRAPHIC vacancy rate from the INSEE
  census, see `sources/vacance`. Distinct datasets — the two signals
  are correlated but not interchangeable.

## `sources/vacance`

Per-commune DEMOGRAPHIC vacancy rate from the INSEE Recensement de la
Population 2021 ("base communale logement").

- **Needs**: INSEE.
- **Result**: `vacance.Result` with `VacancyRate` (% LOGVAC /
  LOG), counts of total / vacant / résidences principales /
  secondaires, and a distribution-relative tier (`tendu` / `normal` /
  `élevé` / `déprise`).
- **Backend**: gzipped JSON embedded under `data/`, ~34 955 communes
  including the per-arrondissement rows for Paris / Lyon / Marseille
  (the Source does NOT fold arrondissements — Paris 1er vacancy ≠
  Paris 18e vacancy is a real signal here).
- **Disambiguation**: for the FISCAL LOVAC vacancy status, see
  `sources/lovac`.

## `sources/rpls`

Per-commune share of social housing (logement locatif social) under
the loi SRU article 55 framework.

- **Needs**: INSEE. Paris / Lyon / Marseille arrondissements fold to
  the parent commune (upstream publishes one row per parent commune).
- **Result**: `rpls.Result` with `LLSRate` (%) and a distribution-
  relative tier (`rural` / `mixte` / `fort` / `satured`).
- **Backend**: gzipped JSON embedded under `data/`, ~35 228 communes
  (data.gouv.fr "Taux de logements sociaux dans les Communes" 2024
  vintage, frozen 2025-01-01).
- **Note**: ≈ 64 % of communes report a 0 % rate — these are below
  the SRU obligation threshold; the answer is real (TierRural,
  Confidence=high), not missing.

## `sources/sitadel`

Per-commune housing-construction dynamics from the SDES Sitadel annual
file: building permits **authorised** (LOG_AUT) and housing **starts**
(LOG_COM, "commencés"), counted in dwellings. A forward-looking SUPPLY
signal — how much new housing a commune is permitting and breaking ground
on — which weighs on future rents and resale where it is large relative
to the existing stock.

- **Needs**: INSEE. Paris / Lyon / Marseille arrondissements fold to the
  parent commune. The upstream publishes both the parent aggregate
  (75056 / 69123 / 13055) and, for Paris/Lyon only, the per-arrondissement
  codes; the build keeps only the parent aggregates, and `Query` folds
  arrondissement INSEE through `communes.FoldArrondissement`.
- **Result**: `sitadel.Result` with `AuthorizedLatest` / `StartedLatest`
  (dwellings, with their `LatestYear` / `StartedLatestYear` — the latest
  millésime is provisional and carries no starts, so the started year is
  typically one behind), `AuthorizedAvg5y` / `StartedAvg5y` (dwellings/year
  over the last 5 populated years), `CollectifSharePct` (apartment share of
  authorised dwellings, %), and `AuthorizedSeries` + `SeriesStartYear` (the
  full 2013→latest authorised series for a sparkline). Counts only —
  SDP_* floor-area columns are ignored. Deliberately raw (no composite
  tier): absolute counts scale with commune size, so per-stock
  normalisation belongs in the appraisal layer.
- **Backend**: gzipped JSON embedded under `data/` (~35 k communes; SDES
  Sitadel 2026-06 millésime, years 2013→2025). Blank upstream cells are
  kept distinct from a real 0.
- **`IsEmpty()`**: true when the commune is absent, or present with no
  non-zero authorised dwelling in any year.

## `sources/ips_ecoles`

Per-commune median Indice de Position Sociale (IPS) over écoles
primaires, from the DEPP per-establishment dataset.

- **Needs**: INSEE.
- **Result**: `ips_ecoles.Result` with `IPSMedian`, min / max range,
  school count, and a distribution-relative tier (`precaire` / `mixte`
  / `moyen` / `favorise`). Confidence is `high` with ≥ 3 schools,
  `medium` with 1-2.
- **Backend**: gzipped JSON embedded under `data/`, ~16 153 communes
  hosting ≥ 1 école, ~29 990 establishments (rentrée 2024-2025).
- **PLM granularity**: this is the ONLY commune-level Source in the
  gazetteer that yields per-arrondissement readings for Paris / Lyon
  / Marseille — Paris 1er IPS ≈ 130, Paris 16e ≈ 140, Paris 18e ≈ 104,
  differences crushed by every other commune-keyed Source.
- **Aggregation**: UNWEIGHTED median (the upstream CSV does not
  publish per-school effectifs).

## `sources/anct`

ANCT (Agence Nationale de la Cohésion des Territoires) territorial
revitalization programme flags per commune.

- **Needs**: INSEE.
- **Result**: `anct.Result` with booleans for `ActionCoeurDeVille`,
  `PetitesVillesDeDemain`, `ORTSigned`, plus a `DenormandieEligible`
  flag (= ORT-signed).
- **Backend**: offline merged extract under `data/` (Action Cœur de
  Ville + Petites Villes de Demain + Opération de Revitalisation de
  Territoire), covering ~2 400 communes.

## `sources/cartofriches`

Cerema "Cartofriches" national brownfield inventory aggregated per
commune.

- **Needs**: INSEE.
- **Result**: `cartofriches.Result` with `SiteCount`, breakdowns by
  type (industriel / habitat / commercial / …) and status (avec projet
  / sans projet / reconverti), plus cumulative surface in m².
- **Backend**: offline aggregate of ~28 000 sites across ~9 100
  communes.

## `sources/bpe`

Curated subset of INSEE's Base Permanente des Équipements (BPE) 2024
counts: ~25 of the 188 type codes folded into 16 rental-investor
buckets (poste, grande_surface, supérette, boulangerie, école
primaire, collège, lycée, structure_sante, médecin_généraliste,
infirmier, pharmacie, crèche, gare, sport_salle / piscine / terrain).

- **Needs**: INSEE.
- **Result**: `bpe.Result` carrying `Counts map[Bucket]int` +
  `TotalFacilities`. Communes with zero curated facility surface as
  `IsEmpty()` — small rural communes that only carry A129 Mairie fall
  in this bucket on purpose (a Mairie is not a meaningful tenancy
  signal).
- **Backend**: gzipped JSON embedded under `data/`. ~21 700 communes
  × 16 buckets, ~233 KB on disk.
- **Property type irrelevant** — equipment density applies to the
  whole commune.

## `sources/chomage`

Latest INSEE estimate of the local unemployment rate ("taux de chômage
localisé") for the zone d'emploi a commune belongs to, plus a 20-quarter
trend window.

- **Needs**: INSEE.
- **Result**: `chomage.Result` carrying the ZE2020 code + label, the
  latest seasonally-adjusted rate, the matching national average, the
  delta in percentage points, a peer-relative tension flag (tight /
  balanced / loose) and a recent-quarters series suitable for a UI
  sparkline.
- **Backend**: offline merged JSON under `data/` (302 zones d'emploi
  2020 + ~34 875 commune crosswalk, 20-quarter tail).
- **Coverage**: metropolitan France + DOM. Mayotte and French Guiana
  are excluded by INSEE per the source dataset; commune-INSEE-not-found
  cases surface as `IsEmpty()`.

## `sources/delinquance`

Per-commune crime / security indicators from the SSMSI État 4001
framework.

- **Needs**: INSEE.
- **Result**: `delinquance.Result` exposing rates per 1 000 inhabitants
  for ~14 indicators (cambriolage, vandalisme, violences, vols,
  fraude, stupéfiants) plus a peer-relative risk flag for the latest
  reference year (2024 at the time of the embed).
- **Backend**: gzipped JSON embedded under `data/`.

## `sources/education`

Live count of open schools (école, collège, lycée, médico-social) per
commune from the Annuaire de l'Éducation Nationale.

- **Needs**: INSEE.
- **Result**: `education.Result` with `TotalOpen` plus breakdown by
  type for establishments currently OUVERT.
- **Backend**: HTTP GET against `data.education.gouv.fr` (Opendatasoft
  API). Honours `Options.BaseURL` for tests.

## `sources/qpv`

Quartiers Prioritaires de la politique de la Ville (decree 2023-1314):
is the listing **inside** a QPV? Answered by point-in-polygon over the QPV
2024 contours when coordinates are present, with a commune-level fallback.

- **Needs**: INSEE; Lat/Lon strongly recommended (unlocks the address-level
  point-in-polygon path; without them the answer is commune-level only).
- **Result**: `qpv.Result` with `HasQPV`, `MatchLevel` (`point` | `commune`),
  the matched QPV code(s) + labels, and — for a point outside every QPV — a
  `Nearest{Code,Label,Meters}` hint (within 1 km) kept out of `HasQPV`.
  A point inside a QPV returns that single QPV at high confidence; a
  coordinate-less query falls back to the commune's QPV list at medium
  confidence.
- **Backend**: offline gzipped polygon contours under `data/` (ANCT,
  data.gouv.fr, WGS84 GeoJSON, métropole + outre-mer, ~1 584 QPV).

## `sources/sensible`

The State's hardest-neighbourhood perimeters — far more selective than the
~1 500 QPV: is the listing **inside** (or within 400 m of) one of the 62
**QRR** "quartiers de reconquête républicaine" (police-priority perimeters
designated by the ministère de l'Intérieur against entrenched trafficking,
2018–2021, official polygons) or one of the 4 **ORCOD-IN** copropriétés
dégradées d'intérêt national (décrets en Conseil d'État — the classic
judicial-auction trap)? The lists are administrative snapshots (QRR frozen
since 2021): a stable photo of structurally distressed areas, not a live feed.

- **Needs**: Lat/Lon (no commune-level fallback — that grain is `qpv`'s job).
- **Result**: `sensible.Result` with `Sensitive` (inside ≥ 1 zone), `In`
  (containing zones) and `Nearby` (boundary within 400 m — absorbs geocoding
  imprecision), each a `Zone{Name, Kind (qrr|orcod|curated), Dep, Vague,
  DistanceM, Note}`. ORCOD/curated entries cite their décret in `Note`.
- **Backend**: offline gzipped QRR polygons under `data/` (ministère de
  l'Intérieur via data.gouv.fr, WGS84, 62 zones) + an in-code curated circle
  overlay (`curated.go` — official designations only, source required: the 4
  ORCOD-IN décrets, plus documented additions such as Les 4000 à La Courneuve,
  NPNRU d'intérêt national, which the QRR perimeter misses).

## `sources/zonageabc`

Official zonage A bis / A / B1 / B2 / C classification per commune
(arrêté du 5 sep 2025). The zonage anchors several housing-tension
references (DPE displays, "logement intermédiaire", etc.).

- **Needs**: INSEE. Paris / Lyon / Marseille arrondissements fold to
  the parent commune.
- **Result**: `zonageabc.Result` with `Zone` and the legal source
  arrêté reference.
- **Backend**: offline JSON under `data/`, ~34 875 communes.

## `sources/zonetendue`

Décret 2013-392 (and 2025-1267) "zone tendue" + "tendue touristique"
classification, plus the TLV-2013 (Taxe sur les Logements Vacants
2013-area) flag.

- **Needs**: INSEE.
- **Result**: `zonetendue.Result` with `Tendue`, `TenduTouristique`,
  `TLV2013` booleans, driving signal of préavis-réduit-1-mois, TLV /
  THRS surcharges, and encadrement à la relocation.
- **Backend**: offline JSON under `data/`, ~3 725 listed communes
  (absence = non tendue).

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

Today: `dvf` → PriceEstimator; `carteloyers` + `encadrement` + `oll` →
RentEstimator; `georisques` + `catnat` → HazardReporter.

On top of these, `appraisal/zonescore` is a terminal consumer that folds
the whole `Dossier` into a single yield-first 0–100 zone score with a
per-axis breakdown (rendement, tension, solvabilité, sécurité, fiscalité,
accès). `zonescore.Compute` scores one address; `zonescore.Compare`
ranks several. The CLI surfaces them via `appraise` and `compare`.

### Tests via `Options.BaseURL`

Sources whose Options struct exposes a `BaseURL` field are wired to a
local `httptest.NewServer` in tests. The Source's `Options.BaseURL`
or the corresponding package-level `BaseURL` var (when blank) is read
at Query time, so a single change at the constructor level swaps the
upstream cleanly. See [testing.md](testing.md).

### Batch / commune-level access

The offline, commune-keyed Sources expose their embedded index directly, so
you can read **many communes without building a `Listing` or running `Query`**
— load once, look up repeatedly:

| Helper | Returns |
|---|---|
| `dvfagg.Load(dir)` → `*Index`      | `.Codes()` (every INSEE with price data), `.Lookup(insee)` → `Result` |
| `qpv.Load(dir)` → `*Index`         | `.HasQPV(insee)` — coordinate-free, commune-level (NOT point-in-polygon) |
| `sensible.Load(dir)` → `*Index`    | `.ZonesForCommune(insee)` — QRR/ORCOD perimeters intersecting the commune (commune-grain) |
| `delinquance.Load(dir)` → `*Index` | `.Level(insee)` → coarse `RiskFlag` |
| `carteloyers.Load(dir)` → `*Index` | `.Lookup(insee, typology)` → `Row`; `Row.HCEURPerM2()` converts the CC median to hors-charges |
| `communes.Default()` → `Table`     | `.All()` (every commune row), `.Lookup(insee)` |

`dir` is the datadir (`""` loads the embedded dataset). On top of these, the
top-level **`overview`** package joins them into one row per commune:

```go
import "github.com/bpineau/gazetteer/overview"

rows, _ := overview.Build(overview.Options{Depts: []string{"75", "93"}})
// each CommuneOverview merges price (dvfagg), market rent (carteloyers),
// encadrement cap, income (filosofi), vacancy, taxe foncière, QPV, zonage,
// zone tendue, distance-to-Paris and nearby transit lines — all offline.
```

`overview.Build` does no network I/O; it iterates the communes `dvfagg` has
price data for (`Depts` empty = national). It is the batch/screening
counterpart to the per-address `Client.Collect`.

## `sources/rnc`

Copropriété context from the Registre National d'Immatriculation des Copropriétés (RNC, ANAH / data.gouv.fr).

- **Needs**: INSEE (5-digit). Lat/Lon strongly recommended (primary matching key); Address used to normalize the street. Without INSEE the Source emits `gazetteer.ErrInsufficientInputs`.
- **Match**: geo-proximity (≤ ~60 m) + normalized street within the commune. `MatchMethod` (`geo_voie`/`voie`) + `Confidence` (`high`/`medium`/`low`) are exposed. The Source does not consume a parcelle input, but each Result now carries the copro's cadastral parcelles (`Cadastre`, canonical 14-char refs) so a caller can verify/override the match against DVF, the cadastre source, or an auction fiche's parsed parcelles.
- **Result**: `Immatriculation`, `NomUsage`, lots (`LotsTotal`/`LotsHabitation`/`LotsStationnement`), `ConstructionPeriod`, `TypeSyndic`, `MandatEnCours`/`MandatFin`, `CoproAidee`, `Cadastre []Parcelle`, programme perimeters (`CoproACV`/`CoproPVD`/`CoproPDP`), QPV, `WebURL`, plus `Attention` + `Signals` — a LOW-CONFIDENCE triage hint, **not a distress verdict**.
- **Signals** (stable keys): `no_active_mandate` (mandate absent or expired with no successor), `syndic_unknown`/`syndic_benevole` (non-pro/undeclared syndic, **only on a copro of ≥ 50 lots**), `copro_aidee` (engaged ANAH subsidy — weak, since 2020 it also counts MaPrimeRénov' Copro on healthy copros), `fragile_profile` (the "grand ensemble dégradé" archetype: large + pre-1975 + inside a QPV).
- **IsEmpty()**: true when no copropriété matched the address.
- **Limitation**: the RNC open-data export **redacts** financial declarations AND the legal-procedure/arrêté columns (administration provisoire 29-1, mandataire ad hoc 29-1A, plan de sauvegarde, arrêtés péril/insalubrité) — the ANAH notice documents them, but they are stripped from the published files. A hard distress flag is therefore impossible from open data; the reliable procedure signal stays a manual check of the annuaire / CCV.
- **Upstream data**: data.gouv.fr "registre-national-dimmatriculation-des-coproprietes", resource "RNIC - Actualisation quotidienne" (daily "with-qpv" CSV, ~400 MB). Refresh: `gazetteer refresh --go-embed-update rnc`.

## `sources/links`

Deep-link URLs to useful third-party tools and datasets for the address —
including tools whose data other sources already extract, so a human can
cross-check a typed `Result` against the original site in one click. The odd one
out: it performs **no HTTP and ships no dataset**, it merely *builds* well-known
deep links from the listing's coordinates and address fields.

- **Needs**: at least one of lat/lon, INSEE, or an address; otherwise
  `gazetteer.ErrInsufficientInputs`. Coordinates unlock the bulk of the links.
- **Result**: `Links []Link` (each `{Key, Label, Category, URL}`), plus a
  `Map()` helper returning a `key→URL` map. Categories: `map`, `prices`,
  `risks`, `urbanism`, `context`.
- **Links built** (v1): Google Maps / Street View, OpenStreetMap, Géoportail
  (ortho + cadastre WMTS), IGN Remonter le temps (`map`); Pappers Immobilier,
  DVF explorer on explore.data.gouv.fr (`prices`); Géorisques rapport, enriched
  with INSEE/city when present (`risks`); Géoportail de l'Urbanisme / PLU
  (`urbanism`); INSEE commune fiche (COG) + a plain web search (`context`).
- **Not scored**: navigation convenience only — deliberately kept out of the
  zone score.
