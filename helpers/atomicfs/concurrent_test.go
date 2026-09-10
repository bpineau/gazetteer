package atomicfs

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestWriteFileConcurrentWritersDoNotTear: the tmpfile used to be a fixed
// "<path>.partial", so two writers of the same destination opened it O_TRUNC,
// interleaved their bytes and then both renamed the mixture into place. Each
// writer must own its tmpfile, leaving the destination as exactly one writer's
// payload.
func TestWriteFileConcurrentWritersDoNotTear(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "shared.bin")

	const writers = 8
	payloads := make([][]byte, writers)
	for i := range payloads {
		payloads[i] = bytes.Repeat([]byte{byte('a' + i)}, 1<<20) // 1 MiB, distinguishable
	}

	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = WriteFile(dst, payloads[i], 0o644)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	var matched bool
	for _, want := range payloads {
		if bytes.Equal(got, want) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("destination is a mixture of concurrent writes (%d bytes, first byte %q)", len(got), got[0])
	}
	if m, _ := filepath.Glob(dst + ".*.partial"); len(m) > 0 {
		t.Errorf("tmpfiles left behind: %v", m)
	}
	if fi, err := os.Stat(dst); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
	}
}
