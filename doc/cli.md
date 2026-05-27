# `gazetteer` command-line tool

The `cmd/gazetteer` binary is a thin front-end over the library. Use it
to look up an address, inspect the registered Source catalogue, or
sanity-check a wiring during development.

## Install

```bash
go install github.com/bpineau/gazetteer/cmd/gazetteer@latest
```

## Sub-commands

```
gazetteer query      [--source dvf,osm_transit,...] [--json] [--verbose] [--dump] <addr>
gazetteer appraise   [--source dvf,osm_transit,...] [--json] [--verbose] [--dump] <addr>
gazetteer normalize  [--json] [--verbose]                                          <addr>
gazetteer sources    list
gazetteer sources    doc       <name>
gazetteer refresh    <source>|all
gazetteer version
```

Run `gazetteer <cmd> -h` for the per-sub-command flag list.

### `query`

Run every configured Source against an address; print either a
per-source human summary or the full Dossier as JSON.

```bash
$ gazetteer query "1 rue de Rivoli, 75001 Paris"

$ gazetteer query --source dvf,osm_transit "10 rue Dareau, 75014 Paris"

$ gazetteer query --json "1 rue de Rivoli, 75001 Paris" | jq .
```

- `--source` — comma-separated Source names. Default: every Source
  the CLI knows how to instantiate.
- `--json` — emit the full Dossier as indented JSON instead of the
  human summary.
- `--verbose` — DEBUG-level slog output to stderr.
- `--dump` — log raw HTTP request/response payloads for Sources that
  honour the flag (`gazetteer.WithDebugDump`).

### `appraise`

Same pipeline as `query`, plus the appraisal synthesisers
(`appraisal.PricePerM2`, `appraisal.RentValue`,
`appraisal.HazardProfile`) printed under the per-Source summary.

```bash
$ gazetteer appraise "10 rue Dareau, 75014 Paris"
$ gazetteer appraise --json "10 rue Dareau, 75014 Paris"
```

The JSON envelope adds three top-level keys (`price`, `rent`,
`hazard`) alongside the raw `dossier`.

### `normalize`

Resolve a free-text address via the BAN
(`api-adresse.data.gouv.fr`) and print the canonical Listing.

```bash
$ gazetteer normalize "1 rue de rivoli, 75001 paris"
address  1 Rue de Rivoli 75001 Paris
city     Paris
zip      75001
insee    75101
lat,lon  48.856164,2.351548

$ gazetteer normalize --json "1 rue de rivoli, 75001 paris"
```

### `sources list`

List every Source registered with the library plus its version:

```bash
$ gazetteer sources list
ademe           v2
anct            v1
bdnb            v2  (opt-in via --source)
carteloyers     v1
cartofriches    v1
delinquance     v1
dvf             v4
education       v1
encadrement     v1
filosofi        v1
georisques      v1
locservice      v1
osm_transit     v3  (opt-in via --source)
qpv             v1
taxefonciere    v1
vacance         v1
zonageabc       v1
zonetendue      v1
```

Sources marked `(opt-in via --source)` are not part of the default
`query` / `appraise` set; pass them explicitly via `--source` (e.g.
`osm_transit` needs an offline station catalog the default factory
does not currently install, `bdnb` enforces a rolling per-key quota
the CLI does not burn by default).

### `sources doc <name>`

Print a JSON skeleton of a Source's typed Result, built via the
registered factory:

```bash
$ gazetteer sources doc dvf
{
  "value_eur_per_m2_centimes": null,
  "value_eur_centimes": null,
  ...
}
```

Useful as a quick reference for the wire shape without having to grep
the source.

### `refresh <source>|all`

Re-fetches upstream data for Sources that carry an embedded dataset
(`carteloyers`, `encadrement`, `filosofi`, `taxefonciere`, `vacance`,
`osm_transit`). The current implementation is a stub — each Source
will own a `refresh.go` over time. The CLI validates the target name
against the registry so a typo surfaces immediately.

### `version`

Prints the binary's build version (from
`runtime/debug.ReadBuildInfo`).

## Exit codes

- `0` — success
- `1` — any error (Source failure, address could not be normalised,
  unknown sub-command, …). Errors are printed on stderr.

## Environment

The CLI does not consult any environment variable today. HTTP behaviour
is whatever `httpx.New(httpx.Options{})` returns: per-host rate-limit
of 1 req/s, no HTTP cache directory, no snapshot directory.
