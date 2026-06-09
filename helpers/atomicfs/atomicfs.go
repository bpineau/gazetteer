package atomicfs

import (
	"fmt"
	"io"
	"os"
)

// WriteFile writes data to path via a "<path>.partial" tmpfile +
// rename(2). The destination is either the new file or untouched ; no
// partial state is visible to concurrent readers. On rename failure
// the tmpfile is removed best-effort.
//
// The caller is expected to have created the parent directory.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".partial"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("atomicfs: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("atomicfs: rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// CopyFile copies src to dst with the same "<dst>.partial" tmpfile +
// rename(2) discipline as WriteFile: concurrent readers of dst see either
// the previous content or the complete new copy, never a partial write.
// The copy is streamed, so src may be arbitrarily large.
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
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("atomicfs: close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("atomicfs: rename %s -> %s: %w", tmp, dst, err)
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
