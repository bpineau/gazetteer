package atomicfs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bpineau/gazetteer/pkg/atomicfs"
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
	// No .partial sibling left behind.
	if _, err := os.Stat(dst + ".partial"); !os.IsNotExist(err) {
		t.Errorf(".partial sibling still present after successful write")
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
