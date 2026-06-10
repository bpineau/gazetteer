# Reusable building blocks — `helpers/`, `dataset`, and the core seams

Gazetteer's sources are assembled from standalone packages that are just as
usable **outside** the Dossier pipeline. They are part of the supported
public API: import them directly for any French real-estate / geo / open-data
tooling, even when you don't need "data about an address". (The heaviest
consumer of these packages is an auction-scraping app that uses `httpx` from
60+ files while never calling `Client.Collect` at all.)

Every package carries full godoc — `go doc github.com/bpineau/gazetteer/helpers/<name>`
is the authoritative reference; this page is the map.

## HTTP, resilience, caching

| Package | What it gives you | Reach for it when |
|---|---|---|
| [`helpers/httpx`](../helpers/httpx) | A polite production HTTP client: per-host token-bucket rate limits, Retry-After-honouring backoff, an on-disk ETag/Last-Modified-revalidating response cache, atomic snapshot downloads, a shared User-Agent. | Any scraper or open-data client. Start per-host limits from `factory.HostRateLimits()` (production-tuned values for BAN, data.gouv.fr, ADEME, …) and extend. |
| [`helpers/circuit`](../helpers/circuit) | Circuit breakers for unreliable upstreams: consecutive-failure streaks, 429/quota tripping, sliding-window failure ratios (`RateWindow`), content-shape streaks, plus `Fetcher`/`HTTPFetcher` — the fetch abstraction that plugs into every live Source's `Options.Fetcher`. See [circuit_breakers.md](circuit_breakers.md). | An upstream that throttles, crashes for minutes at a time, or serves anti-bot interstitials — and you want the whole run to stop paying for it after the Nth failure. |
| [`helpers/kvcache`](../helpers/kvcache) (+ `kvcache/memcache`, `kvcache/kvcachetest`) | A minimal persistent key-value cache **interface** with TTL semantics, an in-memory implementation, and a conformance test suite for your own backend (consumers run SQLite/bun adapters through it). See [caching.md](caching.md). | You need "cache this geocode/section list across process restarts" with a backend you own. |
| [`helpers/fallback`](../helpers/fallback) | A tiered fallback ladder (`Walk`): try tiers in order, classify outcomes, stop at the first success — with per-tier provenance. Drives DVF's address→commune→neighbors→department ladder. | Any "try the precise thing, then progressively coarser things" lookup. |
| [`helpers/atomicfs`](../helpers/atomicfs) | Atomic file writes/copies via tmpfile+rename (`WriteFile`, `CopyFile`, `Exists`, `NonEmpty`). | Artifacts that must never be observed half-written. |
| [`helpers/safejson`](../helpers/safejson) | `MustMarshal` and friends for values that cannot fail to encode. | Log/debug payloads where a marshal error is a programming bug. |

## French geocoding, administrative geography

| Package | What it gives you | Reach for it when |
|---|---|---|
| [`helpers/banx`](../helpers/banx) | A BAN (Base Adresse Nationale) geocoding client: forward + reverse, score filtering, a cache decorator with département-coherence guards against homonym drift, the INSEE-resolution cascade (`INSEEResolver`), zip/département reasoning (`ZipsShareDepartment`), and `NewDefaultGeocoder` — the exact cached+guarded stack the factory wires. `banx/maaddr` canonicalizes addresses for matching. | Anything that turns French free-text addresses into coordinates/INSEE codes. |
| [`helpers/communes`](../helpers/communes) | The embedded table of ~35k French communes: INSEE↔name/zip lookup, centroids, neighbors-within-radius, same-department, Paris/Lyon/Marseille arrondissement folding (`FoldArrondissement`, `ArrondissementParents`), zip→département (`DeptFromZip`), and the fully offline `ResolveINSEE(city, zip)` with the PLM postal rules. Note: the table is an embedded snapshot — it does **not** participate in `gazetteer refresh` and ages with the module version. | Any commune-level lookup, INSEE resolution without network I/O, or "which communes are near X". |
| [`helpers/fraddr`](../helpers/fraddr) | French street-address parsing: house number, street tokens, postal-code boundary detection ("30-32, av. André Kervazo" → number + tokens). | Tokenizing addresses for fuzzy matching or API query building. |
| [`helpers/frnorm`](../helpers/frnorm) | French text/number normalization: prices to centimes (`ParseFRPriceToCentimes`), decimal-comma floats incl. NBSP thousands separators (`ParseFRFloat`), accents stripping, whitespace collapsing, French date and hearing-time parsing. | Parsing anything humans or French administrations typed. |
| [`helpers/proptype`](../helpers/proptype) | The canonical property-type vocabulary: `Normalize` maps ~50 raw labels ("F2", "studette", "pavillon", "local commercial") onto one enum; `ToListingType` bridges to the coarse `gazetteer.PropertyType` used for source gating. | Classifying listings from any French property site. |

## Geometry & geo-math

| Package | What it gives you | Reach for it when |
|---|---|---|
| [`helpers/geodist`](../helpers/geodist) | Haversine distances (`MetersBetween`, `KmBetween`). | Point-to-point distance. That's it; it's tiny on purpose. |
| [`helpers/geopoly`](../helpers/geopoly) | The polygon kernel: even-odd point-in-polygon (`Covers`), bounding boxes, centroids, equirectangular areas in m². | Point-in-area tests against GeoJSON-shaped geometry. |
| [`helpers/geoindex`](../helpers/geoindex) | The embedded-polygon-index layer over geopoly: a compact wire format for polygon datasets, GeoJSON decoding, and a bbox-prefiltered first-cover `Resolve` / nearest-vertex `Nearest` index. This is what an out-of-tree **spatial source** should build on (iris, qpv and encadrement do). | Shipping your own "which zone is this point in" dataset. |
| [`helpers/stats`](../helpers/stats) | Median, percentiles (numpy-linear), decimal rounding, MAD outlier masks. | Small numeric reductions without pulling a stats dependency. |

## Scraping

| Package | What it gives you | Reach for it when |
|---|---|---|
| [`helpers/scrape`](../helpers/scrape) (+ `scrape/antibot`) | goquery-based HTML parsing helpers and anti-bot interstitial detection (DataDome and friends) that maps to `gazetteer.ErrAntiBot`. | Scraping French property sites without re-discovering the anti-bot signatures. |

## Shipping your own datasets — `dataset`

The [`dataset`](../dataset) package (see [datasets.md](datasets.md)) is not
gazetteer-specific: any app can use it to ship **its own embedded datasets
with a refresh pipeline** — declare a `Set` (embedded `fs.FS` + raw upstream
URLs + a transform), read it with datadir-overrides-embed semantics
(`Set.Open`, `Lazy[T]`, `ReadGzJSON`/`WriteGzJSON`, `BOMReader`), and
refresh it idempotently (`Refresh`, sha256-pinned,
manifest-tracked). Out-of-tree plugin sources participate in
`gazetteer refresh` identically to in-tree ones.

## Core seams (package `gazetteer`)

Useful even if you never build a Dossier:

- **`gazetteer.FetchUpstream` + `FetchSpec`** — a GET with the gazetteer
  error taxonomy baked in (transport/5xx/429 → transient, 4xx → permanent,
  404 → your empty payload). The shared fetch body of every live source;
  use it in plugins so your errors classify correctly.
- **`gazetteer.Fetcher`** — the inverse seam: every live source's
  `Options.Fetcher` accepts your implementation (circuit breakers, quota
  trippers, recorded fixtures) while keeping the source's URL building and
  parsing. `helpers/circuit.HTTPFetcher` implements it.
- **`gazetteer.QueryTyped[T]`** — typed wrapper over any `Source.Query`;
  each source also exposes it pre-instantiated as `(*Source).QueryResult`.
- **The error sentinels** (`ErrUpstreamUnavailable`, `ErrAntiBot`,
  `ErrInsufficientInputs`, `CircuitTrippedError`, …) — a ready-made error
  taxonomy for enrichment pipelines; `errors.Is`-matchable, classified to
  statuses by the framework.
- **`gazetteer/gazettestest`** — a stub Source that runs through the real
  status classifier, for deterministic pipeline tests (see
  [testing.md](testing.md)).

## Per-source batch readers

Most offline sources also export a batch path that skips the
`Listing`/`Query` machinery entirely: `Load(dir)` returns the parsed index
once per process (`dvfagg.Load(dir).Lookup(insee)`, `qpv.Load(dir).HasQPV`,
`delinquance.Load(dir).Level`, …). The catalog marks which sources have one
(`gazetteer sources catalog --json`, field `batch`), and
[`overview.Build`](../overview) is the ready-made join of the
screening-relevant ones into one row per commune.
