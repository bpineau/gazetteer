# atomicfs — rename(2)-based atomic file writes

A tiny set of helpers for atomic file writes and cheap stat-based
pre-validation. "Atomic" means: write to a sibling tmpfile, fsync, then
rename it into place, so a concurrent reader or a crash mid-write never
observes a half-written file.

Used by every dataset refresh path in the project (a partially written
dataset artifact must never shadow the embedded one) and reusable by any
program that persists files other processes read.

## Quick start

```go
import "github.com/bpineau/gazetteer/helpers/atomicfs"

// Readers of snapshot.json see either the old or the new content,
// never a torn write.
if err := atomicfs.WriteFile("/var/data/snapshot.json", body, 0o644); err != nil {
    return err
}

// Same guarantee for a copy.
if err := atomicfs.CopyFile(src, dst, 0o644); err != nil {
    return err
}

// Cheap pre-validation before trusting a file on disk.
if !atomicfs.NonEmpty(path, 1024) { // exists and holds at least 1 KiB
    // treat as absent, fall back to the embedded dataset
}
```

## Public API

See `go doc github.com/bpineau/gazetteer/helpers/atomicfs`:

- `func WriteFile(path string, data []byte, perm os.FileMode) error`
- `func CopyFile(src, dst string, perm os.FileMode) error`
- `func Exists(path string) bool`
- `func NonEmpty(path string, min int64) bool`

## Design notes

- The tmpfile is created next to the destination (same directory, same
  filesystem) so the final rename is guaranteed atomic; a tmpfile in
  `/tmp` would silently degrade to copy semantics across filesystems.
- The package intentionally exposes only functions and ships no types.
  Callers manage their own parent directories and file modes.
- `Exists` / `NonEmpty` are advisory stats, not locks: they answer "is
  there something plausible here right now", which is all a
  read-mostly dataset cache needs.

## Status

Stable. Symbols may be added but not renamed or removed without a
deprecation cycle.
