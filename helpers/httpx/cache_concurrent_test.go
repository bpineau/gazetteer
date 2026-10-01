package httpx

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestWriteFileAtomicConcurrentWriters: the miss path is single-flighted per
// key now, but concurrent writers of one entry remain reachable (two processes
// sharing a cache directory, a cache-bypassing caller, a prune racing a
// write). With a fixed "<path>.tmp" they shared one O_TRUNC tmpfile and
// renamed the interleaved result; readEntry's BodyLen check hid it for bodies,
// the meta file had no such guard. Each writer must own its tmpfile.
func TestWriteFileAtomicConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "abc123.body")

	const writers = 8
	payloads := make([][]byte, writers)
	for i := range payloads {
		payloads[i] = bytes.Repeat([]byte{byte('a' + i)}, 1<<20)
	}

	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = writeFileAtomic(path, payloads[i])
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range payloads {
		if bytes.Equal(got, want) {
			if m, _ := filepath.Glob(path + ".*.tmp"); len(m) > 0 {
				t.Errorf("tmpfiles left behind: %v", m)
			}
			if fi, err := os.Stat(path); err == nil && fi.Mode().Perm() != 0o644 {
				t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
			}
			return
		}
	}
	t.Fatalf("cache entry is a mixture of concurrent writes (%d bytes, first byte %q)", len(got), got[0])
}
