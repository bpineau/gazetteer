// Package dataset manages the raw and processed data files that back
// block-dataset Sources — the ones that ship a pre-indexed CSV/JSON
// artifact rather than querying a live API per address.
//
// It does two things:
//
//   - Read path: at runtime a Source loads its processed artifact from a
//     flat data directory ("datadir") when present, falling back to the
//     copy embedded in the binary, and degrading to ErrUnavailable when a
//     non-embedded dataset was never downloaded. See Set.Open.
//
//   - Write path: Refresh downloads each Set's raw upstream input(s), runs
//     the Set's Transform to (re)build the processed artifact, validates it,
//     and persists both raw and processed into the datadir alongside a
//     per-source manifest. See Refresh.
//
// A Source declares exactly one [Set] per logical dataset. The package is
// self-sufficient for out-of-tree plugins: they build a Set, resolve the
// datadir with [ResolveDir], and pass their sets straight to [Refresh].
//
// Layering: dataset depends only on the standard library plus the project's
// httpx and atomicfs helpers. It never imports the gazetteer core, so the
// dependency edge is one-way (core → dataset).
//
// Datadir layout is flat. For a source "foo" shipping processed file
// "foo_communes.json.gz" with one raw input:
//
//	<datadir>/foo_communes.json.gz   processed artifact (also embedded when small)
//	<datadir>/foo.raw.csv            raw upstream input (never embedded)
//	<datadir>/foo.manifest.json      per-source manifest (sha256, version, urls…)
//
// Embedded artifacts live under "data/" inside each Set.Embed filesystem.
package dataset
