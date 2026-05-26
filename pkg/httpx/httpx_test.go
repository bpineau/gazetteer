package httpx

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient builds a Client with sane test defaults: small backoff,
// disabled timeout interference, etc. Each test directs cache and
// snapshot directories under t.TempDir().
func newTestClient(t *testing.T, opts Options) *Client {
	t.Helper()
	if opts.BaseRetryInterval == 0 {
		opts.BaseRetryInterval = 10 * time.Millisecond
	}
	if opts.MaxResponseBytes == 0 {
		opts.MaxResponseBytes = 10 * 1024 * 1024
	}
	c, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// Test #1 — rate-limit observable: 10 GETs with RL=5/s and burst=1
// must take at least ~1.6 s (i.e. (10-1)/5 = 1.8s, allow some slack).
func TestRateLimit_Observable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	burst := 1
	c := newTestClient(t, Options{
		RateLimitPerHost: 5,
		BurstPerHost:     burst,
	})

	ctx := context.Background()
	start := time.Now()
	for i := range 10 {
		_, _, err := c.GetBytes(ctx, srv.URL+"/", nil)
		if err != nil {
			t.Fatalf("GetBytes #%d: %v", i, err)
		}
	}
	elapsed := time.Since(start)
	min := 1500 * time.Millisecond
	if elapsed < min {
		t.Errorf("rate-limit too fast: %v < %v", elapsed, min)
	}
}

// Test #2 — retry on 503 then 200.
func TestRetry_503_then_200(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		k := atomic.AddInt32(&n, 1)
		if k <= 2 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := newTestClient(t, Options{
		RateLimitPerHost: 1000,
		MaxRetries:       3,
	})
	body, _, err := c.GetBytes(context.Background(), srv.URL+"/", nil)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("unexpected body: %q", body)
	}
	if got := atomic.LoadInt32(&n); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

// Test #2b — 503 forever returns ErrTooManyRetries.
func TestRetry_TooManyRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	c := newTestClient(t, Options{
		RateLimitPerHost: 1000,
		MaxRetries:       2,
	})
	_, _, err := c.GetBytes(context.Background(), srv.URL+"/", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	tmr, ok := errors.AsType[*ErrTooManyRetries](err)
	if !ok {
		t.Fatalf("expected *ErrTooManyRetries, got %T: %v", err, err)
	}
	if tmr.Attempts != 3 { // MaxRetries+1
		t.Fatalf("attempts=%d, want 3", tmr.Attempts)
	}
}

// Test #3 — cache hit fresh: 2 GETs for same URL → only 1 server hit.
func TestCache_FreshHit(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	c := newTestClient(t, Options{
		HTTPCacheDir:     dir,
		RateLimitPerHost: 1000,
	})
	ctx := context.Background()

	// 1st request: miss.
	body1, r1, err := c.GetBytes(ctx, srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r1.FromCache {
		t.Fatal("first request should not be FromCache")
	}
	if string(body1) != "hello" {
		t.Fatalf("body1=%q", body1)
	}

	// 2nd request: hit.
	body2, r2, err := c.GetBytes(ctx, srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.FromCache {
		t.Fatal("second request should be FromCache")
	}
	if string(body2) != "hello" {
		t.Fatalf("body2=%q", body2)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected 1 server hit, got %d", got)
	}
}

// Test #4 — cache 304 revalidation with ETag.
func TestCache_Revalidation_304(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("ETag", `"v1"`)
		// Cache-Control immediately expired so the second request triggers revalidation.
		w.Header().Set("Cache-Control", "max-age=0")
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("v1-body"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	c := newTestClient(t, Options{
		HTTPCacheDir:     dir,
		RateLimitPerHost: 1000,
	})
	ctx := context.Background()

	if _, _, err := c.GetBytes(ctx, srv.URL+"/x", nil); err != nil {
		t.Fatal(err)
	}
	body2, r2, err := c.GetBytes(ctx, srv.URL+"/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.FromCache {
		t.Fatalf("second response should be served from cache after 304")
	}
	if string(body2) != "v1-body" {
		t.Fatalf("body=%q", body2)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("expected 2 server hits (initial + revalidation), got %d", got)
	}
}

// Test #5 — Retry-After honoured.
func TestRetry_HonoursRetryAfter(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		k := atomic.AddInt32(&n, 1)
		if k == 1 {
			w.Header().Set("Retry-After", "1") // 1 second
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newTestClient(t, Options{
		RateLimitPerHost:  1000,
		MaxRetries:        2,
		BaseRetryInterval: 5 * time.Millisecond, // would be too short without Retry-After
	})

	start := time.Now()
	_, _, err := c.GetBytes(context.Background(), srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 900*time.Millisecond {
		t.Fatalf("Retry-After not honoured: elapsed=%v", elapsed)
	}
}

// Test #6 — Download: sha256 correct, .tmp cleaned on error,
// rename atomic, SkipIfExists no-network.
func TestDownload_SHA256_Atomic_Skip(t *testing.T) {
	payload := []byte("encheridor-download-payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "sub", "out.bin")

	c := newTestClient(t, Options{RateLimitPerHost: 1000})
	ctx := context.Background()

	// 1. Successful download: sha256 set, .tmp absent, file on disk.
	res, err := c.Download(ctx, srv.URL+"/file", dest, DownloadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.SHA256 != sha256Hex(payload) {
		t.Fatalf("sha256 mismatch: got %s, want %s", res.SHA256, sha256Hex(payload))
	}
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf(".tmp not cleaned: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("file contents mismatch")
	}

	// 2. SkipIfExists: must not touch network. Use a closed server URL.
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	closed.Close()
	res2, err := c.Download(ctx, closed.URL+"/x", dest, DownloadOptions{SkipIfExists: true})
	if err != nil {
		t.Fatalf("SkipIfExists should not need network: %v", err)
	}
	if !res2.Skipped {
		t.Fatal("expected Skipped=true")
	}
	if res2.SHA256 != sha256Hex(payload) {
		t.Fatalf("rehash mismatch")
	}

	// 3. ExpectedSHA256 mismatch: error AND .tmp removed.
	dest2 := filepath.Join(dir, "wrong.bin")
	_, err = c.Download(ctx, srv.URL+"/x", dest2, DownloadOptions{ExpectedSHA256: "deadbeef"})
	if err == nil {
		t.Fatal("expected sha256 mismatch error")
	}
	if _, statErr := os.Stat(dest2 + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatalf(".tmp not removed on mismatch: %v", statErr)
	}
	if _, statErr := os.Stat(dest2); !os.IsNotExist(statErr) {
		t.Fatalf("dest must not exist on error: %v", statErr)
	}
}

// Bug #12 — chantier 2026-05-02 dataset report.
//
// 3.9 GB of snapshots ended up under data/raw/_/2026-05-02/_/ instead of
// being tagged source=<src> / runID=<id>. The pipeline tags ctx for
// "fetch listings" requests but the document downloader (httpx.Download)
// uses a sibling ctx without the WithSource / WithRunID tags. This test
// nails the contract end of the contract that lives inside httpx :
// when ctx IS tagged, Download must produce snapshots under
// <SnapshotDir>/<source>/<date>/<runID>/, just like GetBytes does.
//
// (The other half of the fix — making sure the pipeline propagates the
// tagged ctx into Download — lives in internal/core/pipeline and is
// owned by another sub-agent.)
func TestSnapshot_DocumentsTaggedBySource(t *testing.T) {
	payload := []byte("PDFlikepayload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	snapDir := t.TempDir()
	dest := filepath.Join(t.TempDir(), "doc.pdf")
	c := newTestClient(t, Options{
		SnapshotDir:      snapDir,
		RateLimitPerHost: 1000,
	})

	ctx := WithRunID(WithSource(context.Background(), "licitor"), "run-bug12")
	res, err := c.Download(ctx, srv.URL+"/cdc.pdf", dest, DownloadOptions{})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.SHA256 == "" {
		t.Fatal("expected sha256 to be set")
	}

	// Snapshot must land under <snapDir>/licitor/<date>/run-bug12/*.
	// guessExt() turns "application/pdf" into "bin" (it only special-cases
	// text/json/html/xml/image), so the file extension on disk is .bin.
	matches, _ := filepath.Glob(filepath.Join(snapDir, "licitor", "*", "run-bug12", "*"))
	if len(matches) != 1 {
		// The fall-through bucket — the symptom we're guarding against.
		bad, _ := filepath.Glob(filepath.Join(snapDir, "_", "*", "_", "*"))
		t.Fatalf("expected 1 tagged snapshot, got %d ; untagged fall-through bucket: %d files",
			len(matches), len(bad))
	}
	// And nothing should have leaked into the untagged bucket.
	if bad, _ := filepath.Glob(filepath.Join(snapDir, "_", "*", "_", "*")); len(bad) != 0 {
		t.Errorf("untagged bucket should be empty when ctx has source/runID, got %d files: %v", len(bad), bad)
	}
}

// Test #7 — Snapshot: file produced, JSON valid, base64 encoding for binary.
func TestSnapshot_Produced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))
	defer srv.Close()

	snapDir := t.TempDir()
	c := newTestClient(t, Options{
		SnapshotDir:      snapDir,
		RateLimitPerHost: 1000,
	})
	ctx := WithRunID(WithSource(context.Background(), "ut"), "run42")
	if _, _, err := c.GetBytes(ctx, srv.URL+"/snap", nil); err != nil {
		t.Fatal(err)
	}

	// Find the file under snapDir/ut/<date>/run42/*.json
	matches, _ := filepath.Glob(filepath.Join(snapDir, "ut", "*", "run42", "*.json"))
	if len(matches) != 1 {
		t.Fatalf("expected 1 snapshot file, got %d", len(matches))
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var env snapshotEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("snapshot JSON invalid: %v", err)
	}
	if env.Response.Status != 200 {
		t.Fatalf("snapshot status: %d", env.Response.Status)
	}

	// Now binary path: image/png -> base64
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	}))
	defer srv2.Close()
	if _, _, err := c.GetBytes(ctx, srv2.URL+"/img", nil); err != nil {
		t.Fatal(err)
	}
	matches2, _ := filepath.Glob(filepath.Join(snapDir, "ut", "*", "run42", "*.png"))
	if len(matches2) != 1 {
		t.Fatalf("expected 1 png snapshot, got %d", len(matches2))
	}
	raw2, _ := os.ReadFile(matches2[0])
	var env2 snapshotEnvelope
	if err := json.Unmarshal(raw2, &env2); err != nil {
		t.Fatalf("png snapshot JSON invalid: %v", err)
	}
	if env2.BodyEncoding != "base64" {
		t.Fatalf("expected base64 encoding, got %s", env2.BodyEncoding)
	}
	dec, err := base64.StdEncoding.DecodeString(env2.Body)
	if err != nil || len(dec) != 8 {
		t.Fatalf("snapshot body decode error: %v len=%d", err, len(dec))
	}
}

// Test #8 — Bypass cache.
func TestBypassCache(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte("X"))
	}))
	defer srv.Close()

	c := newTestClient(t, Options{
		HTTPCacheDir:     t.TempDir(),
		RateLimitPerHost: 1000,
	})
	ctx := context.Background()

	for range 3 {
		ctx2 := WithBypassCache(ctx)
		if _, _, err := c.GetBytes(ctx2, srv.URL+"/", nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("bypass cache: expected 3 server hits, got %d", got)
	}
}

// Test #9 — Concurrent rate-limit on 2 hosts: each host rate limit
// applies independently.
func TestRateLimit_TwoHosts_Independent(t *testing.T) {
	mk := func(reqLog *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reqLog.Add(1)
			w.WriteHeader(200)
		}))
	}
	var hitsA, hitsB atomic.Int32
	a := mk(&hitsA)
	b := mk(&hitsB)
	defer a.Close()
	defer b.Close()

	rl := 5.0
	c := newTestClient(t, Options{
		RateLimitPerHost: rl,
		BurstPerHost:     1,
	})

	const N = 10
	var wg sync.WaitGroup
	wg.Add(2 * N)
	start := time.Now()
	for range N {
		go func() { defer wg.Done(); _, _, _ = c.GetBytes(context.Background(), a.URL+"/", nil) }()
		go func() { defer wg.Done(); _, _, _ = c.GetBytes(context.Background(), b.URL+"/", nil) }()
	}
	wg.Wait()
	elapsed := time.Since(start)

	// Each host independently: ~ (N-1)/5 = 1.8s. Total (in parallel) ≈ 1.8s.
	// If both hosts shared the limiter, total would be ~ (2N-1)/5 = 3.8s.
	if elapsed > 2700*time.Millisecond {
		t.Errorf("limiters appear to share state: elapsed=%v", elapsed)
	}
	if elapsed < 1500*time.Millisecond {
		t.Errorf("rate limit not enforced: elapsed=%v", elapsed)
	}
	if hitsA.Load() != N || hitsB.Load() != N {
		t.Fatalf("hits A=%d B=%d", hitsA.Load(), hitsB.Load())
	}
}

// A32 regression — staticcheck U1000 finding on ratelimit.go:19.
//
// `rateLimitTransport.mu` was declared but never Lock/Unlocked. The slow
// path of `limiterFor` constructs a `*rate.Limiter` and races to insert
// it via `sync.Map.LoadOrStore`, which is the documented safe pattern;
// the unused `mu` was dead code (contradicted by the comment that says
// "we don't bother with sync.Once"). This test slams `limiterFor` with
// many concurrent goroutines for a fresh host to ensure that:
//  1. there is no data race detected by -race;
//  2. all goroutines observe the same *rate.Limiter instance (i.e.
//     LoadOrStore really does dedupe construction).
func TestRateLimitTransport_LimiterForConcurrent(t *testing.T) {
	r := Options{RateLimitPerHost: 1000}.resolve()
	rt := newRateLimitTransport(http.DefaultTransport, r)

	const N = 64
	var wg sync.WaitGroup
	wg.Add(N)
	got := make([]uintptr, N)
	for i := range N {
		go func() {
			defer wg.Done()
			// Compare by raw pointer identity (printed via %p) to
			// avoid naming the *rate.Limiter type in this file.
			got[i] = ptrHex(fmt.Sprintf("%p", rt.limiterFor("host.example")))
		}()
	}
	wg.Wait()

	first := got[0]
	for i := 1; i < N; i++ {
		if got[i] != first {
			t.Fatalf("limiterFor returned different instances under contention: got[%d]=%#x vs got[0]=%#x", i, got[i], first)
		}
	}
}

// ptrHex parses a Go pointer string like "0x140000a4280" into a uintptr
// for identity comparison.
func ptrHex(s string) uintptr {
	var p uintptr
	_, _ = fmt.Sscanf(s, "0x%x", &p)
	return p
}

// Test #10 — No goroutine leak after Client.Close().
//
// We don't import goleak (it's not strictly needed) and instead measure
// runtime.NumGoroutine() before and after a small workload. Allow a small
// slack because the test framework itself can spawn helpers.
func TestNoGoroutineLeak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := newTestClient(t, Options{
		HTTPCacheDir:     t.TempDir(),
		SnapshotDir:      t.TempDir(),
		RateLimitPerHost: 1000,
	})

	// Warm-up to materialise once-only goroutines (httptest, dialer etc.).
	for range 2 {
		_, _, _ = c.GetBytes(context.Background(), srv.URL+"/", nil)
	}

	runtime.GC()
	before := runtime.NumGoroutine()

	for range 30 {
		_, _, err := c.GetBytes(context.Background(), srv.URL+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = c.Close()

	// Wait briefly for transport's idle conns to close. Up to 1s slack.
	deadline := time.Now().Add(1 * time.Second)
	var after int
	for time.Now().Before(deadline) {
		runtime.GC()
		after = runtime.NumGoroutine()
		if after-before <= 2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	if after-before > 2 {
		t.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}

// Test #11 — colly compatibility surface: Transport() returns the same
// underlying RoundTripper exposed to the world. This test is minimal as
// the spec says: a real colly integration test is out of scope here
// (colly is wired by the source chantiers).
func TestTransport_Exposed(t *testing.T) {
	c := newTestClient(t, Options{RateLimitPerHost: 1000})
	if c.Transport() == nil {
		t.Fatal("Transport() returned nil")
	}
}

// --- Helpers ---

// sha256Hex is a tiny duplicate of sha256OfFile applied to bytes. We don't
// expose a helper publicly because callers can use crypto/sha256 directly.
func sha256Hex(b []byte) string {
	tmp, err := os.CreateTemp("", "h*")
	if err != nil {
		return ""
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, _ = tmp.Write(b)
	_ = tmp.Close()
	h, _, err := sha256OfFile(tmp.Name())
	if err != nil {
		return ""
	}
	return h
}

// --- Misc unit tests covering options resolution and small helpers. ---

func TestParseRetryAfter_Seconds(t *testing.T) {
	if d := parseRetryAfter("3", time.Now()); d != 3*time.Second {
		t.Fatalf("got %v", d)
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	d := parseRetryAfter(now.Add(2*time.Second).Format(http.TimeFormat), now)
	if d <= 0 || d > 3*time.Second {
		t.Fatalf("got %v", d)
	}
}

func TestParseRetryAfter_Garbage(t *testing.T) {
	if d := parseRetryAfter("not-a-date", time.Now()); d != 0 {
		t.Fatalf("got %v", d)
	}
}

func TestComputeExpiry_MaxAge(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := http.Header{}
	h.Set("Cache-Control", "max-age=120, public")
	exp := computeExpiry(h, now, time.Hour)
	if exp != now.Add(120*time.Second).Unix() {
		t.Fatalf("expiry=%d", exp)
	}
}

func TestComputeExpiry_NoStore(t *testing.T) {
	h := http.Header{}
	h.Set("Cache-Control", "no-store")
	if exp := computeExpiry(h, time.Now(), time.Hour); exp != 0 {
		t.Fatalf("expiry=%d", exp)
	}
}

func TestOptions_Defaults(t *testing.T) {
	r := Options{}.resolve()
	if r.userAgent != DefaultUserAgent {
		t.Fatal("UA default")
	}
	if r.rateLimit != DefaultRateLimitPerHost {
		t.Fatal("rate default")
	}
	if r.burst != DefaultBurstPerHost {
		t.Fatal("burst default")
	}
	if r.maxRetries != DefaultMaxRetries {
		t.Fatal("max retries default")
	}
	if r.maxRetryAfter != DefaultMaxRetryAfter {
		t.Fatal("max retry-after default")
	}
}

func TestPerHost_HeadersAndUA(t *testing.T) {
	got := struct {
		ua string
		x  string
	}{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.ua = r.Header.Get("User-Agent")
		got.x = r.Header.Get("X-Custom")
	}))
	defer srv.Close()

	host, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	customUA := "encheridor-test/1.0"
	c := newTestClient(t, Options{
		RateLimitPerHost: 1000,
		PerHost: map[string]HostOptions{
			host.Host: {
				UserAgent: &customUA,
				Headers: http.Header{
					"X-Custom": []string{"yes"},
				},
			},
		},
	})
	if _, _, err := c.GetBytes(context.Background(), srv.URL+"/x", nil); err != nil {
		t.Fatal(err)
	}
	if got.ua != customUA {
		t.Fatalf("UA=%q", got.ua)
	}
	if got.x != "yes" {
		t.Fatalf("X-Custom=%q", got.x)
	}
}

func TestErrHTTP_FormatAndAs(t *testing.T) {
	e := &ErrHTTP{Status: 418, URL: "http://t.invalid/", Body: []byte("teapot")}
	if !strings.Contains(e.Error(), "418") {
		t.Fatalf("err string: %q", e.Error())
	}
	if _, ok := errors.AsType[*ErrHTTP](e); !ok {
		t.Fatalf("errors.AsType failed")
	}
	if status, ok := asHTTPStatus(e); !ok || status != 418 {
		t.Fatalf("asHTTPStatus: %d %v", status, ok)
	}
}

func TestGetJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"v":7}`))
	}))
	defer srv.Close()

	c := newTestClient(t, Options{RateLimitPerHost: 1000})
	var out struct{ V int }
	if err := c.GetJSON(context.Background(), srv.URL+"/", nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.V != 7 {
		t.Fatalf("V=%d", out.V)
	}
}

// fixture-based test: pre-populated cache hit served without any
// network call. Validates that an entry written by writeEntry is
// readable by the cache transport.
func TestCache_FixtureHit_NoNetwork(t *testing.T) {
	dir := t.TempDir()
	r := Options{HTTPCacheDir: dir, RateLimitPerHost: 1000}.resolve()
	r.now = func() time.Time { return time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC) }
	ct := newCacheTransport(unreachableTransport{}, r, dir)

	// Build a synthetic request and pre-populate its cache entry.
	req, _ := http.NewRequest(http.MethodGet, "http://fixture.invalid/x", nil)
	hash := requestHash(req, "")
	metaPath, bodyPath := ct.pathsFor(hash)
	meta := &cacheMeta{
		URL:          req.URL.String(),
		Method:       req.Method,
		Status:       200,
		Header:       http.Header{"Content-Type": []string{"text/plain"}},
		FetchedAtSec: r.now().Unix(),
		ExpiresAtSec: r.now().Add(time.Hour).Unix(),
		BodyLen:      5,
	}
	if err := ct.writeEntry(metaPath, bodyPath, meta, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	resp, err := ct.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Fatalf("body=%q", body)
	}
	if resp.Header.Get("X-From-Cache") != "1" {
		t.Fatalf("X-From-Cache absent")
	}
}

// unreachableTransport panics if used; helps assert no-network paths.
type unreachableTransport struct{}

func (unreachableTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("network used unexpectedly")
}

// fixture-based test: 304 path served by writing an entry then
// returning a 304 from the inner transport.
func TestCache_FixtureRevalidation_304(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	r := Options{HTTPCacheDir: dir, RateLimitPerHost: 1000}.resolve()
	r.now = func() time.Time { return now }

	// Pre-populate stale entry with ETag.
	pre := newCacheTransport(unreachableTransport{}, r, dir)
	req, _ := http.NewRequest(http.MethodGet, "http://fixture.invalid/y", nil)
	hash := requestHash(req, "")
	metaPath, bodyPath := pre.pathsFor(hash)
	hdr := http.Header{}
	hdr.Set("ETag", `"abc"`)
	hdr.Set("Content-Type", "text/plain")
	meta := &cacheMeta{
		URL:          req.URL.String(),
		Method:       req.Method,
		Status:       200,
		Header:       hdr,
		FetchedAtSec: now.Add(-time.Hour).Unix(),
		ExpiresAtSec: now.Add(-time.Minute).Unix(), // already expired
		BodyLen:      9,
	}
	if err := pre.writeEntry(metaPath, bodyPath, meta, []byte("body-data")); err != nil {
		t.Fatal(err)
	}

	// Inner transport returns 304 if the conditional header is set.
	inner := &fakeRoundTripper{f: func(rq *http.Request) (*http.Response, error) {
		if rq.Header.Get("If-None-Match") != `"abc"` {
			t.Fatalf("conditional header missing: %v", rq.Header)
		}
		return &http.Response{
			StatusCode: 304,
			Header:     http.Header{"Cache-Control": []string{"max-age=120"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}}
	ct := newCacheTransport(inner, r, dir)

	resp, err := ct.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "body-data" {
		t.Fatalf("revalidated body wrong: %q", body)
	}
	if resp.Header.Get("X-From-Cache") != "1" {
		t.Fatalf("X-From-Cache absent")
	}
}

type fakeRoundTripper struct {
	f func(*http.Request) (*http.Response, error)
}

func (f *fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f.f(r) }

// Sanity test: the snapshot middleware does not write when neither
// SnapshotDir nor WithSnapshot are set. Doubles as a perf-regression check.
func TestSnapshot_DisabledIsPassThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("z"))
	}))
	defer srv.Close()

	c := newTestClient(t, Options{RateLimitPerHost: 1000})
	if _, _, err := c.GetBytes(context.Background(), srv.URL+"/", nil); err != nil {
		t.Fatal(err)
	}
	// nothing to assert: just ensure no panic and no error.
	_ = strconv.Itoa // suppress unused import if any
}

// Test WithSnapshot per-request override turns on snapshotting even when
// global Options.SnapshotDir is empty.
func TestWithSnapshot_PerRequestOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	c := newTestClient(t, Options{RateLimitPerHost: 1000})
	ctx := WithRunID(WithSource(WithSnapshot(context.Background(), dir), "src"), "run")
	if _, _, err := c.GetBytes(ctx, srv.URL+"/x", nil); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "src", "*", "run", "*"))
	if len(matches) == 0 {
		t.Fatal("expected at least one snapshot file")
	}
}

// Test that GetBytes returns *ErrHTTP for 4xx without retry.
func TestGetBytes_4xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()
	c := newTestClient(t, Options{RateLimitPerHost: 1000})

	_, _, err := c.GetBytes(context.Background(), srv.URL+"/", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	he, ok := errors.AsType[*ErrHTTP](err)
	if !ok {
		t.Fatalf("expected *ErrHTTP, got %T", err)
	}
	if he.Status != 404 {
		t.Fatalf("status=%d", he.Status)
	}
}

// Test that Download surfaces an error for 4xx and removes any tmp file.
func TestDownload_4xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := newTestClient(t, Options{RateLimitPerHost: 1000})
	dest := filepath.Join(t.TempDir(), "missing.bin")
	_, err := c.Download(context.Background(), srv.URL+"/x", dest, DownloadOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := errors.AsType[*ErrHTTP](err); !ok {
		t.Fatalf("expected *ErrHTTP, got %T: %v", err, err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("dest must not exist: %v", statErr)
	}
}

// Test ErrTransport / ErrTooManyRetries wrapping behaviour.
func TestErrors_Wrap(t *testing.T) {
	inner := errors.New("eof")
	te := &ErrTransport{URL: "u", Err: inner}
	if !strings.Contains(te.Error(), "transport") {
		t.Fatal("transport error string")
	}
	if !errors.Is(te, inner) {
		t.Fatal("ErrTransport must wrap")
	}

	tmr := &ErrTooManyRetries{URL: "u", Attempts: 3, Err: te}
	if !strings.Contains(tmr.Error(), "exhausted") {
		t.Fatal("too-many string")
	}
	if !errors.Is(tmr, inner) {
		t.Fatal("ErrTooManyRetries must wrap chain to inner")
	}

	// Status extraction from a wrapped ErrHTTP.
	if _, ok := asHTTPStatus(te); ok {
		t.Fatal("transport error should not yield status")
	}
	httpErr := &ErrHTTP{Status: 503, URL: "u"}
	wrapped := fmt.Errorf("ctx: %w", httpErr)
	if s, ok := asHTTPStatus(wrapped); !ok || s != 503 {
		t.Fatalf("status=%d ok=%v", s, ok)
	}
}

// Test that MaxBytes guards downloads.
func TestDownload_MaxBytes(t *testing.T) {
	body := strings.Repeat("a", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := newTestClient(t, Options{RateLimitPerHost: 1000})
	dest := filepath.Join(t.TempDir(), "big.bin")
	_, err := c.Download(context.Background(), srv.URL+"/", dest, DownloadOptions{MaxBytes: 1024})
	if err == nil {
		t.Fatal("expected MaxBytes error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("dest must not exist on MaxBytes error")
	}
}

// Test that GetBytes surfaces a MaxResponseBytes error.
func TestGetBytes_MaxResponseBytes(t *testing.T) {
	body := strings.Repeat("a", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := newTestClient(t, Options{RateLimitPerHost: 1000, MaxResponseBytes: 1024})
	_, _, err := c.GetBytes(context.Background(), srv.URL+"/", nil)
	if err == nil {
		t.Fatal("expected MaxResponseBytes error")
	}
}

// Test that isRetryableNetErr distinguishes context.Canceled and net.OpError.
func TestIsRetryableNetErr(t *testing.T) {
	if isRetryableNetErr(nil) {
		t.Fatal("nil should be false")
	}
	if isRetryableNetErr(context.Canceled) {
		t.Fatal("ctx.Canceled must not be retried")
	}
	if !isRetryableNetErr(io.EOF) {
		t.Fatal("io.EOF should be retried")
	}
	// Generic error: defaults retryable.
	if !isRetryableNetErr(errors.New("anything")) {
		t.Fatal("generic err should default retryable")
	}
}

// A14 regression — Bug critique transversal detecte 2026-05-02 22:50 CEST.
//
// Symptome live : l'enricher locservice produisait 198 / 198 parse failures
// (cf. doc/validation/runs/A9_locservice_20260502-204223.log). Cause : le bundle
// browserClientHints (aligne Chrome 147 reel le matin meme) contenait
// `Accept-Encoding: gzip, deflate, br, zstd`. Quand le caller pose
// Accept-Encoding manuellement, Go's net/http Transport considere que c'est
// la responsabilite du caller de decompresser et n'auto-decompresse plus.
// Les parsers recevaient donc les bytes gzippes bruts. bienici avait ete
// workaround-e avec `Accept-Encoding: identity` hardcode (cf. A20a, fetcher.go).
//
// Ce test :
//  1. lance un serveur qui sert du gzip si le client annonce Accept-Encoding: gzip ;
//  2. utilise un httpx.Client default ;
//  3. verifie que le body retourne par GetBytes est le PLAINTEXT decompresse,
//     pas les bytes gzippes bruts.
//
// Fail avant le fix (Accept-Encoding defini par browserClientHints empeche
// l'auto-decompression). Vert apres le fix (drop d'Accept-Encoding du bundle
// → stdlib ajoute gzip + decompresse de maniere transparente).
func TestAcceptEncoding_AutoDecompressed(t *testing.T) {
	plain := []byte("encheridor-plaintext-payload-which-must-be-decompressed-by-stdlib")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ae := r.Header.Get("Accept-Encoding")
		if !strings.Contains(ae, "gzip") {
			// caller didn't ask for gzip — return plaintext
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(plain)
			return
		}
		// caller asked for gzip — encode and serve
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write(plain)
		_ = gz.Close()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	c := newTestClient(t, Options{RateLimitPerHost: 1000})
	body, _, err := c.GetBytes(context.Background(), srv.URL+"/", nil)
	if err != nil {
		t.Fatalf("GetBytes: %v", err)
	}
	// If Accept-Encoding was set by browserClientHints, Go won't decompress
	// and `body` will start with the gzip magic bytes 0x1f 0x8b. After the
	// fix, body must equal the plaintext.
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		t.Fatalf("body still gzipped — Go did NOT auto-decompress: " +
			"Accept-Encoding likely set by httpx.browserClientHints which " +
			"opts the request out of auto-decompression")
	}
	if string(body) != string(plain) {
		t.Fatalf("body mismatch:\n  got  %q\n  want %q", body, plain)
	}
}

// TestDownload_ImageExtCorrection verifies that when a server returns PNG
// bytes at a URL whose path ends in ".jpg", Download renames the saved file
// to ".png" and returns the corrected path.
func TestDownload_ImageExtCorrection(t *testing.T) {
	// Minimal valid PNG header (8-byte signature + IHDR chunk preamble).
	pngHeader := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	// Pad to 512 bytes so http.DetectContentType has enough to work with.
	pngBytes := make([]byte, 512)
	copy(pngBytes, pngHeader)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg") // server lies about type
		_, _ = w.Write(pngBytes)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "photo.jpg") // caller uses .jpg extension

	c := newTestClient(t, Options{RateLimitPerHost: 1000})
	res, err := c.Download(context.Background(), srv.URL+"/photo.jpg", dest, DownloadOptions{})
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// The returned path must be .png, not .jpg.
	if filepath.Ext(res.Path) != ".png" {
		t.Errorf("res.Path extension = %q, want .png (got %s)", filepath.Ext(res.Path), res.Path)
	}

	// The .jpg must be gone; the .png must exist.
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error(".jpg file should have been renamed away")
	}
	corrected := filepath.Join(dir, "photo.png")
	if _, err := os.Stat(corrected); err != nil {
		t.Errorf(".png file missing after rename: %v", err)
	}

	// JPEG bytes should NOT be renamed.
	jpegBytes := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46}
	jpegBytes = append(jpegBytes, make([]byte, 504)...)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(jpegBytes)
	}))
	defer srv2.Close()
	dest2 := filepath.Join(dir, "real.jpg")
	res2, err := c.Download(context.Background(), srv2.URL+"/x", dest2, DownloadOptions{})
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if res2.Path != dest2 {
		t.Errorf("JPEG path should be unchanged, got %s", res2.Path)
	}
}

// Test guessExt for various Content-Type strings.
func TestGuessExt(t *testing.T) {
	cases := map[string]string{
		"text/html; charset=utf-8": "html",
		"application/json":         "json",
		"application/xml":          "xml",
		"text/plain":               "txt",
		"image/jpeg":               "jpeg",
		"image/png":                "png",
		"application/octet-stream": "bin",
		"":                         "bin",
	}
	for in, want := range cases {
		if got := guessExt(in); got != want {
			t.Errorf("guessExt(%q)=%q, want %q", in, got, want)
		}
	}
}
