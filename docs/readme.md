# gazetteer — documentation

`gazetteer` brings back rich, typed, well-extracted data about a French address
across every dimension that matters when evaluating a property as an investment
(price, rents, demand, solvency, taxes, safety, transport, hazards, building
quality, social/regulatory context, …). Each dimension is a `Source` returning a
fully-typed `Result`; `Client.Collect` runs them in parallel into a `Dossier`.
**That typed data is the point** — the `appraisal`/`zonescore` score on top is an
optional convenience layer. For the per-source data dictionary see
[sources.md](sources.md) and the per-source `go doc`.

This directory hosts the long-form reference docs that complement the
godoc found via `go doc github.com/bpineau/gazetteer/...`.

## Where to start

| Document                                | Audience                              |
|-----------------------------------------|---------------------------------------|
| [../AGENTS.md](../AGENTS.md)            | **AI coding agents** + fast human onboarding (read first) |
| [sources.json](sources.json)            | Machine-readable capability map of every source |
| [concepts.md](concepts.md)              | New users — mental model of the API   |
| [sources.md](sources.md)                | What each Source provides             |
| [datasets.md](datasets.md)              | Offline datasets + `refresh` / datadir|
| [helpers.md](helpers.md)                | **Standalone building blocks** (httpx, banx, communes, …) for any FR real-estate/geo app |
| [plugins.md](plugins.md)                | Source authors                        |
| [circuit_breakers.md](circuit_breakers.md) | Source authors                     |
| [caching.md](caching.md)                | Source authors                        |
| [testing.md](testing.md)                | Library consumers writing tests       |
| [cli.md](cli.md)                        | End users of `cmd/gazetteer`          |

For runnable examples, look at `gazetteer/example_test.go` and
`appraisal/zonescore/example_test.go` (run them with
`go test -run Example ./gazetteer/ ./appraisal/zonescore/`).

## Project layout

```
gazetteer/         core types: Builder, Client, Source, Result, Dossier
gazetteer/gazettestest/  reusable test doubles (StubSource)
factory/           one-call wiring of every stable in-tree Source
appraisal/         consolidation across Sources (price/rent/hazard)
appraisal/zonescore/  yield-first 0–100 zone score + multi-zone Compare
overview/          offline per-commune batch join (CommuneOverview) for screening
sources/<name>/    one package per data source
helpers/<name>/    standalone building blocks (see helpers.md) — public API
dataset/           embed+datadir+refresh pipeline (ship your own datasets)
cmd/gazetteer/     command-line front-end
internal/          implementation detail; no public API
```

## Status

Pre-v1, tagged releases consumed by pinning applications — see the
stability tiers in the top-level [README](../README.md#status--stability).
New `Source` plugins land out-of-tree under any import path and are wired
via `Builder.With` (or `factory.Options.SourceOverrides` to swap a roster
source).

## License

MIT. See `LICENSE`.
