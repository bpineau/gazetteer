# Dataset caching, embedding & refresh — design

Date: 2026-05-30
Status: approved

## Problem

Block-dataset Sources (the ~16 that ship a pre-indexed CSV/JSON artifact under
`sources/<name>/data/`) currently embed that artifact via `go:embed` and load it
through a package-global `sync.Once` singleton. There is no way to:

- refresh a dataset without rebuilding the library,
- ship a dataset too large to embed (download on demand instead),
- keep the raw upstream file alongside the processed one for troubleshooting /
  reprocessing,
- regenerate the committed embedded artifacts from upstream in a repeatable way.

Live-HTTP Sources (dvf, cadastre, ademe, bdnb, georisques, locservice,
education) query an API per address and are **out of scope**. `osm` already has a
per-query on-disk cache and is only lightly aligned (reuse `DefaultDir`).

## Goals

1. **Read path** — at runtime a Source loads its processed artifact preferring a
   *datadir* copy over the embedded fallback, and degrades to an **empty result**
   (`StatusOKEmpty`) when a non-embedded dataset was never downloaded. Never a
   hard failure for a legitimately-absent dataset.
2. **Datadir** — a single **flat** directory holding both raw and processed
   files plus per-source manifests. Default `os.UserCacheDir()/gazetteer`
   (`~/.cache/gazetteer`), overridable by explicit argument and the
   `GAZETTEER_DATA_DIR` environment variable.
3. **Write path** — an explicit API + CLI to download the raw upstream input(s),
   run a per-source transform, validate, and persist both raw and processed.
4. **External plugins** — a 3rd-party Source outside this repo can fully
   participate: declare a `dataset.Set`, get the datadir, refresh to the datadir.
5. **Quality** — idiomatic, robust, easy to use correctly and hard to misuse;
   token-frugal and AI-legible API.

## Non-goals

- Disk-caching live-HTTP Source responses (separate concern).
- Rewriting `osm`'s per-query catalog cache.
- A general plugin-distribution mechanism.

## Architecture

A new public package `github.com/bpineau/gazetteer/dataset` owns everything.
It depends only on the stdlib plus `helpers/httpx` and `helpers/atomicfs`. It
**never imports the `gazetteer` core** (one-way dependency: core → dataset).

### Conventions

- Embedded artifacts live under `data/` inside each source's `embed.FS`.
- Datadir is flat. Filenames must be clean single path elements
  (`filepath.Base(name) == name`, non-empty, no `..`, no separator).
- Per-source manifest sidecar: `<source>.manifest.json`.
- Processed filenames keep their current descriptive, source-prefixed names
  (already collision-free). Raw filenames follow `<source>.raw[.<n>].<ext>`
  derived from the upstream URL's extension.

### The `dataset.Set` descriptor (what a Source declares)

```go
// Set is the binding, declared by one Source, between its embedded fallback
// data and the raw→processed pipeline that can refresh it. A Source ships one
// Set per logical dataset (usually exactly one).
type Set struct {
    Source    string    // owning source name; namespaces datadir files + manifest, locates embed dir
    Embed     fs.FS     // embedded fallback (rooted so that "data/<Processed.Name>" resolves); nil if too large to embed
    Processed File       // the indexed artifact the Source loads at runtime
    Raw       []File     // upstream inputs, kept alongside; never embedded
    Transform Transform  // raw → processed bytes; nil => read-only Set (refresh skips it)
}

// File names a single artifact.
//   Name   — clean datadir basename (and, for Processed, the basename under Embed/data).
//   URL    — upstream location (raw inputs only; empty for Processed).
//   SHA256 — optional pinned hash of the raw input, verified on download.
type File struct {
    Name   string
    URL    string
    SHA256 string
}

// RawSet gives a Transform access to the downloaded raw inputs by name.
// dataset owns the readers' lifecycle; a Transform must not close them.
type RawSet interface {
    Open(name string) (io.ReadCloser, error)
}

// Transform turns the raw inputs into the processed artifact, streaming to dst.
type Transform func(ctx context.Context, raw RawSet, dst io.Writer) error
```

### Read path

```go
// Open returns the processed artifact, preferring a validated datadir copy over
// the embedded fallback. version is the owning Source's Version().
//
// Selection (deterministic, no parsing, no hashing at runtime):
//   1. datadir file <dir>/<Processed.Name> exists:
//        - manifest entry present  → use it iff entry.SourceVersion == version,
//                                     else fall through to embed.
//        - manifest entry absent   → hand-placed; trust and use it.
//   2. else Embed has data/<Processed.Name> → use embed.
//   3. else → ErrUnavailable.
func (s Set) Open(dir string, version int) (io.ReadCloser, error)

var ErrUnavailable = errors.New("dataset: not present in datadir and not embedded")
```

Rationale for the version gate: the real hazard is *schema drift* — a datadir
file produced by a different library `Version()` whose parser no longer matches.
Gating by `SourceVersion` makes that case ignore the datadir file deterministically
(→ embed), rather than parse-then-silently-fall-back (which masks corruption) or
parse-and-crash. A version-matched file that is genuinely corrupt fails **loudly**
at the source's parse step (honest; fixed by `refresh --force`). sha256 is *not*
checked at runtime (avoids hashing multi-MB files on every process start); it is
verified during `refresh` and surfaced by `refresh --list`.

Each Source keeps its existing package-global `sync.Once`+`*Index` loader,
swapping `embedFS.ReadFile("data/x")` for `set.Open(dir, Version)` and mapping
`ErrUnavailable` to an **empty** `*Index` (not an error). The `Options.Index`
test-injection seam is preserved. Consequence: `Query` needs **zero** knowledge
of the `dataset` package — a missing dataset naturally yields an empty `Result`
via the existing `IsEmpty()` path; only genuine IO/parse errors become
`ErrUpstreamPermanent`.

### Write path (refresh engine)

```go
type RefreshOptions struct {
    Dir   string        // datadir; "" => DefaultDir()
    Force bool          // re-download raw even if already present
    Log   func(Event)   // structured progress; nil => discard
}

// Refresh downloads + transforms the given sets into the datadir. It never
// aborts the batch on a single failure; the returned error is errors.Join of
// per-set failures and Report carries per-set outcomes.
func Refresh(ctx context.Context, c *httpx.Client, sets []Set, opts RefreshOptions) (Report, error)

type Report []SetResult
type SetResult struct {
    Source    string
    Processed string
    Raw       []DownloadResult // one per raw input
    SHA256    string           // of the processed artifact
    Bytes     int64
    Skipped   bool             // nothing to do (read-only set, or all present and !Force)
    Err       error
}

type Event struct {
    Source string
    Phase  string // "download" | "transform" | "validate" | "write" | "skip"
    File   string
    Bytes  int64
    SHA    string
    Err    error
}

const DefaultDirEnv = "GAZETTEER_DATA_DIR"
func DefaultDir() (string, error) // os.UserCacheDir()/gazetteer, overridable by env
func ResolveDir(explicit string) (string, error) // explicit > env > DefaultDir
```

Per `Set`, in order (the manifest is the commit point, written last):

1. For each raw `File`: `c.Download(ctx, url, <dir>/<raw.Name>, DownloadOptions{
   SkipIfExists: !Force, ExpectedSHA256: raw.SHA256, MaxBytes: …})`. Raw is
   always kept on disk.
2. `Transform(ctx, rawSet, tmp)` streaming to a sibling temp file; reuse the
   downloaded raws via a `RawSet` backed by the datadir files.
3. **Validate**: re-open the produced processed bytes and confirm they parse —
   the Source supplies a validator (`func(io.Reader) error`) via the Set, or the
   engine performs a structural check (gzip/JSON well-formedness) when none is
   given. A failed transform never replaces the previous artifact.
4. Atomically write the processed file into the datadir.
5. Rewrite `<source>.manifest.json` (sha256, bytes, sourceVersion, fetchedAt,
   urls) last.

`Set.Transform == nil` ⇒ read-only set ⇒ refresh records `Skipped` with a clear
reason. This is the honest state for a source whose upstream transform has not
yet been reconstructed; it can be filled in incrementally as a self-contained
per-source contribution.

### Discovery & wiring

```go
// in package gazetteer (core may import dataset; dataset never imports core):
type DatasetProvider interface { Datasets() []dataset.Set }
```

- Block Sources implement `DatasetProvider`.
- `factory.Options` gains `DataDir string`; `BuilderDefault` resolves it once
  (`dataset.ResolveDir`) and injects it into every block Source's constructor.
- External plugins resolve the dir themselves via `dataset.ResolveDir` /
  `DefaultDir` and pass `[]dataset.Set` straight to `dataset.Refresh`.

### CLI

```
gazetteer refresh [sources...|all] [flags]
  --data-dir DIR      target datadir (default: $GAZETTEER_DATA_DIR or ~/.cache/gazetteer)
  --force             re-download raw even if already present
  --go-embed-update   write the processed artifact into sources/<name>/data/ for re-commit
  --list              report per-source state (present/absent, bytes, sha, version) and exit
```

The CLI is a thin mirror of `dataset.Refresh`. `--go-embed-update` is a
**CLI-only** step (the library knows only the datadir): refresh into the datadir,
then copy each processed artifact into `sources/<source>/data/<name>` under the
module root (resolved in `cmd/`, which lives in the repo). The `dataset` package
carries no `go env GOMOD`, no `Target` enum, no `RepoEmbed`.

## Testing

- `dataset` unit tests: `Open` selection matrix (datadir gated/hand-placed/absent,
  embed, ErrUnavailable); manifest read/write/atomicity; name validation;
  `Refresh` against `httptest.NewServer` serving fixture raws; partial-failure
  `Report` + `errors.Join`; `DefaultDir`/`ResolveDir` precedence.
- **Per-source golden test (required on migration)**: a tiny fixture raw →
  `Transform` → assert the bytes parse-equal the committed embedded artifact.
  Pure, offline, deterministic.
- Existing per-source `Load`/`Query` tests keep passing (read-path swap only;
  `Options.Index` seam intact).

## Migration / rollout

1. `dataset` package + tests.
2. `gazetteer.DatasetProvider` interface.
3. Read-path swap for all 16 block sources (`Load` → `set.Open`, empty on
   `ErrUnavailable`); `factory.Options.DataDir` injection.
4. CLI `refresh` rewrite + `--go-embed-update` copy step.
5. Per-source `Datasets()` declarations with raw URL(s) + `Transform` + golden
   test, added incrementally; sources without a reconstructed transform ship a
   read-only Set (Transform nil) until done.

## Affected sources

Block sources migrated to the read path: carteloyers, encadrement, filosofi,
taxefonciere, cartofriches, delinquance, chomage, vacance_logements, vacance,
bpe, anct, rpls, zonageabc, ips_ecoles, zonetendue, qpv.

carteloyers stays embedded (9.4 MB cumulative; the ~3 MiB guideline governs
future large artifacts, not a forced de-embed). Live-HTTP sources untouched.
```
