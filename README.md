# gazetteer

A Go library that compiles geographic and real-estate data about French
addresses from multiple sources. Given a `Listing` (address + coordinates
+ property attributes), it queries a configurable set of sources in
parallel and returns a typed `Dossier` aggregating every result.

## Status

Alpha. The API may change before v1. Tagging and stable releases are
deferred until the surface stabilises.

## Quick start

```go
package main

import (
	"context"
	"fmt"

	"github.com/bpineau/gazetteer"
	"github.com/bpineau/gazetteer/dvf"
	"github.com/bpineau/gazetteer/osm"
)

func main() {
	client, _ := gazetteer.NewBuilder().
		With(dvf.NewSource(dvf.Options{})).
		With(osm.NewSource(osm.Options{})).
		Build()

	listing := gazetteer.Listing{
		Address: "1 rue de Rivoli",
		City:    "Paris",
		Zip:     "75001",
		Lat:     48.8566,
		Lon:     2.3522,
	}

	d := client.Collect(context.Background(), listing)

	if r, ok := dvf.From(d); ok {
		fmt.Println("DVF median EUR/m²:", r.MedianEurPerM2)
	}
}
```

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
| `filosofi`     | INSEE Filosofi income/poverty statistics by IRIS                 |
| `taxefonciere` | Property tax ratios by commune                                   |
| `vacance`      | Vacancy taxation status by commune                               |

Plus an `appraisal/` layer combining the above into rent + price
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
- **Cache** — pluggable backend for intermediate state (MemCache default)
- **Normalizer** — canonicalises a free-text address into a Listing

## Plugins

Out-of-tree source packages (private antibot scrapers, paid APIs, etc.)
implement the same `Source` interface and register their typed payload
via `gazetteer.Register` in `init()`. Callers wire them with
`builder.With(...)` like any official source.

## License

MIT. See [LICENSE](LICENSE).
