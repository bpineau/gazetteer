package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// readSnapshots returns every envelope written under dir, with its mode.
func readSnapshots(t *testing.T, dir string) ([]snapshotEnvelope, []os.FileMode) {
	t.Helper()
	var envs []snapshotEnvelope
	var modes []os.FileMode
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(p) //nolint:gosec // test temp dir
		if rerr != nil {
			t.Fatal(rerr)
		}
		var e snapshotEnvelope
		if jerr := json.Unmarshal(b, &e); jerr != nil {
			t.Fatalf("%s: %v", p, jerr)
		}
		info, _ := d.Info()
		envs = append(envs, e)
		modes = append(modes, info.Mode().Perm())
		return nil
	})
	return envs, modes
}

// TestSnapshot_KeepsTheResponseSizeBound pins that enabling snapshots does
// not lift MaxResponseBytes: the capture stops at the limit and says so,
// and GetBytes still rejects the oversized body.
func TestSnapshot_KeepsTheResponseSizeBound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 128)))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	c := newTestClient(t, Options{SnapshotDir: dir, RateLimitPerHost: 1000, MaxResponseBytes: 16})
	if _, _, err := c.GetBytes(context.Background(), srv.URL, nil); err == nil || !strings.Contains(err.Error(), "MaxResponseBytes=16") {
		t.Fatalf("GetBytes err = %v, want the size-limit error", err)
	}
	envs, _ := readSnapshots(t, dir)
	if len(envs) != 1 {
		t.Fatalf("%d snapshots, want 1", len(envs))
	}
	if !envs[0].BodyTruncated || len(envs[0].Body) != 16 {
		t.Errorf("snapshot truncated %v with %d body bytes, want true and 16", envs[0].BodyTruncated, len(envs[0].Body))
	}
}

// TestSnapshot_RedactsCredentialsAndStaysPrivate pins that an envelope
// never records a credential and is readable by its owner only.
func TestSnapshot_RedactsCredentialsAndStaysPrivate(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable) // one retry, for Attempts
			return
		}
		w.Header().Set("Set-Cookie", "session=s3cr3t")
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	c := newTestClient(t, Options{SnapshotDir: dir, RateLimitPerHost: 1000, MaxRetries: 1, BaseRetryInterval: 1, BackoffCap: 1})
	_, resp, err := c.GetBytes(context.Background(), srv.URL+"/?apikey=k3y&q=1", http.Header{
		"Authorization": {"Bearer t0ken"},
		"Cookie":        {"a=b"},
		"X-Api-Key":     {"k3y"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Attempts != 2 {
		t.Errorf("Response.Attempts = %d, want 2 (one 503, one retry)", resp.Attempts)
	}
	envs, modes := readSnapshots(t, dir)
	if len(envs) != 1 {
		t.Fatalf("%d snapshots, want 1", len(envs))
	}
	raw, _ := json.Marshal(envs[0])
	for _, secret := range []string{"t0ken", "k3y", "a=b", "s3cr3t"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("snapshot leaks %q: %s", secret, raw)
		}
	}
	if envs[0].Response.Attempts != 2 {
		t.Errorf("snapshot attempts = %d, want 2", envs[0].Response.Attempts)
	}
	if modes[0] != snapshotFileMode {
		t.Errorf("snapshot mode = %v, want %v", modes[0], snapshotFileMode)
	}
}

// TestDownload_MaxBytesOverridesTheClientLimitWithACache pins that a
// download's own MaxBytes applies with the disk cache enabled: the cache
// used to buffer the body under the client's smaller limit first.
func TestDownload_MaxBytesOverridesTheClientLimitWithACache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "max-age=600")
		_, _ = w.Write([]byte(strings.Repeat("x", 128)))
	}))
	t.Cleanup(srv.Close)
	for _, cacheDir := range []string{"", t.TempDir()} {
		c := newTestClient(t, Options{HTTPCacheDir: cacheDir, RateLimitPerHost: 1000, MaxResponseBytes: 16})
		res, err := c.Download(context.Background(), srv.URL, filepath.Join(t.TempDir(), "raw.bin"), DownloadOptions{MaxBytes: 256})
		if err != nil || res.Bytes != 128 {
			t.Errorf("cache dir %q: Download = (%d bytes, %v), want 128 bytes", cacheDir, res.Bytes, err)
		}
	}
}
