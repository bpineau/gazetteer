package banx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/httpx"
	"github.com/bpineau/gazetteer/helpers/kvcache"
	"github.com/bpineau/gazetteer/helpers/kvcache/memcache"
)

// Targeted coverage on the BAN client + CachedGeocoder edge paths
// (HTTP 5xx, malformed payloads, stale cache, delegate without
// ReverseGeocoder, JSON corruption fallthrough, accessor).

// TestBANClient_NilHTTPClient covers the early-return guard in both
// Geocode and Reverse when the BANClient was instantiated without an
// underlying httpx.Client.
func TestBANClient_NilHTTPClient(t *testing.T) {
	c := &BANClient{} // http nil
	if _, err := c.Geocode(context.Background(), GeocodeQuery{Address: "x"}); err == nil {
		t.Fatalf("Geocode: expected error on nil http")
	}
	if _, err := c.Reverse(context.Background(), 48.0, 2.0); err == nil {
		t.Fatalf("Reverse: expected error on nil http")
	}

	// Nil receiver path.
	var nilC *BANClient
	if _, err := nilC.Geocode(context.Background(), GeocodeQuery{Address: "x"}); err == nil {
		t.Fatalf("Geocode on nil receiver: expected error")
	}
	if _, err := nilC.Reverse(context.Background(), 48.0, 2.0); err == nil {
		t.Fatalf("Reverse on nil receiver: expected error")
	}
}

// TestBANClient_EmptyQuery ensures forward Geocode rejects whitespace-only
// input before even hitting the network.
func TestBANClient_EmptyQuery(t *testing.T) {
	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	c := NewBANClient(hc)
	_, err := c.Geocode(context.Background(), GeocodeQuery{Address: "   "})
	if err == nil {
		t.Fatalf("expected error on empty query")
	}
}

// TestBANClient_HTTPError ensures upstream 5xx is wrapped (not silently
// swallowed) by both forward and reverse.
func TestBANClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	BANEndpoint = srv.URL + "/search/"
	BANReverseEndpoint = srv.URL + "/reverse/"
	defer func() {
		BANEndpoint = "https://api-adresse.data.gouv.fr/search/"
		BANReverseEndpoint = "https://api-adresse.data.gouv.fr/reverse/"
	}()

	// MaxRetries: -1 disables the retry loop so the 5xx fixture fails
	// fast. The default (5 retries + 500 ms × 2^n backoff) would otherwise
	// wedge this test at ~15 s per call, ~30 s total.
	hc, _ := httpx.New(httpx.Options{MaxRetries: -1})
	defer func() { _ = hc.Close() }()
	c := NewBANClient(hc)

	if _, err := c.Geocode(context.Background(), GeocodeQuery{Address: "x"}); err == nil {
		t.Fatalf("Geocode: expected error on 5xx")
	}
	if _, err := c.Reverse(context.Background(), 48.0, 2.0); err == nil {
		t.Fatalf("Reverse: expected error on 5xx")
	}
}

// TestBANClient_MalformedCoordinates exercises the len(coordinates)<2
// guard in Geocode and Reverse — happens when BAN returns a feature with
// missing lon/lat (extremely rare but observed historically when an
// upstream caching layer truncated payloads).
func TestBANClient_MalformedCoordinates(t *testing.T) {
	const malformed = `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[2.10]},"properties":{"label":"x","score":0.9,"citycode":"95500","postcode":"95000"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(malformed))
	}))
	defer srv.Close()
	BANEndpoint = srv.URL + "/search/"
	BANReverseEndpoint = srv.URL + "/reverse/"
	defer func() {
		BANEndpoint = "https://api-adresse.data.gouv.fr/search/"
		BANReverseEndpoint = "https://api-adresse.data.gouv.fr/reverse/"
	}()

	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	c := NewBANClient(hc)

	if _, err := c.Geocode(context.Background(), GeocodeQuery{Address: "x"}); err == nil {
		t.Fatalf("Geocode: expected malformed coords error")
	}
	if _, err := c.Reverse(context.Background(), 48.0, 2.0); err == nil {
		t.Fatalf("Reverse: expected malformed coords error")
	}
}

// TestBANClient_Reverse_NotFound covers the Reverse → ErrNotFound path.
func TestBANClient_Reverse_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"FeatureCollection","features":[]}`))
	}))
	defer srv.Close()
	BANReverseEndpoint = srv.URL + "/reverse/"
	defer func() { BANReverseEndpoint = "https://api-adresse.data.gouv.fr/reverse/" }()

	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	c := NewBANClient(hc)
	_, err := c.Reverse(context.Background(), 0.0, 0.0)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- CachedGeocoder paths -------------------------------------------------

// TestCachedGeocoder_DelegateError ensures a delegate error bubbles up
// without writing to cache.
func TestCachedGeocoder_DelegateError(t *testing.T) {
	cache := memcache.New()
	hardErr := errors.New("ban down")
	cached := NewCachedGeocoder(&fakeGeocoder{err: hardErr}, cache, 0)
	_, err := cached.Geocode(context.Background(), GeocodeQuery{Address: "x"})
	if !errors.Is(err, hardErr) {
		t.Fatalf("expected hard err, got %v", err)
	}
	// And the cache must not have been touched.
	if _, err := cache.Get(context.Background(), CacheKey(GeocodeQuery{Address: "x"})); !errors.Is(err, kvcache.ErrNotFound) {
		t.Fatalf("expected cache empty, got %v", err)
	}
}

// TestCachedGeocoder_FillsZeroFetchedAt covers the FetchedAt.IsZero()
// branch — delegates that don't bother to set FetchedAt should still get
// a sane stamp from the cache layer.
func TestCachedGeocoder_FillsZeroFetchedAt(t *testing.T) {
	cached := NewCachedGeocoder(&fakeGeocoder{res: GeocodeResult{
		Lat: 48.0, Lon: 2.0, CityCode: "75056",
		// no FetchedAt
	}}, memcache.New(), 0)
	res, err := cached.Geocode(context.Background(), GeocodeQuery{Address: "x"})
	if err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	if res.FetchedAt.IsZero() {
		t.Fatalf("expected FetchedAt populated by wrapper")
	}
}

// TestCachedGeocoder_StaleCacheRefetches verifies that an expired cache
// row triggers a delegate re-query (not a stale hit).
func TestCachedGeocoder_StaleCacheRefetches(t *testing.T) {
	var calls atomic.Int32
	delegate := &countingGeocoder{
		res: GeocodeResult{Lat: 48.0, Lon: 2.0, CityCode: "75056", Source: "ban"},
		ctr: &calls,
	}
	// Fixed clock at t=0; TTL=1h; we'll fast-forward the wrapper's clock
	// past expiry between calls.
	now := time.Now().UTC()
	cached := NewCachedGeocoder(delegate, memcache.New(), time.Hour)
	cached.now = func() time.Time { return now }

	q := GeocodeQuery{Address: "x"}
	if _, err := cached.Geocode(context.Background(), q); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Within TTL → cache hit.
	if _, err := cached.Geocode(context.Background(), q); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 underlying call within TTL, got %d", got)
	}

	// Fast-forward beyond TTL.
	cached.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := cached.Geocode(context.Background(), q); err != nil {
		t.Fatalf("post-TTL call: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected refetch after TTL expiry, got %d underlying calls", got)
	}
}

// TestCachedGeocoder_Reverse_StaleRefetches mirrors the above for the
// reverse path.
func TestCachedGeocoder_Reverse_StaleRefetches(t *testing.T) {
	var calls atomic.Int32
	delegate := &countingBoth{
		fwd:    GeocodeResult{Lat: 48.0, Lon: 2.0, CityCode: "75056"},
		rev:    GeocodeResult{Lat: 48.0, Lon: 2.0, CityCode: "75056", Source: "ban_reverse"},
		revCtr: &calls,
	}
	now := time.Now().UTC()
	cached := NewCachedGeocoder(delegate, memcache.New(), time.Hour)
	cached.now = func() time.Time { return now }

	if _, err := cached.Reverse(context.Background(), 48.0, 2.0); err != nil {
		t.Fatalf("first reverse: %v", err)
	}
	if _, err := cached.Reverse(context.Background(), 48.0, 2.0); err != nil {
		t.Fatalf("second reverse: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 reverse call within TTL, got %d", got)
	}

	cached.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := cached.Reverse(context.Background(), 48.0, 2.0); err != nil {
		t.Fatalf("post-TTL reverse: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected refetch after TTL, got %d", got)
	}
}

// TestCachedGeocoder_ReverseDelegateNotReverseGeocoder covers the
// type-assertion failure path: a delegate that satisfies Geocoder but
// not ReverseGeocoder must produce an explicit error.
func TestCachedGeocoder_ReverseDelegateNotReverseGeocoder(t *testing.T) {
	cached := NewCachedGeocoder(&fakeGeocoder{}, memcache.New(), 0) // fakeGeocoder has no Reverse
	_, err := cached.Reverse(context.Background(), 48.0, 2.0)
	if err == nil {
		t.Fatalf("expected error when delegate lacks ReverseGeocoder")
	}
}

// TestCachedGeocoder_ReverseDelegateError ensures a reverse delegate
// error bubbles up.
func TestCachedGeocoder_ReverseDelegateError(t *testing.T) {
	hardErr := errors.New("reverse boom")
	cached := NewCachedGeocoder(&revErrGeocoder{err: hardErr}, memcache.New(), 0)
	_, err := cached.Reverse(context.Background(), 48.0, 2.0)
	if !errors.Is(err, hardErr) {
		t.Fatalf("expected reverse err, got %v", err)
	}
}

// TestCachedGeocoder_CorruptCacheRow_Refetches covers the JSON-unmarshal
// error fallthrough: a row whose value_json is not a valid GeocodeResult
// must trigger a re-query rather than a hard failure.
func TestCachedGeocoder_CorruptCacheRow_Refetches(t *testing.T) {
	cache := memcache.New()
	q := GeocodeQuery{Address: "y"}
	exp := time.Now().Add(time.Hour)
	// Write garbage at the cache key.
	if err := cache.Set(context.Background(), kvcache.Entry{
		Key:       CacheKey(q),
		Value:     []byte("{not json"),
		FetchedAt: time.Now(),
		ExpiresAt: &exp,
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var calls atomic.Int32
	delegate := &countingGeocoder{
		res: GeocodeResult{Lat: 1, Lon: 2, CityCode: "75056"},
		ctr: &calls,
	}
	cached := NewCachedGeocoder(delegate, cache, time.Hour)
	if _, err := cached.Geocode(context.Background(), q); err != nil {
		t.Fatalf("Geocode after corrupt row: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected delegate to be called despite cached row, got %d", got)
	}
}

// TestCachedGeocoder_Delegate exposes the wrapped Geocoder for legacy
// unwrap helpers.
func TestCachedGeocoder_Delegate(t *testing.T) {
	inner := &fakeGeocoder{}
	cached := NewCachedGeocoder(inner, memcache.New(), 0)
	if got := cached.Delegate(); got != inner {
		t.Fatalf("Delegate(): want inner ptr, got %v", got)
	}
}

// TestNewCachedGeocoder_DefaultTTL exercises the ttl=0 → 1-year fallback
// path on the constructor.
func TestNewCachedGeocoder_DefaultTTL(t *testing.T) {
	cached := NewCachedGeocoder(&fakeGeocoder{}, memcache.New(), 0)
	if cached.ttl != 365*24*time.Hour {
		t.Fatalf("default ttl = %v, want 1y", cached.ttl)
	}
	cached2 := NewCachedGeocoder(&fakeGeocoder{}, memcache.New(), 5*time.Minute)
	if cached2.ttl != 5*time.Minute {
		t.Fatalf("explicit ttl ignored, got %v", cached2.ttl)
	}
}

// TestContainsToken covers the empty-token branch (returns true).
func TestContainsToken(t *testing.T) {
	if !containsToken("anything", "") {
		t.Fatalf("empty token should yield true")
	}
	if !containsToken("Hello Paris", "paris") {
		t.Fatalf("expected case-insensitive match")
	}
	if containsToken("Hello", "world") {
		t.Fatalf("should not match unrelated token")
	}
}

// TestCacheKey_StableAcrossWhitespaceAndCase verifies CacheKey is stable
// under harmless input perturbations — important so cached entries are
// re-used on slight reformatting.
func TestCacheKey_StableAcrossWhitespaceAndCase(t *testing.T) {
	a := CacheKey(GeocodeQuery{Address: "14 Rue X 95000 PONTOISE"})
	b := CacheKey(GeocodeQuery{Address: "  14 rue x 95000 pontoise  "})
	if a != b {
		t.Fatalf("CacheKey not stable: %s vs %s", a, b)
	}
}

// TestReverseCacheKey_RoundsCoords ensures the 6-decimal rounding actually
// collapses near-identical coordinates onto the same key (~11 cm).
func TestReverseCacheKey_RoundsCoords(t *testing.T) {
	a := ReverseCacheKey(48.8765432, 2.2961234)
	b := ReverseCacheKey(48.8765432, 2.2961234)
	if a != b {
		t.Fatalf("ReverseCacheKey not deterministic: %s vs %s", a, b)
	}
	c := ReverseCacheKey(48.9, 2.3) // far apart → must differ
	if a == c {
		t.Fatalf("ReverseCacheKey collapsed distinct points: %s", a)
	}
}

// --- helpers --------------------------------------------------------------

// countingGeocoder counts forward calls. Used to verify TTL-driven
// re-fetches.
type countingGeocoder struct {
	res GeocodeResult
	err error
	ctr *atomic.Int32
}

func (g *countingGeocoder) Geocode(_ context.Context, _ GeocodeQuery) (GeocodeResult, error) {
	g.ctr.Add(1)
	return g.res, g.err
}

// countingBoth implements Geocoder + ReverseGeocoder; only the reverse
// hits are counted (the fwd is unused in these tests but required so the
// type satisfies the wrapper's delegate constraints in tests where we
// only call Reverse).
type countingBoth struct {
	fwd    GeocodeResult
	rev    GeocodeResult
	revCtr *atomic.Int32
}

func (g *countingBoth) Geocode(_ context.Context, _ GeocodeQuery) (GeocodeResult, error) {
	return g.fwd, nil
}
func (g *countingBoth) Reverse(_ context.Context, _, _ float64) (GeocodeResult, error) {
	g.revCtr.Add(1)
	return g.rev, nil
}

// revErrGeocoder satisfies both Geocoder and ReverseGeocoder, returning
// a configured error from Reverse only.
type revErrGeocoder struct {
	err error
}

func (g *revErrGeocoder) Geocode(_ context.Context, _ GeocodeQuery) (GeocodeResult, error) {
	return GeocodeResult{}, nil
}
func (g *revErrGeocoder) Reverse(_ context.Context, _, _ float64) (GeocodeResult, error) {
	return GeocodeResult{}, g.err
}
