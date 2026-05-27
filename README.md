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
	// installs the BAN-backed Normalizer behind gazetteer.NormalizeAddress.
	client, err := factory.NewDefault(ctx)
	if err != nil {
		log.Fatalf("factory: %v", err)
	}

	// Canonicalise the user's free-text address into a Listing.
	listing, err := gazetteer.NormalizeAddress(ctx, "1 rue de Rivoli, 75001 Paris")
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

Callers that need to add an out-of-tree Source (e.g. `bienici`,
`castorus`) or override a default should use `factory.BuilderDefault`,
chain `.With(plugin)`, then call `.Build()`:

```go
b, err := factory.BuilderDefault(ctx, factory.Options{})
if err != nil { log.Fatal(err) }
client, _ := b.With(myPlugin).Build()
```

`factory.NewDefault` does not currently wire the OSM transit source
(it needs an offline station catalog); add it explicitly via the
Builder path when needed.

## Sources shipped

| Source         | What it provides                                                 |
|----------------|------------------------------------------------------------------|
| `ademe`        | DPE (energy performance certificates)                            |
| `osm`          | Walking distance to nearest métro / RER / tram / train station   |
| `bdnb`         | Base de Données Nationale des Bâtiments — building age, type     |
| `georisques`   | Natural and technological hazards (flood, soil, industrial)      |
| `locservice`   | Rental market reference data                                     |
| `dvf`          | Demandes de Valeurs Foncières — historical transaction prices    |
| `carteloyers`  | National rent observatory tiers                                  |
| `encadrement`  | Rent control zones (Paris, Lille, Lyon, etc.)                    |
| `filosofi`     | INSEE Filosofi income / poverty statistics by IRIS               |
| `taxefonciere` | Property tax ratios by commune                                   |
| `vacance`      | Vacancy taxation status by commune                               |

Plus an `appraisal/` layer combining the above into rent and price
estimates with confidence bands.

## CLI

Install:

```bash
go install github.com/bpineau/gazetteer/cmd/gazetteer@latest
```

Sub-commands:

```
gazetteer sources          # list registered sources
gazetteer query   <addr>   # run every source against a free-text address
gazetteer appraise <addr>  # produce rent + price estimates
gazetteer normalize <addr> # canonicalise a free-text address
gazetteer refresh          # refresh local data files
gazetteer setup            # one-shot initial data fetch
```

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
