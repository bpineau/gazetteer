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
| `anct`           | INSEE                                          | offline ANCT programme list |
| `bdnb`           | address + INSEE                                | data.gouv.fr BDNB PostgREST |
| `bpe`            | INSEE                                          | offline INSEE BPE 2024 subset |
| `cadastre`       | lat/lon                                        | apicarto.ign.fr + cadastre.data.gouv.fr |
| `carteloyers`    | INSEE + property_type + rooms                  | offline ANIL / DHUP dataset |
| `cartofriches`   | INSEE                                          | offline Cerema brownfields  |
| `chomage`        | INSEE                                          | offline INSEE chômage ZE2020|
| `delinquance`    | INSEE                                          | offline SSMSI État 4001     |
| `cdsr`           | lat/lon                                        | offline IDF CDSR snapshot    |
| `oll`            | INSEE (+ rooms)                                | offline OLL observed rents   |
| `dpedist`        | INSEE                                          | data.ademe.fr values_agg API|
| `dvf`            | INSEE or address + property_type (+ surface)   | data.gouv.fr Etalab DVF     |
| `education`      | INSEE                                          | data.education.gouv.fr API  |
| `encadrement`    | zip/INSEE + property_type + rooms (+ lat/lon for 93)| offline barème + zonage |
| `filosofi`       | INSEE                                          | offline INSEE Filosofi 2021 |
| `georisques`     | lat/lon (or address)                           | georisques.gouv.fr report   |
| `ips_ecoles`     | INSEE (arrondissement-aware)                   | offline DEPP IPS 2024-2025  |
| `locservice`     | INSEE + property_type + rooms                  | locservice.fr HTML scrape   |
| `osm_transit`    | lat/lon + offline station catalog              | OSM Overpass (refresh only) |
| `qpv`            | INSEE                                          | offline ANCT QPV 2024 list  |
| `rpls`           | INSEE                                          | offline data.gouv SRU 2024  |
| `taxefonciere`   | INSEE + surface_m2                             | offline DGFiP rates         |
| `vacance`        | INSEE                                          | offline LOVAC 2025 (fiscal) |
| `vacance_logements` | INSEE (arrondissement-aware)                | offline INSEE RP 2021       |
| `zonageabc`      | INSEE                                          | offline arrêté 2025-09-05   |
| `zonetendue`     | INSEE                                          | offline décret 2013-392     |

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
  [caching.md](caching.md).
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
- **Result**: a regulated rent value with low/medium/high ceilings;
  satisfies `appraisal.RentEstimator` with `Bracket` populated when a
  legal cap applies.
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
- **Vintages**: Paris 2025, Lyon 2025-2026, Plaine Commune 2022, Est
  Ensemble 2023 (zonage 2022). Zone numbers are not unique across EPTs,
  so lookups are scoped by `(zone_source, zone)`.

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
- **Result**: observed median + inter-quartile band (€/m²/month HC), mean
  surface, sample size, and the resolved OLL zone. Satisfies
  `appraisal.RentEstimator` (weight 0.95 in `DefaultRentWeights`, above the
  modelled `carteloyers`). `IsEmpty` (StatusOKEmpty) outside the perimeter.
- **Resolution**: INSEE → OLL zone (embedded commune→zone map) → the cell for
  the rooms bucket. Zone numbers join the rent table via
  `Zone_calcul = "L<agglo>.4."+zeropad2(zone)`.
- **Scope (v1)**: the Paris-region perimeter "agglomération parisienne hors
  Paris" (OLL code L7502 = petite/grande couronne), vintage 2024. Extensible to
  more OLL agglomerations. Appartements only.
- **Refresh**: from the observatory's per-agglo ZIP archive (ISO-8859-1 CSVs).

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

## `sources/taxefonciere`

Per-commune `taxe foncière` estimate.

- **Needs**: INSEE + surface_m2.
- **Result**: estimated annual `taxe foncière` in cents, broken down
  into TFPB and TEOM portions; confidence reflects whether the
  V2 (DGFiP voted rates × VLC × surface) or V1 (legacy per-m² ratio)
  path applied, and whether the lookup hit the commune or fell back to
  the department.

## `sources/vacance`

Per-commune FISCAL vacancy status from the LOVAC 2025 dataset (TLV
2013 / THRS perimeter — the dataset Bercy uses to assess the Taxe sur
les Logements Vacants).

- **Needs**: INSEE.
- **Result**: vacancy rate %, long-term vacancy split. Missing
  communes (secret statistique) surface as `IsEmpty()`.
- **Disambiguation**: for the DEMOGRAPHIC vacancy rate from the INSEE
  census, see `sources/vacance_logements`. Distinct datasets — the two
  signals are correlated but not interchangeable.

## `sources/vacance_logements`

Per-commune DEMOGRAPHIC vacancy rate from the INSEE Recensement de la
Population 2021 ("base communale logement").

- **Needs**: INSEE.
- **Result**: `vacance_logements.Result` with `VacancyRate` (% LOGVAC /
  LOG), counts of total / vacant / résidences principales /
  secondaires, and a distribution-relative tier (`tendu` / `normal` /
  `élevé` / `déprise`).
- **Backend**: gzipped JSON embedded under `data/`, ~34 955 communes
  including the per-arrondissement rows for Paris / Lyon / Marseille
  (the Source does NOT fold arrondissements — Paris 1er vacancy ≠
  Paris 18e vacancy is a real signal here).

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

Quartiers Prioritaires de la politique de la Ville (decree 2023-1314)
membership at commune granularity.

- **Needs**: INSEE.
- **Result**: `qpv.Result` with `HasQPV`, count of QPV in the commune,
  and the codes + labels.
- **Backend**: offline JSON under `data/`, ~840 communes / ~1 584 QPV.

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

Today: `dvf` → PriceEstimator; `carteloyers` + `encadrement` →
RentEstimator; `georisques` → HazardReporter.

### Tests via `Options.BaseURL`

Sources whose Options struct exposes a `BaseURL` field are wired to a
local `httptest.NewServer` in tests. The Source's `Options.BaseURL`
or the corresponding package-level `BaseURL` var (when blank) is read
at Query time, so a single change at the constructor level swaps the
upstream cleanly. See [testing.md](testing.md).
