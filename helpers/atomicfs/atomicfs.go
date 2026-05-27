package atomicfs

import (
	"fmt"
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
