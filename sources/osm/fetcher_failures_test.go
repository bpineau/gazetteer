package osm

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

// None of the tests in this file runs in parallel: Query reads the
// package-level overpassMirrorTimeout, which setMirrorTimeoutForTest
// rewrites, so a parallel reader would race the rewrite (and report it under
// -race). They are all sub-second anyway, retries being disabled below.

// quietFetcher builds a fetcher whose single endpoint is srv, with no
// fallback mirror and no log output.
func quietFetcher(t *testing.T, endpoint string) *HTTPOverpassFetcher {
	t.Helper()
	// MaxRetries: -1 keeps one attempt per mirror: the default 5 retries with
	// exponential backoff would turn every 5xx/429 fixture below into a
	// multi-second wait, and the fetcher's own fallback walk is what is
	// under test, not httpx's retry loop.
	hc, err := httpx.New(httpx.Options{RateLimitPerHost: 1000, BurstPerHost: 1000, MaxRetries: -1})
	if err != nil {
		t.Fatalf("httpx: %v", err)
	}
	f := NewHTTPOverpassFetcher(hc, endpoint)
	f.fallbacks = nil
	f.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return f
}

func TestOverpassFetcher_Guards(t *testing.T) {
	// No HTTP client: nothing to post with.
	var bare HTTPOverpassFetcher
	if _, err := bare.Query(context.Background(), "[out:json];"); err == nil ||
		!strings.Contains(err.Error(), "nil http client") {
		t.Errorf("Query on a client-less fetcher = %v, want a nil-http-client error", err)
	}

	f := quietFetcher(t, "http://127.0.0.1:1/api/interpreter")
	for _, ql := range []string{"", "   ", "\n\t"} {
		if _, err := f.Query(context.Background(), ql); err == nil ||
			!strings.Contains(err.Error(), "empty QL") {
			t.Errorf("Query(%q) = %v, want an empty-QL error", ql, err)
		}
	}
}

// TestOverpassFetcher_PostsFormEncodedQL pins the wire protocol every public
// mirror serves: the QL travels in the `data` form field, with an honest
// User-Agent (overpass-api.de answers 406 to browser-mimicking agents).
func TestOverpassFetcher_PostsFormEncodedQL(t *testing.T) {
	var (
		gotMethod, gotCT, gotUA, gotAccept, gotData string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotCT = r.Method, r.Header.Get("Content-Type")
		gotUA, gotAccept = r.Header.Get("User-Agent"), r.Header.Get("Accept")
		_ = r.ParseForm()
		gotData = r.Form.Get("data")
		_, _ = w.Write([]byte(`{"elements":[]}`))
	}))
	defer srv.Close()

	ql := FranceTransitOverpassQL("48.81,2.22,48.91,2.42")
	body, err := quietFetcher(t, srv.URL).Query(context.Background(), ql)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if string(body) != `{"elements":[]}` {
		t.Errorf("body = %s", body)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotUA != overpassUserAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, overpassUserAgent)
	}
	if gotData != ql {
		t.Errorf("data field = %q, want the QL verbatim", gotData)
	}
}

// TestOverpassFetcher_HTTPFailures: every non-2xx answer is an error carrying
// the mirror's own plain-text explanation (rate limit, syntax error,
// overload), which is what the caller logs.
func TestOverpassFetcher_HTTPFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"rate limited", http.StatusTooManyRequests, "runtime error: Too many requests from your IP"},
		{"server overloaded", http.StatusServiceUnavailable, "The server is probably too busy"},
		{"gateway timeout", http.StatusGatewayTimeout, "timeout"},
		{"agent rejected", http.StatusNotAcceptable, "not acceptable"},
		{"internal error", http.StatusInternalServerError, "boom"},
		{"redirect not followed to a body", http.StatusMovedPermanently, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			_, err := quietFetcher(t, srv.URL).Query(context.Background(), "[out:json];node(1);out;")
			if err == nil {
				t.Fatalf("HTTP %d must be an error", c.status)
			}
			if !strings.Contains(err.Error(), "overpass HTTP") {
				t.Errorf("err = %v, want an overpass-HTTP error", err)
			}
			if c.body != "" && !strings.Contains(err.Error(), c.body) {
				t.Errorf("err = %v, want the mirror's message %q passed through", err, c.body)
			}
		})
	}
}

// TestOverpassFetcher_ErrorPreviewIsBounded: the mirror's message is quoted
// in the error, but only its first 512 characters.
func TestOverpassFetcher_ErrorPreviewIsBounded(t *testing.T) {
	long := strings.Repeat("x", 2000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(long))
	}))
	defer srv.Close()

	_, err := quietFetcher(t, srv.URL).Query(context.Background(), "[out:json];node(1);out;")
	if err == nil {
		t.Fatal("HTTP 400 must be an error")
	}
	if n := strings.Count(err.Error(), "x"); n != 512 {
		t.Errorf("error quotes %d body characters, want the 512-char preview", n)
	}
}

// TestOverpassFetcher_BodyCap: going through HTTPClient().Do bypasses httpx's
// own read bound, so the fetcher caps the answer itself. A body one byte past
// the cap must be REFUSED, not silently truncated into an unparsable catalog.
func TestOverpassFetcher_BodyCap(t *testing.T) {
	old := maxOverpassBodyBytes
	maxOverpassBodyBytes = 64
	t.Cleanup(func() { maxOverpassBodyBytes = old })

	cases := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"under the cap", 32, false},
		{"exactly at the cap", 64, false},
		{"one byte over", 65, true},
		{"far over", 4096, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(strings.Repeat("y", c.size)))
			}))
			defer srv.Close()

			body, err := quietFetcher(t, srv.URL).Query(context.Background(), "[out:json];node(1);out;")
			switch {
			case c.wantErr:
				if err == nil || !strings.Contains(err.Error(), "exceeds") {
					t.Fatalf("a %d-byte answer = (%d bytes, %v), want an exceeds-cap error", c.size, len(body), err)
				}
			default:
				if err != nil {
					t.Fatalf("a %d-byte answer = %v, want nil", c.size, err)
				}
				if len(body) != c.size {
					t.Errorf("body = %d bytes, want %d", len(body), c.size)
				}
			}
		})
	}
}

// TestOverpassFetcher_AllMirrorsFail: the walk tries every mirror, warns per
// mirror, escalates once, and hands the last error back.
func TestOverpassFetcher_AllMirrorsFail(t *testing.T) {
	down := func(status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
	}
	primary, fallback := down(http.StatusInternalServerError), down(http.StatusTooManyRequests)
	defer primary.Close()
	defer fallback.Close()

	f := quietFetcher(t, primary.URL)
	f.fallbacks = []string{fallback.URL}
	logger, rec := newRecorder()
	f.SetLogger(logger)

	_, err := f.Query(context.Background(), "[out:json];node(1);out;")
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v, want the LAST mirror's error (429)", err)
	}
	if !rec.has("osm.mirror_failed") || !rec.has("osm.all_mirrors_failed") {
		t.Error("a total outage must warn per mirror and escalate once")
	}
	// Both mirrors now carry one failure each.
	for _, ep := range []string{primary.URL, fallback.URL} {
		if got := f.streak(ep); got != 1 {
			t.Errorf("streak(%s) = %d, want 1", ep, got)
		}
	}
}

// TestOverpassFetcher_SkipThresholdAndProbe: a mirror that keeps failing is
// skipped outright, but not forever — every mirrorProbeEvery-th skip lets a
// probe through, so a recovered mirror rejoins the rotation.
func TestOverpassFetcher_SkipThresholdAndProbe(t *testing.T) {
	var hits int
	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"elements":[]}`))
	}))
	defer srv.Close()

	f := quietFetcher(t, srv.URL)
	logger, rec := newRecorder()
	f.SetLogger(logger)

	for range mirrorSkipThreshold {
		if _, err := f.Query(context.Background(), "[out:json];node(1);out;"); err == nil {
			t.Fatal("a 500 must be an error")
		}
	}
	if hits != mirrorSkipThreshold {
		t.Fatalf("mirror hit %d times, want %d before the skip kicks in", hits, mirrorSkipThreshold)
	}

	// Now skipped: the error says so and the mirror is not contacted.
	_, err := f.Query(context.Background(), "[out:json];node(1);out;")
	if err == nil || !strings.Contains(err.Error(), "consecutive failures") {
		t.Fatalf("err = %v, want a skipped-mirror error", err)
	}
	if hits != mirrorSkipThreshold {
		t.Errorf("a skipped mirror was still contacted (%d hits)", hits)
	}
	if !rec.has("osm.mirror_skipped") {
		t.Error("the skip must be visible in the logs")
	}

	// The mirror recovers; the periodic probe finds out and the streak resets.
	fail = false
	var recovered bool
	for range mirrorProbeEvery {
		if _, err := f.Query(context.Background(), "[out:json];node(1);out;"); err == nil {
			recovered = true
			break
		}
	}
	if !recovered {
		t.Fatal("a recovered mirror never rejoined: the probe window is closed")
	}
	if got := f.streak(srv.URL); got != 0 {
		t.Errorf("streak after a success = %d, want 0", got)
	}
}

// TestOverpassFetcher_CallerCancellationSparesTheStreak is the regression
// test for a real bug: a caller that walks away (Ctrl-C, an abandoned
// Collect) used to be folded into every mirror's consecutive-failure streak.
// Three abandoned refreshes were enough to blacklist a perfectly healthy
// rotation for the next mirrorProbeEvery-1 calls of the live Source's
// long-lived fetcher. A DEADLINE the caller set for the fetch is the
// opposite and still counts.
func TestOverpassFetcher_CallerCancellationSparesTheStreak(t *testing.T) {
	var hits int
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"elements":[]}`))
	}))
	defer healthy.Close()

	f := quietFetcher(t, healthy.URL)
	f.fallbacks = []string{healthy.URL + "/fallback"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Query(ctx, "[out:json];node(1);out;"); err == nil {
		t.Fatal("a Query under a cancelled context must fail")
	}
	if hits != 0 {
		t.Errorf("mirror contacted %d times under a cancelled context, want 0", hits)
	}
	for _, ep := range []string{healthy.URL, healthy.URL + "/fallback"} {
		if got := f.streak(ep); got != 0 {
			t.Errorf("streak(%s) = %d after a caller cancellation, want 0", ep, got)
		}
	}
	// The mirrors are untouched, so the very next live call still goes out.
	if _, err := f.Query(context.Background(), "[out:json];node(1);out;"); err != nil {
		t.Fatalf("Query after a cancellation = %v, want nil", err)
	}
	if hits != 1 {
		t.Errorf("mirror hit %d times, want 1", hits)
	}
}

// TestOverpassFetcher_DeadlineCountsAgainstTheMirror is the other half of the
// rule above: a mirror that eats the whole per-attempt slice IS the signal
// the skip logic exists for.
func TestOverpassFetcher_DeadlineCountsAgainstTheMirror(t *testing.T) {
	stop := make(chan struct{})
	hung := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select { // hang past the per-attempt slice, but never past the test
		case <-r.Context().Done():
		case <-stop:
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(hung.Close) // runs second (cleanups are LIFO)
	t.Cleanup(func() { close(stop) })

	restore := setMirrorTimeoutForTest(t, 50*time.Millisecond)
	defer restore()

	f := quietFetcher(t, hung.URL)
	if _, err := f.Query(context.Background(), "[out:json];node(1);out;"); err == nil {
		t.Fatal("a hung mirror must fail")
	}
	if got := f.streak(hung.URL); got != 1 {
		t.Errorf("streak = %d after a per-mirror timeout, want 1", got)
	}
}
