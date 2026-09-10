package httpx

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ErrCacheDisabled is returned by Client.PruneCache when the Client runs
// without an HTTP cache (Options.HTTPCacheDir empty): there is nothing to
// prune, and silently reporting "removed 0" would hide the misconfiguration.
var ErrCacheDisabled = errors.New("httpx: no HTTP cache directory configured")

// PruneOptions selects what one Client.PruneCache pass removes. Both limits
// are independent and both are opt-in; a zero-valued PruneOptions removes
// only the entries that are unusable anyway (see PruneCache).
type PruneOptions struct {
	// MaxAge drops every entry fetched (or last revalidated) longer ago
	// than this. It is wall-clock age, NOT the entry's own TTL: a long-lived
	// cache accumulates entries no caller will ever ask for again, and their
	// expires_at says nothing about that. 0 disables the age pass.
	MaxAge time.Duration

	// MaxBytes caps the cache's total on-disk footprint (meta + body files)
	// after the age pass. When the survivors exceed it, the
	// oldest-fetched-first are removed until the total fits. 0 disables the
	// size pass.
	//
	// Fetch time is the only ordering the on-disk format records: there is
	// no last-read timestamp, so this evicts the oldest entries, not the
	// coldest ones.
	MaxBytes int64
}

// PruneStats reports what one Client.PruneCache pass did.
type PruneStats struct {
	// ScannedEntries is the number of cache entries the pass examined,
	// including the broken ones it removed.
	ScannedEntries int

	// RemovedEntries is how many entries the pass deleted.
	RemovedEntries int

	// RemovedBytes is the on-disk footprint (meta + body) those entries
	// occupied.
	RemovedBytes int64

	// RemainingEntries is how many usable entries the cache still holds.
	RemainingEntries int

	// RemainingBytes is their total on-disk footprint (meta + body).
	RemainingBytes int64
}

// PruneCache removes cache entries the caller no longer wants on disk: every
// entry older than opts.MaxAge, then the oldest entries still standing until
// the cache fits opts.MaxBytes. Entries that are unusable in any case are
// always removed: a meta file without its body (or the reverse), and a meta
// file that no longer parses, all of which the read path already treats as a
// miss.
//
// NOTHING CALLS THIS AUTOMATICALLY, by design. The disk cache is shared
// state with a lifetime the library cannot judge: a long-lived server wants
// a nightly pass, a batch job wants none (its whole point is to keep the
// corpus it downloaded), and an operator may be holding entries on purpose
// for offline replay. So the policy, and the moment, belong to the caller:
// wire this into a cron, a maintenance endpoint or a startup hook, and pick
// the two limits yourself.
//
// It is safe to run while requests are in flight: a request whose entry is
// removed mid-flight simply misses and refetches (the entry files are
// replaced atomically, never mutated in place). Concurrent PruneCache calls
// on the same directory are safe too; they may double-count a removal in
// their stats.
//
// Returns ErrCacheDisabled when the Client has no cache directory, and a
// zero-valued PruneStats with a nil error when the directory does not exist
// yet (nothing has been cached). The now-empty shard directories are left in
// place: the next write reuses them, and removing them would race a
// concurrent writer that has just created one.
func (c *Client) PruneCache(opts PruneOptions) (PruneStats, error) {
	if c.resolved.cacheDir == "" {
		return PruneStats{}, ErrCacheDisabled
	}
	return pruneCacheDir(c.resolved.cacheDir, c.resolved.now(), opts)
}

// pruneEntry is one cache entry as seen from the disk: the two files, their
// combined footprint, and when the entry was fetched. broken marks an entry
// the read path could never serve.
type pruneEntry struct {
	metaPath  string
	bodyPath  string
	bytes     int64
	fetchedAt time.Time
	broken    bool
}

// pruneCacheDir implements PruneCache over one directory, with now injected
// so the age pass is testable.
func pruneCacheDir(dir string, now time.Time, opts PruneOptions) (PruneStats, error) {
	entries, err := scanCacheDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return PruneStats{}, nil
		}
		return PruneStats{}, err
	}

	var stats PruneStats
	stats.ScannedEntries = len(entries)

	remove := func(e *pruneEntry) {
		_ = os.Remove(e.metaPath)
		_ = os.Remove(e.bodyPath)
		stats.RemovedEntries++
		stats.RemovedBytes += e.bytes
	}

	survivors := make([]*pruneEntry, 0, len(entries))
	for _, e := range entries {
		switch {
		case e.broken:
			remove(e)
		case opts.MaxAge > 0 && now.Sub(e.fetchedAt) > opts.MaxAge:
			remove(e)
		default:
			survivors = append(survivors, e)
		}
	}

	var total int64
	for _, e := range survivors {
		total += e.bytes
	}
	if opts.MaxBytes > 0 && total > opts.MaxBytes {
		// Oldest fetch first: the on-disk format records no read time.
		slices.SortFunc(survivors, func(a, b *pruneEntry) int {
			return a.fetchedAt.Compare(b.fetchedAt)
		})
		first := len(survivors)
		for i, e := range survivors {
			if total <= opts.MaxBytes {
				first = i
				break
			}
			remove(e)
			total -= e.bytes
		}
		survivors = survivors[first:]
	}

	stats.RemainingEntries = len(survivors)
	stats.RemainingBytes = total
	return stats, nil
}

// scanCacheDir reads the cache layout (<dir>/<2 chars>/<hash>.{json,body})
// into one pruneEntry per hash. An entry missing either file, or whose meta
// does not parse, comes back marked broken.
func scanCacheDir(dir string) ([]*pruneEntry, error) {
	byHash := make(map[string]*pruneEntry)
	entryFor := func(path string) *pruneEntry {
		key := strings.TrimSuffix(path, filepath.Ext(path))
		e, ok := byHash[key]
		if !ok {
			e = &pruneEntry{}
			byHash[key] = e
		}
		return e
	}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".json" && ext != ".body" {
			return nil // tmpfiles from an in-flight write, or foreign files
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil // vanished under us (a concurrent prune): not our problem
			}
			return err
		}
		e := entryFor(path)
		e.bytes += info.Size()
		if ext == ".json" {
			e.metaPath = path
		} else {
			e.bodyPath = path
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make([]*pruneEntry, 0, len(byHash))
	for key, e := range byHash {
		half := e.metaPath == "" || e.bodyPath == ""
		if e.metaPath == "" {
			e.metaPath = key + ".json"
		}
		if e.bodyPath == "" {
			e.bodyPath = key + ".body"
		}
		switch meta, ok := readCacheMeta(e.metaPath); {
		case half, !ok:
			// Half an entry, or a meta file that no longer parses: the read
			// path treats both as a miss, so they are pure dead weight.
			e.broken = true
		default:
			e.fetchedAt = time.Unix(meta.FetchedAtSec, 0)
		}
		out = append(out, e)
	}
	return out, nil
}

// readCacheMeta parses one on-disk meta file, reporting ok=false when it is
// unreadable or malformed (exactly what the read path treats as a miss).
func readCacheMeta(path string) (*cacheMeta, bool) {
	b, err := os.ReadFile(path) //nolint:gosec // path comes from the cache layout walk, not from user input
	if err != nil {
		return nil, false
	}
	var meta cacheMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return nil, false
	}
	return &meta, true
}
