package atomicfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteFile writes data to path via a "<path>.partial" tmpfile, fsync
// and rename(2). The destination is either the new file or untouched;
// no partial state is visible to concurrent readers, and the content is
// flushed to stable storage before the rename so a system crash cannot
// leave an empty or truncated destination behind. On failure the
// tmpfile is removed best-effort.
//
// The caller is expected to have created the parent directory.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".partial"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm) //nolint:gosec // caller-controlled path by design
	if err != nil {
		return fmt.Errorf("atomicfs: create %s: %w", tmp, err)
	}
	if _, err := out.Write(data); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("atomicfs: write %s: %w", tmp, err)
	}
	return seal(out, tmp, path)
}

// CopyFile copies src to dst with the same "<dst>.partial" tmpfile,
// fsync and rename(2) discipline as WriteFile: concurrent readers of
// dst see either the previous content or the complete new copy, never a
// partial write, even across a system crash. The copy is streamed, so
// src may be arbitrarily large.
//
// The caller is expected to have created dst's parent directory.
func CopyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src) //nolint:gosec // caller-controlled path by design
	if err != nil {
		return fmt.Errorf("atomicfs: open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	tmp := dst + ".partial"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm) //nolint:gosec // caller-controlled path by design
	if err != nil {
		return fmt.Errorf("atomicfs: create %s: %w", tmp, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("atomicfs: copy to %s: %w", tmp, err)
	}
	return seal(out, tmp, dst)
}

// seal finishes an atomic write: flush the tmpfile to stable storage,
// close it, swap it into place, then fsync the parent directory
// (best-effort) so the rename itself survives a crash. On any failure
// the tmpfile is removed best-effort.
func seal(out *os.File, tmp, path string) error {
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("atomicfs: sync %s: %w", tmp, err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("atomicfs: close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("atomicfs: rename %s -> %s: %w", tmp, path, err)
	}
	// Durability of the rename requires the directory entry itself to
	// reach disk. Failure here is not worth surfacing: the data is
	// already safely renamed for every live reader.
	if dir, err := os.Open(filepath.Dir(path)); err == nil { //nolint:gosec // derived from caller's path
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// Exists reports whether path is a regular file. Directories, broken
// symlinks, and missing paths all return false.
func Exists(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return st.Mode().IsRegular()
}

// NonEmpty reports whether path is a regular file with size strictly
// greater than min bytes. Useful for cheap pre-validation of artefacts
// (LLM outputs, scraped dumps, OSM catalog snapshots).
func NonEmpty(path string, min int64) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return st.Mode().IsRegular() && st.Size() > min
}
