# gazetteer

A Go library that compiles geographic and real-estate data about French
addresses from multiple sources. Given a free-text address (or a fully
populated `Listing`), it queries a configurable set of sources in parallel
and returns a typed `Dossier` aggregating every result.

## Status

Alpha. The API may change before v1. Tagging and stable releases are
deferred until the surface stabilises.

## Quick start

Starting from a raw, user-supplied address string is the common case.
The library exposes `NormalizeAddress` to canonicalise it (BAN forward
geocode + INSEE commune lookup) before handing the resulting `Listing`
to the configured sources.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/dvf"
	"github.com/bpineau/gazetteer/sources/osm"
	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/communes"
	"github.com/bpineau/gazetteer/helpers/httpx"
)

func main() {
	ctx := context.Background()

	// One-time setup: install the default BAN-backed Normalizer so
	// gazetteer.NormalizeAddress can resolve a raw string into a
	// Listing populated with INSEE, postcode, lat/lon.
	hc, err := httpx.New(httpx.Options{})
	if err != nil {
		log.Fatalf("httpx: %v", err)
	}
	gazetteer.SetDefaultNormalizer(
		gazetteer.NewBANNormalizer(banx.NewBANClient(hc), communes.MustDefault()),
	)

	// Normalise the user's address.
	listing, err := gazetteer.NormalizeAddress(ctx, "1 rue de Rivoli, 75001 Paris")
	if err != nil {
		log.Fatalf("normalize: %v", err)
	}

	// Configure which sources to query. Sources with strict
	// dependencies (DVF needs an HTTP client and a geocoder; OSM
	// needs a station catalog loaded later via UpdateCatalog) are
	// shown here; sources whose Options have a zero-value default
	// (ademe, georisques, locservice, …) can be constructed with
	// `Options{}` and will fall back to gazetteer.HTTPClientFrom(ctx).
	dvfSource := dvf.NewSource(dvf.Options{
		HTTP:     hc,
		Geocoder: banx.NewBANClient(hc),
	})
	// osm.NewSource(Options{}) returns immediately; Query will return
	// ErrNoCatalog until the catalog is installed via UpdateCatalog
	// (typically by a background refresh goroutine).
	osmSource := osm.NewSource(osm.Options{})

	client, err := gazetteer.NewBuilder().
		With(dvfSource).
		With(osmSource).
		Build()
	if err != nil {
		log.Fatalf("build: %v", err)
	}

	// Collect runs every configured source in parallel and aggregates
	// their typed payloads into a Dossier.
	dossier := client.Collect(ctx, listing)

	if r, ok := dvf.From(dossier); ok {
		fmt.Printf("DVF: sample_size=%d, level=%s\n",
			r.SampleSize, r.Evidence.LevelUsed)
	}
	if r, ok := osm.From(dossier); ok {
		fmt.Printf("OSM: %d transit station(s) in range\n", len(r.Stations))
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
