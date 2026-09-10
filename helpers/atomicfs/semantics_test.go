package atomicfs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestWriteFile_HonoursPerm pins the mode of the destination. os.CreateTemp
// always makes the tmpfile 0600, so the mode the caller asked for only
// reaches the destination because createTemp chmods it; and the mode is not
// umask-filtered (fchmod ignores the umask), which is why the exact bits
// are asserted rather than a "at least" relation.
func TestWriteFile_HonoursPerm(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		perm os.FileMode
	}{
		{"private", 0o600},
		{"group-readable", 0o640},
		{"world-readable", 0o644},
		{"executable", 0o755},
		{"read-only", 0o444},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			dst := filepath.Join(dir, "out.bin")
			if err := WriteFile(dst, []byte("payload"), c.perm); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			assertMode(t, dst, c.perm)

			// Copy through the same discipline: same mode contract.
			cp := filepath.Join(dir, "copy.bin")
			if err := CopyFile(dst, cp, c.perm); err != nil {
				t.Fatalf("CopyFile: %v", err)
			}
			assertMode(t, cp, c.perm)
		})
	}
}

// TestWriteFile_OverwriteReplacesMode: rename(2) swaps the whole inode, so
// the destination takes the NEW file's mode, not the mode it had before.
func TestWriteFile_OverwriteReplacesMode(t *testing.T) {
	t.Parallel()
	dst := filepath.Join(t.TempDir(), "out.bin")
	if err := WriteFile(dst, []byte("first"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := WriteFile(dst, []byte("second"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	assertMode(t, dst, 0o644)
	if got, _ := os.ReadFile(dst); string(got) != "second" {
		t.Errorf("content = %q, want %q", got, "second")
	}
}

// TestWriteFile_EmptyPayload: a zero-byte write is a legitimate write, and
// it must still land atomically (NonEmpty then reports false, which is the
// whole point of the stat-based pre-validation helpers).
func TestWriteFile_EmptyPayload(t *testing.T) {
	t.Parallel()
	dst := filepath.Join(t.TempDir(), "empty.json")
	if err := WriteFile(dst, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !Exists(dst) {
		t.Error("Exists = false, want true for a zero-byte destination")
	}
	if NonEmpty(dst, 0) {
		t.Error("NonEmpty(min=0) = true, want false for a zero-byte file")
	}
}

// TestWriteFile_UnwritableDir: the tmpfile is created next to the
// destination, so a directory the process cannot write to fails BEFORE any
// destination state is touched.
func TestWriteFile_UnwritableDir(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode bits do not deny writes")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) // let TempDir clean up

	dst := filepath.Join(dir, "out.bin")
	err := WriteFile(dst, []byte("x"), 0o644)
	if err == nil || !strings.Contains(err.Error(), "create tmpfile") {
		t.Errorf("WriteFile into an unwritable dir = %v, want a create-tmpfile error", err)
	}
	if Exists(dst) {
		t.Error("destination created despite the failure")
	}

	// CopyFile fails the same way, and only after the source opened fine.
	src := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dst, 0o644); err == nil || !strings.Contains(err.Error(), "create tmpfile") {
		t.Errorf("CopyFile into an unwritable dir = %v, want a create-tmpfile error", err)
	}
}

// TestCopyFile_UnreadableSource covers the streaming failure: the source
// opens but the copy itself errors (a directory reads as EISDIR). The
// tmpfile must be gone and the previous destination content intact.
func TestCopyFile_UnreadableSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "adirectory")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst.bin")
	if err := os.WriteFile(dst, []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := CopyFile(srcDir, dst, 0o644)
	if err == nil || !strings.Contains(err.Error(), "atomicfs") {
		t.Fatalf("CopyFile(directory) = %v, want an atomicfs-prefixed error", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "previous" {
		t.Errorf("destination clobbered by a failed copy: %q", got)
	}
	if m, _ := filepath.Glob(dst + ".*.partial"); len(m) > 0 {
		t.Errorf("failed copy left tmpfiles behind: %v", m)
	}
}

// TestSeal_SyncFailureCleansTmp exercises the fsync leg directly: an
// already-closed file cannot be flushed, so seal must report it and unlink
// the tmpfile instead of renaming an unflushed file into place.
func TestSeal_SyncFailureCleansTmp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.bin")
	out, tmp, err := createTemp(dst, 0o644)
	if err != nil {
		t.Fatalf("createTemp: %v", err)
	}
	if err := out.Close(); err != nil { // sabotage the fsync below
		t.Fatal(err)
	}
	err = seal(out, tmp, dst)
	if err == nil || !strings.Contains(err.Error(), "sync") {
		t.Fatalf("seal on a closed file = %v, want a sync error", err)
	}
	if Exists(dst) {
		t.Error("destination created although the flush failed")
	}
	if m, _ := filepath.Glob(dst + ".*.partial"); len(m) > 0 {
		t.Errorf("failed seal left tmpfiles behind: %v", m)
	}
}

// TestCreateTemp_NamesAreUnique is the property the concurrency fix rests
// on: two writers of the same destination never share a tmpfile.
func TestCreateTemp_NamesAreUnique(t *testing.T) {
	t.Parallel()
	dst := filepath.Join(t.TempDir(), "same.bin")
	seen := map[string]bool{}
	for range 16 {
		out, tmp, err := createTemp(dst, 0o600)
		if err != nil {
			t.Fatalf("createTemp: %v", err)
		}
		if seen[tmp] {
			t.Fatalf("tmpfile name %q handed out twice", tmp)
		}
		seen[tmp] = true
		if !strings.HasPrefix(filepath.Base(tmp), "same.bin.") ||
			!strings.HasSuffix(tmp, ".partial") {
			t.Errorf("tmpfile %q must be a <dst>.<random>.partial sibling", tmp)
		}
		_ = out.Close()
		_ = os.Remove(tmp)
	}
}

// TestCopyFile_StreamsLargeSource: the copy goes through io.Copy, so a
// source far larger than one read buffer must arrive byte-identical.
func TestCopyFile_StreamsLargeSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "big.bin")
	payload := bytes.Repeat([]byte("gazetteer"), 300_000) // ~2.7 MiB
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "big.copy")
	if err := CopyFile(src, dst, 0o644); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("copy differs from source (%d bytes vs %d)", len(got), len(payload))
	}
}

// TestCopyFile_ConcurrentWritersDoNotTear is CopyFile's half of the
// per-writer-tmpfile contract: the destination ends up as exactly one
// source's bytes, never a mixture.
func TestCopyFile_ConcurrentWritersDoNotTear(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dst := filepath.Join(dir, "shared.bin")

	const writers = 8
	srcs := make([]string, writers)
	payloads := make([][]byte, writers)
	for i := range writers {
		payloads[i] = bytes.Repeat([]byte{byte('a' + i)}, 1<<20) // 1 MiB, distinguishable
		srcs[i] = filepath.Join(dir, string(rune('a'+i))+".bin")
		if err := os.WriteFile(srcs[i], payloads[i], 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = CopyFile(srcs[i], dst, 0o644)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("copier %d: %v", i, err)
		}
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range payloads {
		if bytes.Equal(got, want) {
			if m, _ := filepath.Glob(dst + ".*.partial"); len(m) > 0 {
				t.Errorf("tmpfiles left behind: %v", m)
			}
			return
		}
	}
	t.Fatalf("destination is a mixture of concurrent copies (%d bytes, first byte %q)", len(got), got[0])
}

// TestStatHelpers tabulates Exists / NonEmpty over every shape the doc
// promises to reject: a directory, a broken symlink, a missing path.
func TestStatHelpers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(file, []byte("1234"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.bin")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "broken.link")
	if err := os.Symlink(filepath.Join(dir, "nowhere"), broken); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "good.link")
	if err := os.Symlink(file, good); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name        string
		path        string
		min         int64
		wantExists  bool
		wantNonEmpt bool
	}{
		{"regular file", file, 3, true, true},
		{"regular file, min at size", file, 4, true, false},
		{"empty file", empty, 0, true, false},
		{"directory", sub, 0, false, false},
		{"broken symlink", broken, 0, false, false},
		{"symlink to a file", good, 3, true, true}, // Stat follows the link
		{"missing", filepath.Join(dir, "nope"), 0, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Exists(c.path); got != c.wantExists {
				t.Errorf("Exists = %v, want %v", got, c.wantExists)
			}
			if got := NonEmpty(c.path, c.min); got != c.wantNonEmpt {
				t.Errorf("NonEmpty(min=%d) = %v, want %v", c.min, got, c.wantNonEmpt)
			}
		})
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := fi.Mode().Perm(); got != want {
		t.Errorf("%s mode = %v, want %v", filepath.Base(path), got, want)
	}
}
