# gazetteer

A Go library that compiles geographic and real-estate data about French
addresses from multiple sources. Given a free-text address (or a fully
populated `Listing`), it queries a configurable set of sources in parallel
and returns a typed `Dossier` aggregating every result.

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

	if r, ok := gazetteer.Get[*dvf.Result](dossier, dvf.Name); ok {
		fmt.Printf("DVF: sample_size=%d, level=%s\n",
			r.SampleSize, r.Evidence.LevelUsed)
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

`factory.NewDefault` wires every stable Source, including `osm_transit`
(it ships an embedded station catalog with a live Overpass fallback, so
no setup is needed). Only `bdnb` is left out of the default set — add it
via the Builder path when you have an API key and quota to spend.

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
| `nuisances`    | IDF environmental-nuisance grid (noise + air, 500 m cells)         |

Market data:

| Source         | What it provides                                                 |
|----------------|------------------------------------------------------------------|
| `dvf`          | Demandes de Valeurs Foncières — historical transaction prices    |
| `locservice`   | Rental market reference data (tension, médiane €/m²)             |
| `carteloyers`  | National rent observatory tiers                                  |
| `oll`          | Observed market rents by zone (Observatoires Locaux des Loyers)  |
| `encadrement`  | Rent control caps (Paris, Plaine Commune + Est Ensemble 93, Lyon) |
| `lovac`        | Vacancy rate per commune from the LOVAC fiscal file              |
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
| `qpv`          | Quartiers Prioritaires de la politique de la Ville membership    |
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

Plus an `appraisal/` layer combining the above into rent and price
estimates with confidence bands, and `appraisal/zonescore`, a yield-first
composite zone score (0–100) with an explainable per-axis breakdown
(rendement, tension, solvabilité, sécurité, fiscalité, accès).

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

See [doc/cli.md](doc/cli.md) for the full reference.

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
[doc/datasets.md](doc/datasets.md) for the full model and how to make an
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
