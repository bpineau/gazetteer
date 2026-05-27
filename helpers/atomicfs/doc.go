// Package atomicfs is a tiny set of helpers for atomic file writes
// and cheap stat-based pre-validation.
//
// "Atomic" here means rename(2)-based: we write to a sibling tmpfile
// then atomically swap it into the destination so a concurrent
// reader or a crash mid-write never observes a half-written file.
//
// The package intentionally exposes only a few functions and ships
// no types. Callers manage their own parent directories and file
// modes.
//
// Example:
//
//	if err := atomicfs.WriteFile("/var/data/snapshot.json", body, 0o644); err != nil {
//	    return err
//	}
package atomicfs
