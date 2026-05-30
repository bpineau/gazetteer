# AGENTS.md — orienting guide for AI coding agents (and humans in a hurry)

This file is the **canonical entry point** for working in this repo. Read it
first; it is written to be ingested in one shot. Deeper references live in
[`docs/`](docs/). Everything here is kept honest by tests — if a fact below is
wrong, that's a bug.

## What this is — the data is the product

`gazetteer` is a Go library that, given a French address, brings back **rich,
typed, well-extracted data across every dimension that matters when an investor
evaluates a property** — price, rents, rental demand, tenant solvency, taxes,
safety, transport, hazards, building quality, the social/regulatory context, and
more. Each dimension comes from a dedicated `Source` as a **fully-typed `Result`
with documented, unit-bearing fields**. That typed data *is* the point.

An optional, thin convenience layer sits on top — `appraisal.*` consolidates a
few dimensions (price/rent/hazard) and `appraisal/zonescore` composites them
into a 0–100 score. Treat it as a sample high-level API, not the goal: most
callers want the underlying Results, not the score.

A CLI (`cmd/gazetteer`) is the fastest way to explore the data.

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

// Pull the typed data you care about — this is the main use of the lib:
if r, ok := gazetteer.Get[*dvf.Result](dossier, dvf.Name); ok && !r.IsEmpty() {
    // r is *dvf.Result; every field is documented with its unit (see godoc)
}

// Optional: the convenience synthesis layer on top.
score := zonescore.Compute(dossier)
```

## The data is the point — discovering Result types + field meanings

Most of your work here is "which source gives dimension X, and what does each
field mean (units!)". The answers, easiest first:

1. **`gazetteer sources catalog --json`** (or `docs/sources.json`) — every
   source's summary, required inputs, coverage, the dimension it covers, and its
   `result_schema` (field names). Start here to pick a source.
2. **`go doc github.com/bpineau/gazetteer/sources/<name> Result`** — the
   authoritative field-by-field meaning **with units**. Every `Result` field
   carries a godoc comment (e.g. DVF prices are `…Cents` integers, OLL rent is
   `€/m²/month`, shares are `%`). This is the canonical data dictionary.
3. **`gazetteer sources doc <name>`** — the Result's JSON shape (field names +
   zero values) for a quick wire-format glance.
4. [docs/sources.md](docs/sources.md) — prose: what each source provides and the
   key Result fields.

Convention you can rely on: **units live in the field name or its godoc** — cents
vs euros, €/m² vs €/m²/month, % , metres, counts. When in doubt, read the field's
godoc; never guess a unit.

## The uniform Source contract — learn one, know all

**Every** package under `sources/<name>/` has the *same* shape. Once you've used
one source you've used all of them:

| Symbol | Meaning |
|---|---|
| `const Name` | the registry key, e.g. `dvf.Name == "dvf"` |
| `const Version` | bumps when logic changes |
| `type Options struct{ …; DataDir string }` | config; zero value is usually valid |
| `func NewSource(Options) *Source` | constructor |
| `func Query(ctx, Options, Listing) (*Result, error)` | **atomic helper** — run one source without the builder |
| `type Result struct{ …; Evidence Evidence }` | the typed payload; `Evidence` is a `json:"-"` provenance sidecar |
| `func (*Result) IsEmpty() bool` | true ⇒ "ran fine, no data for this address" |

Pull a source's result out of a Dossier with the generic accessor:

```go
r, ok := gazetteer.Get[*filoiris.Result](dossier, filoiris.Name)
```

## Fastest way to explore — the CLI is self-describing

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

## Inputs cheat-sheet — what each `Listing` field unlocks

`Normalize()` fills INSEE, Lat/Lon and IRIS from free text. If a source returns
empty, the usual cause is a **missing input** or **out-of-coverage** address.

| Listing field | Unlocks |
|---|---|
| `INSEE` (5-digit) | most commune-level sources (filosofi, taxefonciere, delinquance, …) |
| `Lat` / `Lon` | spatial sources: cadastre, cdsr, gpe, nuisances, osm_transit, georisques |
| `IRIS` (9-digit) | `filoiris`, `logiris` — **Île-de-France only**; set by the `iris` source / normalizer |
| `SurfaceM2` | DVF €-total, taxe-foncière estimate |
| `Rooms` | carteloyers, oll, encadrement (typology bucket) |
| `PropertyType` | DVF + encadrement eligibility (default `apartment`) |

## Reading results — the empty/error model (read before debugging)

| Outcome | Meaning | What to do |
|---|---|---|
| `Status==OK`, `IsEmpty()==false` | real data | use it |
| `Status==OKEmpty` / `IsEmpty()==true` | source ran, **no data for this address** — NOT an error | check the source's required input + coverage in the catalog |
| `Status==Failed*` + `Result.Err != nil` | real failure (transient / permanent / antibot) | inspect `Err`; transient = retryable |
| `errors.Is(err, gazetteer.ErrInsufficientInputs)` | you didn't supply a required input | see the cheat-sheet above |
| `errors.Is(err, gazetteer.ErrSourceCircuitTripped)` | upstream tripped a breaker this run | transient; fresh run resets |

## Debugging recipes

- **"source X returned empty"** → Does the Listing carry X's required input
  (cheat-sheet)? Is the address inside X's coverage (catalog `coverage`)? Run
  `gazetteer query --verbose --source X "<addr>"`.
- **"the number looks wrong"** → every Result has an `Evidence` sidecar with
  provenance (which tier/zone/dataset year it used). Inspect it.
- **"it's slow"** → only the *live-HTTP* sources cost latency (dvf, georisques,
  education, ademe, cadastre). Offline sources are instant. DVF is the usual
  culprit; it's already optimised (section prefilter + `dvf.HostRateLimits()`).

## Optional convenience layer (appraisal + zonescore)

Sits on top of the Dossier; **skip it if you just want the data**. A sample
high-level API, not the project's purpose.

- `appraisal.PricePerM2`, `RentValue`, `HazardProfile` consolidate a few
  dimensions across sources (a source opts in by implementing
  `appraisal.PriceEstimator` / `RentEstimator` / `HazardReporter`).
- `appraisal/zonescore.Compute(dossier, opts…)` → a 0–100 score over 6 axes
  (rendement, tension, solvabilité, sécurité, fiscalité, accès);
  `zonescore.Compare(...)` ranks several addresses; weight presets via
  `zonescore.Personas` / the CLI `--profile`. The catalog's `feeds` field says
  which source drives which axis.

## Adding a new Source (checklist)

Copy a model: `sources/filoiris` (clean dataset-backed source) or `sources/gpe`
(spatial). Then:

1. `result.go` — `Result` + `IsEmpty()` + `Evidence`, with a package godoc.
2. `source.go` — `Name`/`Version`/`Options`/`Query`, and `init()` calls
   `gazetteer.Register(Name, func() any { return &Result{} })`.
3. `loader.go` + `transform.go` — only if it ships an embedded dataset
   (see [docs/datasets.md](docs/datasets.md); bootstrap via
   `gazetteer refresh --go-embed-update <name>`).
4. Wire it: `factory/factory.go`, `cmd/gazetteer/sources_registry.go`, a
   renderer in `cmd/gazetteer/render.go`, and a **catalog descriptor** in
   `cmd/gazetteer/catalog.go`.
5. Tests + docs (`docs/sources.md`, README, godoc — this is the Definition of
   Done, not a follow-up).

The catalog **completeness test** fails until every registered source has a
descriptor, so the machine-readable catalog can never silently drift.
See [docs/plugins.md](docs/plugins.md) for out-of-tree plugins.

## Invariants & footguns

- `zonescore.Options.Weights` **replaces** the default weight set wholesale — a
  partial map means "score only these axes", not "tweak a few".
- `gazetteer refresh` is **idempotent** (a current dataset is skipped); safe on boot.
- The IRIS sources (`iris`, `filoiris`, `logiris`) cover **Île-de-France only**.
- `oll` **excludes Paris intra-muros** (use `encadrement` for Paris rents).
- `gpe` (future Grand Paris Express stations) is **informational, not scored** —
  future transit must not distort the yield-first-today score.
- Datasets ship **embedded in the binary**; the datadir (`~/.cache/gazetteer`)
  is an *optional* override populated by `refresh`, never required.

## Where things live

```
gazetteer/            core types: Builder, Client, Source, Result, Dossier, Get[T]
factory/              one-call wiring of every stable source (NewDefault)
sources/<name>/       one package per source (uniform shape, see above)
appraisal/            PricePerM2 / RentValue / HazardProfile consolidation
appraisal/zonescore/  the 0–100 zone score + Compare + Personas
helpers/<name>/       banx, httpx, geopoly, geodist, communes, circuit, kvcache…
cmd/gazetteer/        the CLI (+ the source catalog)
docs/                 long-form reference (start at docs/readme.md)
```

## Full reference

[docs/concepts.md](docs/concepts.md) · [docs/sources.md](docs/sources.md) ·
[docs/cli.md](docs/cli.md) · [docs/datasets.md](docs/datasets.md) ·
[docs/plugins.md](docs/plugins.md) · [docs/testing.md](docs/testing.md)
