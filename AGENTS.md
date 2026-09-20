# AGENTS.md - orienting guide for AI coding agents (and humans in a hurry)

This file is the **canonical entry point** for working in this repo. Read it
first; it is written to be ingested in one shot. Deeper references live in
[`docs/`](docs/). The machine-readable facts are kept honest by tests
(`docs/sources.json` and the `sources list` transcript in `docs/cli.md` are
both compared to the live catalog by `cmd/gazetteer`'s guards); the prose
pages are maintained by hand. If a fact below is wrong, that is a bug.

## What this is, and what it is for

`gazetteer` answers, for a French address: **is this property worth buying, and
at what price?** Not with an opinion, but with the raw typed facts a real-estate
investor would otherwise spend a day collecting by hand: what square metres
actually sold for nearby, what rent the market pays and what rent the law
caps, who lives there and what they earn, what the taxe fonciere will cost,
what the crime, flood, soil and pollution records say, how far the metro is and
what the building itself is made of.

The **data is the product**. Each dimension comes from a dedicated `Source` as
a fully-typed `Result` with documented, unit-bearing fields. A thin convenience
layer sits on top (`appraisal.*` consolidates price / rent / hazard,
`appraisal/zonescore` composites them into a 0-100 score): treat it as a sample
high-level API, not the goal. Most callers want the underlying Results.

Consumers, and how a change propagates:

| Consumer | What it is | Direction |
|---|---|---|
| `cmd/gazetteer` | the CLI, the fastest way to explore the data | in this repo |
| `../locador` | a rental-investment engine: it turns these facts into a yield and a maximum price | DEPENDS on gazetteer, **by published version** (`go.mod`). A change locador needs ships as a tagged gazetteer release, then a `go.mod` bump there. Locally it can be wired with a `replace`, which is never committed |
| `../gazetteer-fr-plugins` | out-of-tree Sources that stay outside this module (licence or weight) | DEPENDS on gazetteer's public API. Breaking `Source`, `Result` or the registry breaks it: check before changing them |

**Deliberately NOT in scope**: anything outside France; a hosted service, an
API server or a database (this is a library plus a CLI, and datasets ship
embedded in the binary); scraping behind a login or a paywall; storing anything
about the people who live at an address; giving investment advice, as opposed
to the facts an investor weighs.

## Priorities and non-negotiables

When a trade-off is unclear, these decide it, in order.

1. **A wrong number is worse than no number.** Every Result field is read as
   fact by something downstream. A silently wrong reading (a unit confusion, a
   commune centre read as an address, a legal cap read as a market rent) is the
   worst defect this repo can ship. `IsEmpty()` exists so that "no data" is
   always available as an honest answer: use it rather than guessing.
2. **Units live in the field name or its godoc, always.** Cents vs euros,
   EUR/m2 vs EUR/m2/month, percent, metres, counts. Never add a numeric field
   whose unit a reader has to infer, and never guess a unit when reading one.
3. **The uniform Source contract is the whole design.** Every
   `sources/<name>/` package has the same shape, so learning one teaches all of
   them. Do not invent a different shape for a new source, however convenient.
4. **A behaviour change owes a `Version` bump** on the source that changed, so
   a cached or stored Result can be told apart from a fresh one.
5. **The catalog cannot drift.** `docs/sources.json`, the roster and the CLI
   descriptors are compared against the live registry by tests: a new source is
   not done until all three know about it.
6. **Never hammer an upstream.** Live sources go through the rate-limited,
   cached HTTP stack; the atomic `Query` path deliberately has none of that, so
   use a factory-built Client for anything repeated.
7. **This repo is public: nothing personal in it.** No real address anyone
   lives at presented as somebody's, no name, no home path, no session
   narration, in code, docs, tests or commit messages.
8. **English for code, godoc and docs. Never a typographic dash** (no em-dash,
   no en-dash), anywhere: a plain hyphen, a comma, a colon or parentheses.

## 30-second mental model

```
Listing (address + property attrs)
   │  client.Normalize()  → fills INSEE, Lat/Lon, IRIS from free text
   ▼
Sources run in parallel (each independent, offline or live HTTP)
   ▼
Dossier  = map[name]Result   ← THE PRODUCT: one typed Result per dimension
   ▼ (optional convenience layer)
appraisal.PricePerM2 / RentValue / HazardProfile  ·  zonescore.Compute → score
```

```go
client, _ := factory.NewDefault(ctx)               // wires every stable source
listing, _ := client.Normalize(ctx, "12 rue X, 93100 Montreuil")
dossier := client.Collect(ctx, listing)            // runs all sources in parallel

// Pull the typed data you care about - this is the main use of the lib:
if r, ok := gazetteer.Get[*dvf.Result](dossier, dvf.Name); ok && !r.IsEmpty() {
    // r is *dvf.Result; every field is documented with its unit (see godoc)
}

// Optional: the convenience synthesis layer on top.
score := zonescore.Compute(dossier)
```

## The data is the point - discovering Result types + field meanings

Most of your work here is "which source gives dimension X, and what does each
field mean (units!)". The answers, easiest first:

1. **`gazetteer sources catalog --json`** (or `docs/sources.json`) - every
   source's summary, required inputs, coverage, what it feeds, batch
   capability, and its `result_schema` (field names). Start here to pick a source; it's ~15 KB, so
   filter (`jq '.[] | select(.name=="dvf")'` or `del(.[].result_schema)`)
   instead of ingesting it whole. Browsing by
   intent ("which source gives rental-demand data?") →
   **`gazetteer sources dimensions`** groups them by investor-evaluation
   dimension (price, rents, demand, solvency, taxes, transport, hazards, …).
2. **`go doc github.com/bpineau/gazetteer/sources/<name> Result`** - the
   authoritative field-by-field meaning **with units**. Every `Result` field
   carries a godoc comment (e.g. DVF prices are `…Cents` integers, OLL rent is
   `€/m²/month`, shares are `%`). This is the canonical data dictionary.
3. **`gazetteer sources doc <name>`** - the Result's JSON shape (field names +
   zero values) for a quick wire-format glance.
4. [docs/sources.md](docs/sources.md) - prose: what each source provides and the
   key Result fields.

Convention you can rely on: **units live in the field name or its godoc** - cents
vs euros, €/m² vs €/m²/month, % , metres, counts. When in doubt, read the field's
godoc; never guess a unit.

## The uniform Source contract - learn one, know all

**Every** package under `sources/<name>/` has the *same* shape. Once you've used
one source you've used all of them:

| Symbol | Meaning |
|---|---|
| `const Name` | the registry key, e.g. `dvf.Name == "dvf"` |
| `const Version` | bumps when logic changes |
| `type Options struct{ … }` | config; zero value is usually valid; offline sources carry `DataDir` |
| `func NewSource(Options) *Source` | constructor (uniform: none returns an error) |
| `func Query(ctx, Options, Listing) (*Result, error)` | **atomic helper** - run one source without the builder |
| `func (s *Source) QueryResult(ctx, Listing) (*Result, error)` | typed Query on a held instance (no `any` assertion) |
| `Options.Fetcher gazetteer.Fetcher` (live HTTP sources) | inject circuit breakers / fixtures into the fetch path (dvf and osm_transit have their own seams: `HTTP`/`CircuitTripped`, `OverpassFetcher`) |
| `type Result struct{ …; Evidence Evidence }` | the typed payload; `Evidence` is a `json:"-"` provenance sidecar |
| `func (*Result) IsEmpty() bool` | true ⇒ "ran fine, no data for this address" |

Pull a source's result out of a Dossier with the generic accessor:

```go
r, ok := gazetteer.Get[*filoiris.Result](dossier, filoiris.Name)
```

## Fastest way to explore - the CLI is self-describing

```bash
gazetteer sources catalog --json   # ← START HERE: every source's inputs,
                                    #   coverage, returns and which axis it feeds
gazetteer sources list             # names + versions (+ opt-in marker)
gazetteer sources doc <name>       # the Result's JSON shape (reflected)
gazetteer query    --json <addr>   # run every source on a real address
gazetteer appraise        <addr>   # query + price/rent/hazard + zone score
gazetteer compare  <a> <b> …       # rank addresses; --profile yield|transport|…
```

`docs/sources.json` is the same catalog committed to the repo (read it without
running anything).

## Inputs cheat-sheet - what each `Listing` field unlocks

`Normalize()` fills INSEE, Lat/Lon and IRIS from free text. If a source returns
empty, the usual cause is a **missing input** or **out-of-coverage** address.

| Listing field | Unlocks |
|---|---|
| `INSEE` (5-digit) | most commune-level sources (filosofi, taxefonciere, delinquance, …) |
| `Lat` / `Lon` | spatial sources: cadastre, cdsr, gpe, nuisances, osm_transit, georisques |
| `IRIS` (9-digit) | `filoiris`, `logiris` - **Île-de-France only**; set by the `iris` source / normalizer |
| `SurfaceM2` | DVF €-total, taxe-foncière estimate |
| `Rooms` | carteloyers, oll, encadrement (typology bucket) |
| `PropertyType` | DVF + encadrement eligibility (default `apartment`) |

## Reading results - the empty/error model (read before debugging)

| Outcome | Meaning | What to do |
|---|---|---|
| `Status==OK`, `IsEmpty()==false` | real data | use it |
| `Status==OKEmpty` / `IsEmpty()==true` | source ran, **no data for this address** - NOT an error | check the source's required input + coverage in the catalog |
| `Status==Failed*` + `Result.Err != nil` | real failure (transient / permanent / antibot) | inspect `Err`; transient = retryable |
| `errors.Is(err, gazetteer.ErrInsufficientInputs)` | you didn't supply a required input | see the cheat-sheet above |
| `errors.Is(err, gazetteer.ErrSourceCircuitTripped)` | upstream tripped a breaker this run | transient; fresh run resets |

## Debugging recipes

- **"a mostly-empty Dossier, why?"** → `gazetteer query --explain "<addr>"`
  (`appraise --explain` too, which keeps the synthesis under the diagnosis).
  It prints the normalised Listing and, per source that returned nothing, the
  cause: a **missing required input** ("Listing is missing X, which this source
  needs") vs **no data for this address** ("inputs present → coverage: …"). This
  is the first move for any "I got nothing back" question.
- **"source X returned empty"** → `--explain` answers it; for the raw logs run
  `gazetteer query --verbose --source X "<addr>"`.
- **"the number looks wrong"** → every Result has an `Evidence` sidecar with
  provenance (which tier/zone/dataset year it used). Inspect it.
- **"it's slow"** → only the *live-HTTP* sources cost latency
  (`factory.LiveSourceNames()` lists them); offline sources are instant. DVF
  is the usual culprit; it's already optimised (per-Query memo + section-geo
  cache + tuned `factory.HostRateLimits()`).

## Optional convenience layer (appraisal + zonescore)

Sits on top of the Dossier; **skip it if you just want the data**. A sample
high-level API, not the project's purpose.

- `appraisal.PricePerM2`, `RentValue`, `HazardProfile` consolidate a few
  dimensions across sources (a source opts in by implementing
  `appraisal.PriceEstimator` / `RentEstimator` / `HazardReporter`).
  `appraisal.Price/Rent/HazardSourceNames()` list which registered sources
  feed each synthesis (plugins included); estimates expose `EURPerM2()`
  accessors over the integer-cents fields.
- `appraisal/zonescore.Compute(dossier, opts…)` → a 0-100 score over 6 axes
  (rendement, tension, solvabilité, sécurité, fiscalité, accès);
  `zonescore.Compare(...)` ranks several addresses; weight presets via
  `zonescore.Personas` / the CLI `--profile`. The catalog's `feeds` field says
  which source drives which axis.

## Standalone building blocks - the library beneath the library

`helpers/*` and `dataset` are supported public API, usable without ever
building a Dossier: `httpx` (rate-limited HTTP client + disk cache, misses
single-flighted per key, pruning explicit via `PruneCache`), `banx`
(BAN geocoding, cached + dept-guarded; `NewDefaultGeocoder` is the canonical
production stack), `communes` (35k-commune table; offline
`ResolveINSEE(city, zip)` with PLM rules), `frnorm`/`fraddr`/`proptype`
(French parsing; `proptype.ToListingType` bridges to `gazetteer.PropertyType`),
`circuit` (breakers; `HTTPFetcher` implements the `gazetteer.Fetcher`
injection seam), `kvcache`, `geodist`/`geopoly`/`geoindex`, `scrape`,
`stats`, `fallback`, `atomicfs`, `safejson` - and `dataset` lets any app ship
its own embedded+refreshable datasets. **[docs/helpers.md](docs/helpers.md)
is the map**; each package's godoc is the reference.

## Batch & subset access - beyond one-address-at-a-time

Two patterns sit alongside the per-address `Collect`:

- **Run fewer Sources.** `factory.Options.Exclude` is a deny-list applied to
  the full default roster (e.g. `Exclude: []string{"bdnb"}` drops the live BDNB
  API - note: the catalog's `default: false` on bdnb is the *CLI's* opt-in
  policy; the factory wires every source regardless); `Builder.Without(names…)` prunes a pre-populated Builder before
  `.Build()`; `Client.CollectSome(ctx, listing, names…)` collects only a named
  subset on an existing Client. `Client.SourceNames()` enumerates what a Client
  will run; `factory.OfflineSourceNames()` / `LiveSourceNames()` split the
  roster into instant embedded Sources vs network ones - collect the offline
  set first for a fast partial answer, then pay for the live APIs. Sources run
  independently, so dropping an unconsumed one never affects the others.
- **Tune one Source, keep the roster.** `factory.Options.SourceOverrides`
  swaps a single Source's constructor while sharing the factory's deps
  (rate-limited HTTP client, cached geocoder) - e.g. give dvf a persistent
  `SectionCache`, or inject an `Options.Fetcher` circuit breaker into a source
  that carries that seam (ademe, georisques, ...). Typo'd names
  error. To *add* a source, use `BuilderDefault(...).With(plugin)`.
- **Screen every commune offline.** `overview.Build(overview.Options{Depts…})`
  joins the embedded, commune-keyed Sources into one `CommuneOverview` row per
  commune (price, market rent, encadrement cap, income, vacancy, taxe foncière,
  QPV, sensitive-zone perimeters, zonage, transit lines) with **no network I/O** - the inverse of the
  per-address Dossier. It rides on per-Source **batch-read helpers** that skip
  the `Listing`/`Query` path (error handling elided): `dvfagg.Load(dir).Codes()`
  / `.Lookup(insee)`, `qpv.Load(dir).HasQPV(insee)`,
  `delinquance.Load(dir).Level(insee)`, `sensible.Load(dir).ZonesForCommune(insee)`, `communes.Default().All()` - reach for these whenever you need many communes
  at once instead of one address. Batch-capable sources are flagged `batch`
  in the catalog. Rank/filter on the row's derived methods
  (`EffectivePriceEURM2`, `EffectiveRentEURM2HC` = min(market, legal cap),
  `GrossYieldPct`, `PriceReliable`), not on raw fields - those rules live in
  the library so consumers don't re-derive them.

## Adding a new Source (checklist)

Copy a model: `sources/filoiris` (clean dataset-backed source) or `sources/gpe`
(spatial). Then:

1. `result.go` - `Result` + `IsEmpty()` + `Evidence`, with a package godoc.
2. `source.go` - `Name`/`Version`/`Options`/`Query`, and `init()` calls
   `gazetteer.Register(Name, func() any { return &Result{} })`.
3. `loader.go` + `transform.go` - only if it ships an embedded dataset
   (see [docs/datasets.md](docs/datasets.md); bootstrap via
   `gazetteer refresh --go-embed-update <name>`).
4. Wire it: one **roster entry** in `internal/roster/roster.go` (feeds both
   `factory.NewDefault` and the CLI), a renderer in `cmd/gazetteer/render.go`,
   and a **catalog descriptor** in `cmd/gazetteer/catalog.go`.
5. Tests + docs (`docs/sources.md`, README, godoc - this is the Definition of
   Done, not a follow-up).

The roster and catalog **completeness tests** fail until every registered
source has a roster entry and a descriptor, so neither the wiring nor the
machine-readable catalog can silently drift.
See [docs/plugins.md](docs/plugins.md) for out-of-tree plugins.

## How to work: the gate, CI, and commits

```bash
make tools      # once: installs goimports + golangci-lint
make hooks      # once: installs .githooks, so pre-commit runs `make precommit`
make precommit  # THE GATE: fmt-check + vet + lint + test + tidy-check
make test-race  # what CI adds on top; run it after touching anything concurrent
go test ./sources/dvf/ -run TestSomething -count=1   # one test
```

`make precommit` is the single completion gate. Green looks like `ok` or
`no test files` on every package and no other output: any `FAIL`, any lint
line, any gofmt or `go mod tidy` diff is a failure. It runs in seconds with a
warm Go cache, so there is no excuse for skipping it. Bypass it for a WIP
checkpoint with `git commit --no-verify` only. The hook chains to any global
hooks path first, so it never disables an external secret scanner.

**There IS push CI here** (`.github/workflows/ci.yml`): every push to `master`
and every pull request runs `make precommit` plus `make test-race` on
**ubuntu-latest, amd64**. A green local gate on an arm64 Mac does NOT prove the
Linux amd64 runner is green: architecture-dependent floating point, map
ordering under `-race`, and anything touching timing or the filesystem have all
broken there while passing locally. So: commit to `master`, push, then **watch
the run** (`gh run watch`, or `gh run list -L1`) and fix what it reports before
walking away.

## Invariants & footguns

- `zonescore.Options.Weights` **replaces** the default weight set wholesale - a
  partial map means "score only these axes", not "tweak a few". To tweak,
  use `zonescore.WeightsWith(profile, overrides)` (merges, validates axis
  names).
- `gazetteer refresh` is **idempotent** (a current dataset is skipped); safe on boot.
- **Dataset loaders are process-global and first-`Load(dir)`-wins**: the dir
  from the first call is cached for the process lifetime (dataset.Lazy). Two
  components disagreeing on DataDir silently share the first one's data.
- **The atomic `Query`/`QueryResult` path has no built-in politeness**: outside
  `Collect`, the ctx fallback is `gazetteer.DefaultHTTPClient`, a bare client
  with a `DefaultHTTPTimeout` (60 s) deadline and nothing else: no rate limits,
  no retries, no cache. For live sources, pass `Options.HTTPClient` (start from
  `factory.HostRateLimits()`) or use a factory-built Client.
- **Per-Source timeout: bounded via the factory, unbounded via the raw
  Builder.** `factory.NewDefault` / `NewDefaultWith` set
  `factory.DefaultPerSourceTimeout` (45 s) on the Client, so one wedged
  upstream cannot pin a whole `Collect` (the Source is cut, its Result is
  `StatusFailedTransient`, its siblings still land). Override with
  `factory.Options.PerSourceTimeout` (a negative value disables the bound);
  a hand-built `gazetteer.Builder` still defaults to NO bound and opts in via
  `WithPerSourceTimeout`.
- IRIS coverage is **Île-de-France only in practice**: the `iris` resolver and
  `logiris` are IDF-scoped datasets. `filoiris`'s dataset is *national*, but it
  only fires where `Listing.IRIS` is set - and `iris` (IDF-only) is the sole
  resolver that sets it, so non-IDF addresses get no IRIS and thus no `filoiris`.
- `oll` **excludes Paris intra-muros** (use `encadrement` for Paris rents).
- `gpe` (future Grand Paris Express stations) is **informational, not scored** - 
  future transit must not distort the yield-first-today score.
- Datasets ship **embedded in the binary**; the datadir (`~/.cache/gazetteer`)
  is an *optional* override populated by `refresh`, never required.
- **Evidence survives Dossier JSON as raw JSON only.** `Result.Evidence` is
  marshaled, but un-marshal restores it as `json.RawMessage` (no factory for
  evidence types); `Result.Err` round-trips as a plain string (Status
  survives - gate retries on it, not on `errors.Is` after a round-trip).
- **Gate a Result on `IsEmpty()`, never on `field != 0`.** Many numeric Result
  fields are plain values where `0` is a *legitimate* reading (e.g. `rpls` 0 %
  social housing - ~64 % of communes; a count of 0) - distinct from "no data".
  `IsEmpty()` (⇒ `StatusOKEmpty`) is the only correct "did this source find
  anything" test; comparing a field to zero silently drops real zeros.
- **Rent basis - CC vs HC.** `carteloyers` rents are *charges comprises*
  (field suffix `…CC`); `oll` and `encadrement` are *hors charges* (suffix
  `…HC` on encadrement; documented HC on oll's `Observed…` fields). Don't
  compare the raw fields across sources - different bases. Use
  `appraisal.RentValue`, which converts CC→HC (≈0.90) before blending.
- `taxefonciere.EstimatedEURPerYear` is an **order-of-magnitude estimate, not
  the exact bill** - a valeur-locative proxy understates high-value communes
  (Paris ≈ ½ the real figure). Compare communes with it; don't quote it as the sum due.
- `CollectSome` / `Builder.Without` / `factory.Options.Exclude` **ignore unknown
  Source names** (a typo'd name silently runs/keeps nothing) - they now log a
  warning, so watch the logs when a subset comes back unexpectedly empty.
- **A commune centre is a perfectly ordinary coordinate**, and that is the trap.
  It has a cadastral parcel, a DPE and a nearest metro station, so an
  address-granularity reading taken on one is wrong with no outward sign.
  `Listing.CoordPrecision` (a `banx.Precision`: `housenumber`, `street`,
  `locality`, `municipality`, or empty for "the geocoder did not report",
  which is NOT "coarse") records what Lat/Lon are the position OF. A source that
  reads at address granularity gates on it with
  `Listing.CoordsAtLeast(banx.PrecisionX)`; commune-keyed sources ignore it.
  Never take `Coords()` alone as permission to attribute a parcel, a building or
  a distance to a dwelling.
- **Measure to a polygon's BOUNDARY, not to its nearest vertex.**
  `geopoly.MultiPolygon.BoundaryDistanceM` (used by `geoindex`) is the correct
  distance to a zone: a vertex distance overstates the gap along a long straight
  edge, which quietly moves an address out of a QPV or a sensitive perimeter it
  actually borders. A point inside the shape is at distance 0.
- **A behaviour change owes a `Version` bump** in the source's `source.go`. The
  constant is how a stored or cached Result is told apart from one produced by
  the corrected logic; changing what a source returns without moving it makes
  old and new data indistinguishable.

## Where things live

```
gazetteer/            core types: Builder, Client, Source, Result, Dossier, Get[T]
factory/              one-call wiring of every stable source (NewDefault,
                      SourceOverrides, HostRateLimits, Offline/LiveSourceNames)
internal/roster/      THE single source roster (one entry wires factory + CLI)
sources/<name>/       one package per source (uniform shape, see above)
appraisal/            PricePerM2 / RentValue / HazardProfile consolidation
appraisal/zonescore/  the 0-100 zone score + Compare + Personas
overview/             offline per-commune batch join (CommuneOverview) for screening
dataset/              embed+datadir+refresh pipeline (ship your own datasets)
helpers/<name>/       standalone building blocks (docs/helpers.md): banx, httpx, …
cmd/gazetteer/        the CLI (+ the source catalog)
docs/                 long-form reference (start at docs/readme.md)
```

## Definition of done

- [ ] `make precommit` green (fmt, vet, lint, tests, tidy), and `make test-race`
      too if anything concurrent, cached or time-dependent moved.
- [ ] Behaviour of a source changed? Its `Version` bumped.
- [ ] New source? Roster entry, CLI renderer and catalog descriptor all present
      (the completeness tests fail until they are), plus `docs/sources.md` and
      godoc on every Result field, with its unit.
- [ ] Public API changed? Checked against `../locador` and
      `../gazetteer-fr-plugins`, which compile against it, and said so in the
      report.
- [ ] Docs updated in the same commit: godoc first (it is the data dictionary),
      then the relevant `docs/` page, `README.md` if a command or an output
      changed, this file if an invariant or the layout moved.
- [ ] Every new numeric field carries its unit in the name or the godoc.
- [ ] No typographic dash in the diff.
- [ ] Nothing personal added: no real inhabited address as somebody's, no name,
      no home path.
- [ ] Committed to `master` and pushed, then **CI watched to green**. The Linux
      amd64 runner is not the machine you tested on.
- [ ] Anything a human must run by hand said explicitly, in particular
      `gazetteer refresh` when a dataset vintage moved (it needs the network and
      rewrites embedded data).

## Full reference

[docs/concepts.md](docs/concepts.md) · [docs/helpers.md](docs/helpers.md) ·
[docs/sources.md](docs/sources.md) ·
[docs/cli.md](docs/cli.md) · [docs/datasets.md](docs/datasets.md) ·
[docs/plugins.md](docs/plugins.md) · [docs/testing.md](docs/testing.md) ·
[docs/caching.md](docs/caching.md) ·
[docs/circuit_breakers.md](docs/circuit_breakers.md)
