# gazetteer

A Go library that, given a French address, brings back **rich, typed,
well-extracted data across dimension that matters when evaluating a
property as an investment**:  price, rents, rental demand, tenant solvency,
taxes, safety, transport, hazards, building quality, the social and regulatory
context, and more. Each dimension comes from a dedicated `Source` as a
fully-typed `Result` with [documented, unit-bearing fields](docs/sources.md);
a `Client.Collect` runs them in parallel and returns a typed `Dossier`.

An optional, thin convenience layer (`appraisal` + `appraisal/zonescore`)
consolidates a few dimensions and composites them into an high-level score .

> **AI coding agents:** read [AGENTS.md](AGENTS.md) first (also linked as
> `CLAUDE.md`), then run `gazetteer sources catalog --json`. Run `make hooks`
> once to install the pre-commit gate (`make precommit`).

## Status

Alpha. The API may change before v1. Tagging and stable releases are
deferred until the surface stabilises.

## Quick start

The `factory` package wires every stable in-tree source with sensible
defaults so most callers need fewer than 10 lines:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/bpineau/gazetteer/factory"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/carteloyers"
	"github.com/bpineau/gazetteer/sources/dvf"
)

func main() {
	ctx := context.Background()

	// One-shot setup: builds httpx + BAN + every stable Source and
	// installs the BAN-backed Normalizer on the Client.
	client, err := factory.NewDefault(ctx)
	if err != nil {
		log.Fatalf("factory: %v", err)
	}

	// Canonicalise the user's free-text address into a Listing.
	listing, err := client.Normalize(ctx, "1 rue de Rivoli, 75001 Paris")
	if err != nil {
		log.Fatalf("normalize: %v", err)
	}

	// Run every configured source in parallel; aggregate the typed
	// payloads into a Dossier.
	dossier := client.Collect(ctx, listing)

	// Pull out exactly the typed data you care about. The unit always
	// lives in the field name (see docs/sources.md): DVF prices are
	// integer *centimes*, so divide by 100 for €/m².
	if r, ok := gazetteer.Get[*dvf.Result](dossier, dvf.Name); ok && r.ValueEURPerM2Cents != nil {
		fmt.Printf("Sale price: %d €/m² over %d sales (%s confidence)\n",
			*r.ValueEURPerM2Cents/100, r.SampleSize, r.Confidence)
	}

	// A second dimension, a second Result shape: carteloyers exposes the
	// reference rent as a float in €/m²/month, charges comprises (CC).
	if r, ok := gazetteer.Get[*carteloyers.Result](dossier, carteloyers.Name); ok && !r.IsEmpty() {
		fmt.Printf("Reference rent: %.1f €/m²/month CC (%.1f–%.1f)\n",
			r.LoyerMedEURPerM2CC, r.LoyerLowEURPerM2CC, r.LoyerHighEURPerM2CC)
	}
}
```

Callers that need to add an out-of-tree Source or override a default
should use `factory.BuilderDefault`, chain `.With(plugin)`, then call
`.Build()`:

```go
b, err := factory.BuilderDefault(ctx, factory.Options{})
if err != nil { log.Fatal(err) }
client, _ := b.With(myPlugin).Build()
```

`factory.NewDefault` wires **every** stable Source, including `osm_transit`
(an embedded station catalog with a live Overpass fallback, so no setup is
needed) and `bdnb` (a live building API with a per-key rolling quota). To
prune Sources you never consume — cutting their latency and failure surface —
pass a deny-list:

```go
client, _ := factory.NewDefaultWith(ctx, factory.Options{Exclude: []string{"bdnb"}})
```

`Exclude` is applied to the full default roster, so in-tree Sources added
later still flow in automatically. (The `gazetteer` CLI omits `bdnb` from
its default `query`/`appraise` set for the same quota reason; pass
`--source bdnb` to include it.) Two finer-grained controls:

- `builder.Without(names…)` drops Sources from a pre-populated Builder
  (e.g. `factory.BuilderDefault`) before `.Build()`.
- `client.CollectSome(ctx, listing, names…)` collects only a named subset
  on an existing Client — e.g. fetch the cheap embedded Sources first,
  before paying for the slow live APIs.

## Sources shipped

Building / energy / risk:

| Source         | What it provides                                                 |
|----------------|------------------------------------------------------------------|
| `ademe`        | DPE (energy performance certificates) at the address             |
| `bdnb`         | Base de Données Nationale des Bâtiments — building age, type     |
| `cadastre`     | Cadastral parcel id, contenance, viewer link (+ opt-in bâti)     |
| `iris`         | INSEE IRIS code/name/type at the address (also resolves `Listing.IRIS`) |
| `dpedist`      | DPE class distribution per commune (passoire share F+G)          |
| `georisques`   | Natural and technological hazards (flood, soil, industrial)      |
| `catnat`       | Per-commune history of recognised natural-disaster decrees        |
| `cdsr`         | Nearby region-flagged distressed condominiums (IDF copro risk)   |
| `rnc`          | Copropriété context from the Registre National d'Immatriculation (syndic, lots, QPV; triage hint, no distress verdict) |
| `nuisances`    | IDF environmental-nuisance grid (noise + air, 500 m cells)         |

Market data:

| Source         | What it provides                                                 |
|----------------|------------------------------------------------------------------|
| `dvf`          | Demandes de Valeurs Foncières — historical transaction prices    |
| `dvfagg`       | Per-commune DVF price aggregate (median €/m² + dispersion, offline) — the batch complement to `dvf` |
| `locservice`   | Rental market reference data (tension, médiane €/m²)             |
| `carteloyers`  | National rent observatory tiers                                  |
| `oll`          | Observed market rents by zone (Observatoires Locaux des Loyers)  |
| `encadrement`  | Rent control caps (Paris, Plaine Commune + Est Ensemble 93, Lyon) |
| `lovac`        | Vacancy rate per commune from the LOVAC fiscal file              |
| `sitadel`      | New-housing pipeline per commune — permits authorised + housing starts (SDES Sitadel) |
| `taxefonciere` | Property tax ratios by commune                                   |

Commune-level signals for the investor:

| Source         | What it provides                                                 |
|----------------|------------------------------------------------------------------|
| `filosofi`     | INSEE Filosofi income / poverty statistics by commune            |
| `filoiris`     | INSEE Filosofi income / poverty at IRIS (sub-commune) level      |
| `logiris`      | INSEE census housing at IRIS: renter / social-housing / vacancy  |
| `chomage`      | INSEE local unemployment rate by zone d'emploi (quarterly)       |
| `delinquance`  | SSMSI État 4001 — per-commune crime indicators                   |
| `zonageabc`    | Official A bis / A / B1 / B2 / C tension classification          |
| `zonetendue`   | "Zone tendue" + TLV-2013 + tendue-touristique flags              |
| `anct`         | Action Cœur de Ville / Petites Villes de Demain / ORT membership |
| `qpv`          | Quartiers Prioritaires de la politique de la Ville — point-in-polygon (is the address inside a QPV?), commune fallback |
| `cartofriches` | Cerema brownfield inventory aggregated per commune               |
| `education`    | Count of open schools per commune (live API)                     |
| `bpe`          | INSEE BPE — curated commerce / health / services counts          |
| `rpls`         | % social housing (loi SRU) per commune                           |
| `vacance`      | INSEE census demographic vacancy rate (per arrondissement)       |
| `ips_ecoles`   | DEPP median IPS over écoles primaires (per arrondissement)       |

Transport:

| Source         | What it provides                                                 |
|----------------|------------------------------------------------------------------|
| `osm`          | Walking distance to nearest métro / RER / tram / train station   |
| `gpe`          | Nearest *future* Grand Paris Express station + line + distance    |

External links:

| Source         | What it provides                                                 |
|----------------|------------------------------------------------------------------|
| `links`        | Deep links to useful third-party tools for the address — maps, prices/DVF, Géorisques, PLU, INSEE fiche (built from coordinates / INSEE / address, no HTTP) |

### Optional convenience layer

On top of the typed `Dossier` (the main product) sits a thin, *optional*
high-level API — skip it if you just want the data. `appraisal/` consolidates a
few dimensions into rent and price estimates with confidence bands, and
`appraisal/zonescore` composites them into a 0–100 score with an explainable
per-axis breakdown (rendement, tension, solvabilité, sécurité, fiscalité, accès)
and selectable weight presets (`yield` / `balanced` / `patrimoine` /
`transport`, via the CLI `--profile`). The IRIS-level income (`filoiris`) and
housing (`logiris`) sources sharpen the solvabilité and tension axes where
neighbourhoods diverge within a commune.

## Batch / commune-level data

The per-address flow above (`Normalize` → `Collect` → `Dossier`) answers
"tell me everything about *this* address". For the inverse — screening
*every* commune at once — the `overview` package joins the embedded,
commune-keyed Sources **offline** into one row per commune:

```go
import "github.com/bpineau/gazetteer/overview"

rows, _ := overview.Build(overview.Options{Depts: []string{"75", "93", "94"}})
for _, c := range rows {
    fmt.Printf("%s %s: %.0f €/m², loyer %.1f €/m² HC, délinquance %s\n",
        c.INSEE, c.Name, c.PriceMedianEURM2, c.RentMarketEURM2HC, c.DelinquanceLevel)
}
```

`overview.Build` does no network I/O. It is keyed off the communes that
have DVF price data (`dvfagg`) and merges price, market rent, the
encadrement cap, income, vacancy, taxe foncière, QPV, zonage and nearby
transit lines per commune (`Depts` empty = all communes nationally).

The same commune-keyed shortcut is available per Source via **batch-read
helpers** that skip the full `Query`/`Listing` path — load an index once,
then read many communes:

```go
idx, _ := dvfagg.Load("")               // "" = embedded dataset
for _, insee := range idx.Codes() {     // every commune with price data
    r, _ := idx.Lookup(insee)
    _ = r.PriceMedianEURM2
}
```

Companion helpers: `qpv.Index.HasQPV(insee)`,
`delinquance.Index.Level(insee)`, `communes.Table.All()`.

## CLI

Install:

```bash
go install github.com/bpineau/gazetteer/cmd/gazetteer@latest
```

Sub-commands:

```
gazetteer sources list                  # list every registered Source + version
gazetteer sources doc <name>            # print a Source's typed Result skeleton
gazetteer query     [flags] <addr>      # run every Source against an address
gazetteer appraise  [flags] <addr>      # query + consolidated price/rent/hazard + zone score
gazetteer compare   [flags] <a1> <a2>…  # rank addresses best-first by yield-first zone score
gazetteer normalize [--json] <addr>     # resolve a free-text address to a Listing
gazetteer refresh   [sources|all]       # download/rebuild datasets into the datadir
gazetteer version                       # build version
```

`query` and `appraise` honour the listing's property attributes when
supplied — DVF, encadrement, taxe-foncière and the rental Sources need
them to produce a useful answer:

```
--property-type apartment|house|land|commercial   (default: apartment)
--surface <m²>
--rooms <N>
--source <comma-separated names>                  (default: every Source the CLI knows)
--json                                            (emit the full Dossier)
--verbose --dump                                  (debug)
```

See [docs/cli.md](docs/cli.md) for the full reference.

## Datasets & cache

The offline Sources (rents, taxe foncière, vacancy, crime, schools, …) ship a
pre-indexed dataset embedded in the binary, so they work out of the box with
no setup. A flat **data directory** (default `~/.cache/gazetteer`, overridable
with `--data-dir` or `$GAZETTEER_DATA_DIR`) lets you override or refresh those
datasets from upstream **without rebuilding the library**: when a refreshed
copy is present there it takes precedence over the embedded one; otherwise the
embedded copy is used.

`gazetteer refresh` downloads each Source's real upstream file(s) and rebuilds
its dataset into the datadir:

```bash
gazetteer refresh --list        # show each artifact: where it loads from, size, refreshable
gazetteer refresh               # download + (re)build every dataset into the datadir
gazetteer refresh delinquance   # just one source
```

`refresh` is **idempotent**: a dataset already present and current in the
datadir is skipped untouched — no download, no rebuild. Only the first run
does work, so you can safely call it on every start (e.g. a one-time warm-up
on boot); pass `--force` to rebuild regardless. Library callers use the same
contract via `dataset.Refresh`:

```go
sets := /* collect dataset.Set from your sources (DatasetProvider) */
_, err := dataset.Refresh(ctx, httpClient, sets, dataset.RefreshOptions{}) // idempotent; Force to rebuild
```

Maintainers re-embed a refreshed dataset with `gazetteer refresh
--go-embed-update` (rebuilds into the datadir, then copies the artifact back
into `sources/<name>/data/` for re-commit). See
[docs/datasets.md](docs/datasets.md) for the full model and how to make an
out-of-tree Source refreshable.

## Concepts

- **Listing** — the universal input (address + coords + property attrs)
- **Source** — a named, versioned data origin: `Query(ctx, listing) → (payload, error)`
- **Result** — the framework envelope around a Source's typed payload
- **Dossier** — the aggregated output of one `Client.Collect` call
- **Builder / Client** — configure sources, then run them in parallel
- **Cache** — pluggable backend for intermediate state (in-memory default)
- **Normalizer** — canonicalises a free-text address into a Listing

## Plugins

Out-of-tree source packages implement the same `Source` interface and
register their typed payload via `gazetteer.Register` in `init()`.
Callers wire them with `builder.With(...)` like any official source —
the framework itself has no compile-time knowledge of which sources are
available.

## License

MIT. See [LICENSE](LICENSE).
