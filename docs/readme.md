# gazetteer — documentation

`gazetteer` is a Go library that compiles geographic and real-estate
data about French addresses from a configurable set of upstream sources.
Given a free-text address (or a fully populated `Listing`), it queries
every configured `Source` in parallel and returns a typed `Dossier`
aggregating every result.

This directory hosts the long-form reference docs that complement the
godoc found via `go doc github.com/bpineau/gazetteer/...`.

## Where to start

| Document                                | Audience                              |
|-----------------------------------------|---------------------------------------|
| [concepts.md](concepts.md)              | New users — mental model of the API   |
| [sources.md](sources.md)                | What each Source provides             |
| [datasets.md](datasets.md)              | Offline datasets + `refresh` / datadir|
| [plugins.md](plugins.md)                | Source authors                        |
| [circuit_breakers.md](circuit_breakers.md) | Source authors                     |
| [caching.md](caching.md)                | Source authors                        |
| [testing.md](testing.md)                | Library consumers writing tests       |
| [cli.md](cli.md)                        | End users of `cmd/gazetteer`          |

For runnable examples, look at `gazetteer/example_test.go` and the
per-source `example_test.go` files; they are reachable via
`go doc -examples ./...`.

## Project layout

```
gazetteer/         core types: Builder, Client, Source, Result, Dossier
gazetteer/gazettestest/  reusable test doubles (StubSource)
factory/           one-call wiring of every stable in-tree Source
appraisal/         consolidation across Sources (price/rent/hazard)
appraisal/zonescore/  yield-first 0–100 zone score + multi-zone Compare
sources/<name>/    one package per data source
helpers/<name>/    reusable utilities (banx, httpx, kvcache, circuit, ...)
cmd/gazetteer/     command-line front-end
internal/          implementation detail; no public API
```

## Status

Alpha. The API may break before v1; releases are deferred until the
surface stabilises. New `Source` plugins land out-of-tree under any
import path and are wired via `Builder.With`.

## License

MIT. See `LICENSE`.
