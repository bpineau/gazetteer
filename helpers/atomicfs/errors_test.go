package atomicfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFile_MissingParentDir(t *testing.T) {
	err := WriteFile(filepath.Join(t.TempDir(), "absent", "f.txt"), []byte("x"), 0o644)
	if err == nil || !strings.Contains(err.Error(), "atomicfs") {
		t.Errorf("missing parent dir must fail with an atomicfs-prefixed error, got %v", err)
	}
}

func TestWriteFile_RenameFailureCleansTmp(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "target")
	// A directory at the destination makes the final rename fail.
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(dst, []byte("x"), 0o644); err == nil {
		t.Fatal("rename over a directory must fail")
	}
	if Exists(dst + ".partial") {
		t.Error("failed write must remove its tmpfile")
	}
}

func TestCopyFile_MissingSource(t *testing.T) {
	dir := t.TempDir()
	err := CopyFile(filepath.Join(dir, "absent"), filepath.Join(dir, "dst"), 0o644)
	if err == nil {
		t.Error("missing source must fail")
	}
}

func TestCopyFile_RenameFailureCleansTmp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dstdir")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dst, 0o644); err == nil {
		t.Fatal("rename over a directory must fail")
	}
	if Exists(dst + ".partial") {
		t.Error("failed copy must remove its tmpfile")
	}
}
