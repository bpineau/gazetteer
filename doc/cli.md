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
gazetteer query      [flags] <addr>
gazetteer appraise   [flags] <addr>
gazetteer compare    [flags] <addr1> <addr2> [...]
gazetteer normalize  [--json] [--verbose] <addr>
gazetteer sources    list
gazetteer sources    doc       <name>
gazetteer refresh    [<source>|all] [--list] [--data-dir DIR] [--force] [--go-embed-update]
gazetteer version
```

Run `gazetteer <cmd> -h` for the per-sub-command flag list.

### `query`

Run every configured Source against an address; print either a
per-source human summary or the full Dossier as JSON.

```bash
$ gazetteer query "1 rue de Rivoli, 75001 Paris"

$ gazetteer query --surface 46 --rooms 2 "10 rue Dareau, 75014 Paris"

$ gazetteer query --source dvf,osm_transit "10 rue Dareau, 75014 Paris"

$ gazetteer query --json "1 rue de Rivoli, 75001 Paris" | jq .
```

- `--property-type apartment|house|land|commercial` — drives Source
  eligibility (DVF, encadrement, MeilleursAgents). Default: `apartment`.
- `--surface <m²>` — habitable surface. Required by DVF, taxe-foncière
  and encadrement to produce a useful answer; ADEME also uses it to
  pick the right dwelling when an address carries several DPE rows.
- `--rooms <N>` — room count. Required by carteloyers, encadrement and
  locservice to pick the typology bucket.
- `--source` — comma-separated Source names. Default: every Source
  the CLI knows how to instantiate (every `Default: true` entry in the
  registry — `bdnb` and `osm_transit` are opt-in, see below).
- `--json` — emit the full Dossier as indented JSON instead of the
  human summary.
- `--profile` — (appraise / compare only) the ZoneScore weight preset:
  `yield` (default, yield-first) | `balanced` | `patrimoine`
  (capital-appreciation / low-hassle) | `transport` (heavily up-weights
  walk-to-station — for a "near a station, not Paris" thesis).
- `--verbose` — DEBUG-level slog output to stderr.
- `--dump` — log raw HTTP request/response payloads for Sources that
  honour the flag.

Every Source prints a compact, human-readable one-line summary of what
it found (e.g. `dvf  10132 €/m², 1645 sales, tier=address_radius`;
`oll  loyer observé 18.0 €/m²/mois (IQR 15.7–20.5, 898 obs, Zone 5)`).
Empty results say why where it helps (e.g. `oll  no observed-rent cell
(Paris intra-muros is out of OLL scope)`).

The flag set is identical for `appraise`. Flags may appear before or
after the positional address; quoted multi-word addresses are not
required.

### `appraise`

Same pipeline as `query`, plus the appraisal synthesisers
(`appraisal.PricePerM2`, `appraisal.RentValue`,
`appraisal.HazardProfile`) and the yield-first **zone score**
(`appraisal/zonescore`, a 0–100 composite with a per-axis breakdown:
rendement, tension, solvabilité, sécurité, fiscalité, accès) printed
under the per-Source summary.

```bash
$ gazetteer appraise "10 rue Dareau, 75014 Paris"
$ gazetteer appraise --json "10 rue Dareau, 75014 Paris"
```

The JSON envelope adds four top-level keys (`price`, `rent`, `hazard`,
`zone_score`) alongside the raw `dossier`.

### `compare`

Rank several candidate addresses **best-first** by the same yield-first
zone score, so you can settle a "j'hésite entre A et B" decision in one
call. Quote each address separately; flags are shared with `query`.

```bash
$ gazetteer compare --surface 46 --rooms 2 \
    "place Jean Jaurès 93100 Montreuil" \
    "10 rue Dareau 75014 Paris"
```

Each address is normalised, its sources collected, and the candidates
are printed as a ranked table plus the winner's axis breakdown.
`--profile` swaps the weighting thesis (e.g. `--profile transport` ranks
by walk-to-station as much as yield). A candidate whose yield is
**known** (the rendement axis is present)
always outranks one whose yield is unknown (e.g. DVF found no
comparables) — a yield-first ranking must not be won by a zone with no
yield data. `--json` emits the full `Comparison`.

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
bpe             v1
cadastre        v1
carteloyers     v1
cartofriches    v1
catnat          v1
cdsr            v1
chomage         v1
delinquance     v3
dpedist         v1
dvf             v4
education       v1
encadrement     v2
filoiris        v1
filosofi        v1
georisques      v1
ips_ecoles      v1
iris            v1
locservice      v1
logiris         v1
lovac           v1
nuisances       v1
oll             v1
osm_transit     v3
qpv             v1
rpls            v1
taxefonciere    v1
vacance         v1
zonageabc       v1
zonetendue      v1
```

Sources marked `(opt-in via --source)` are not part of the default
`query` / `appraise` set; pass them explicitly via `--source`. Today
only `bdnb` is opt-in (it enforces a rolling per-key quota the CLI does
not burn by default); `osm_transit` now ships an embedded station
catalog with a live Overpass fallback, so it runs by default.

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

### `refresh [<source>|all]`

Download each Source's real upstream file(s) and rebuild its dataset
into the **data directory** (default `$GAZETTEER_DATA_DIR` or
`~/.cache/gazetteer`, override with `--data-dir`). A refreshed copy
present there takes precedence over the binary's embedded dataset; with
no datadir the embedded copy is used, so the lib works out of the box.

```bash
$ gazetteer refresh --list          # per-artifact: active origin, size, refreshable
$ gazetteer refresh                 # download + (re)build every dataset
$ gazetteer refresh delinquance     # just one source
```

`refresh` is **idempotent**: a dataset already present and current in
the datadir is skipped untouched (no download, no rebuild), so it is
safe to call on every boot as a warm-up. `--force` rebuilds regardless.
Maintainers re-embed a refreshed dataset with `--go-embed-update`
(rebuilds into the datadir, then copies the artifact back into
`sources/<name>/data/` for re-commit). See
[datasets.md](datasets.md) for the full model.

### `version`

Prints the binary's build version (from
`runtime/debug.ReadBuildInfo`).

## Exit codes

- `0` — success
- `1` — any error (Source failure, address could not be normalised,
  unknown sub-command, …). Errors are printed on stderr.

## Environment

- `GAZETTEER_DATA_DIR` — overrides the data directory the offline Sources
  read refreshed datasets from (default `~/.cache/gazetteer`). The
  `--data-dir` flag on `refresh` takes precedence over it. When unset and
  absent, Sources fall back to their embedded datasets.

HTTP behaviour is `httpx.New` with a polite per-host rate limit of
2 req/s by default, raised to 10 req/s for the data.gouv.fr DVF and
cadastre endpoints (via `dvf.HostRateLimits()`) because DVF fans out one
call per cadastral section. No HTTP cache directory, no snapshot
directory.
