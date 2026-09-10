package atomicfs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bpineau/gazetteer/helpers/atomicfs"
)

func TestWriteFile_CreatesDestinationAtomically(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.txt")
	if err := atomicfs.WriteFile(dst, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("contents = %q, want %q", got, "hello")
	}
	// No .partial sibling left behind (the name carries a random component).
	if leftovers := partials(t, dst); len(leftovers) > 0 {
		t.Errorf(".partial siblings still present after successful write: %v", leftovers)
	}
}

func TestWriteFile_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := atomicfs.WriteFile(dst, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "new" {
		t.Errorf("contents = %q, want %q", got, "new")
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f.txt")
	if atomicfs.Exists(f) {
		t.Error("Exists(missing) = true, want false")
	}
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !atomicfs.Exists(f) {
		t.Error("Exists(file) = false, want true")
	}
	// A directory is NOT a regular file.
	if atomicfs.Exists(dir) {
		t.Error("Exists(dir) = true, want false (Exists only matches regular files)")
	}
}

func TestNonEmpty(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f.txt")
	if atomicfs.NonEmpty(f, 0) {
		t.Error("NonEmpty(missing) = true, want false")
	}
	if err := os.WriteFile(f, []byte("xy"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// size=2, min=2 → strictly-greater-than fails.
	if atomicfs.NonEmpty(f, 2) {
		t.Error("NonEmpty(2 bytes, min=2) = true, want false (strict >)")
	}
	if !atomicfs.NonEmpty(f, 1) {
		t.Error("NonEmpty(2 bytes, min=1) = false, want true")
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := atomicfs.CopyFile(src, dst, 0o644); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("dst content = %q, want %q", got, "payload")
	}
	if atomicfs.Exists(dst + ".partial") {
		t.Error("tmpfile left behind after successful copy")
	}
	// Missing source: dst untouched, no tmpfile.
	if err := atomicfs.CopyFile(filepath.Join(dir, "missing"), dst, 0o644); err == nil {
		t.Error("CopyFile(missing src) = nil error, want error")
	}
	if got, _ := os.ReadFile(dst); string(got) != "payload" {
		t.Errorf("dst clobbered by failed copy: %q", got)
	}
}

// partials lists the tmpfile siblings atomicfs may have left next to path.
// The name is "<path>.<random>.partial", so a plain os.Stat cannot see them.
func partials(t *testing.T, path string) []string {
	t.Helper()
	m, err := filepath.Glob(path + ".*.partial")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return m
}
