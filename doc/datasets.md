# Datasets: datadir, embedding & refresh

Block-dataset sources (the offline ones — `delinquance`, `carteloyers`,
`taxefonciere`, …) ship a pre-indexed CSV/JSON artifact. The `dataset`
package lets that artifact be **overridden from a data directory** and
**refreshed from upstream**, without rebuilding the library.

## The data directory (datadir)

A single flat directory holding, side by side:

- the **processed** artifact each source loads (also embedded when small);
- the **raw** upstream input(s) it was built from (kept for troubleshooting
  and reprocessing; never embedded);
- a per-source manifest `<source>.manifest.json` (sha256, size, version, URLs).

Resolution order (highest precedence first):

1. an explicit path (`--data-dir`, or `factory.Options.DataDir`);
2. the `GAZETTEER_DATA_DIR` environment variable;
3. the default `os.UserCacheDir()/gazetteer` (e.g. `~/.cache/gazetteer`).

`factory.Options.DataDir = "-"` disables the datadir (embedded-only).

## Read resolution (runtime)

For each artifact, a source loads, in order:

1. the **datadir** copy `<datadir>/<name>`, **iff** its manifest entry records
   the same library `Version()` (a deterministic schema-drift guard) — or iff
   it has no manifest entry at all (a deliberately hand-placed file);
2. otherwise the **embedded** copy;
3. otherwise an **empty** result (a non-embedded dataset that was never
   downloaded degrades gracefully to `StatusOKEmpty`, never a hard failure).

A datadir file produced by a *different* library version is ignored in favour
of the embedded copy. sha256 integrity is checked by `refresh`, not on the
hot read path.

## Refreshing

```
gazetteer refresh [sources...|all] [flags]
  --data-dir DIR      target datadir (default $GAZETTEER_DATA_DIR or ~/.cache/gazetteer)
  --force             re-download raw even if already present
  --go-embed-update   also copy rebuilt artifacts into sources/<name>/data/ for re-commit
  --list              report per-source state (origin, size, refreshable) and exit
```

- `gazetteer refresh --list` shows where each artifact resolves from
  (`datadir` / `embed` / `none`) and whether it is refreshable.
- `gazetteer refresh delinquance` downloads + rebuilds one source into the
  datadir; `gazetteer refresh` (or `all`) does every source.
- `gazetteer refresh --go-embed-update` rebuilds into the datadir **and**
  copies each artifact back into `sources/<name>/data/` so you can re-commit
  the embedded data. It must run inside the module checkout.

From code, the CLI is a thin mirror of:

```go
report, err := dataset.Refresh(ctx, httpClient, sets, dataset.RefreshOptions{
    Dir: dir, Force: force,
})
```

## Adding refresh support to a source (extension point)

A source becomes refreshable by declaring a `dataset.Transform` on its
`dataset.Set`. Without one the Set is **read-only**: it still benefits from
the datadir override and is shown as `refreshable: no` by `--list`.

```go
var set = dataset.Set{
    Source:    Name,
    Version:   Version,
    Embed:     embedFS,                                   // data/<Processed.Name>
    Processed: dataset.File{Name: "foo_communes.json.gz"},
    Raw: []dataset.File{{
        Name: "foo.raw.csv",
        URL:  "https://www.data.gouv.fr/.../foo.csv",
        // SHA256: "…",                                   // optional: pin the raw
    }},
    Transform: func(ctx context.Context, raw dataset.RawSet, dst io.Writer) error {
        r, err := raw.Open("foo.raw.csv")                 // by File.Name
        if err != nil {
            return err
        }
        defer r.Close()
        // parse r, build the indexed shape, stream it to dst (gzip + json…).
        return nil
    },
}
```

`Refresh` downloads each `Raw` (reusing the project HTTP client: retries,
rate-limiting, atomic streaming, sha256), runs `Transform` into a temp file,
**validates** it (the Set's `Validate`, else a generic well-formedness check),
then atomically installs it and writes the manifest last as the commit point.

Each transform should be covered by an offline golden test: a fixture raw →
`Transform` → assert the output parses equal to the committed artifact.

A source exposes its Sets to the tooling via `gazetteer.DatasetProvider`:

```go
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }
```

External (out-of-tree) plugins participate identically: build a `dataset.Set`,
resolve the dir with `dataset.ResolveDir`, and pass the sets to
`dataset.Refresh`. (`--go-embed-update` is in-repo only and does not apply to
out-of-tree plugins.)

## Status

The framework (datadir override, refresh engine, CLI, manifests) is complete,
every block source is on the datadir-aware read path, and **every in-tree
block source is refreshable** — each declares its real upstream `Raw` URL(s)
and a `Transform`, with an offline golden test. The transforms were validated
end-to-end against the live upstream: each reproduces (or knowingly
supersedes, when the upstream has a newer vintage) its committed artifact.

`gazetteer refresh --list` shows `refreshable: yes` for all 22 artifacts
across the 16 block sources. The upstream resource URLs and dataset vintages
are pinned in each source's `transform.go`; bump them (and re-commit the
embedded data via `--go-embed-update`) when a new edition ships.

Reading an xlsx upstream (`chomage`) pulls in `github.com/xuri/excelize/v2`;
every other transform uses only the standard library plus the project HTTP
helper.
