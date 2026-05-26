package banx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/pkg/httpx"
	"github.com/bpineau/gazetteer/pkg/kvcache"
	"github.com/bpineau/gazetteer/pkg/kvcache/memcache"
)

// stubGeocoder lets tests inject a deterministic GeocodeResult.
type stubGeocoder struct {
	res   GeocodeResult
	err   error
	calls atomic.Int32
}

func (s *stubGeocoder) Geocode(_ context.Context, _ GeocodeQuery) (GeocodeResult, error) {
	s.calls.Add(1)
	return s.res, s.err
}

const banFixture = `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[2.103,49.046]},"properties":{"label":"14 Rue de la Tournelle 95000 Pontoise","score":0.91,"citycode":"95500","postcode":"95000"}}]}`

func TestGeocodeQueryString(t *testing.T) {
	cases := []struct {
		q    GeocodeQuery
		want string
	}{
		{GeocodeQuery{Address: "14 rue de la Tournelle 95150 Pontoise"},
			"14 rue de la Tournelle 95150 Pontoise"},
		{GeocodeQuery{Address: "14 rue x", City: "Pontoise", Zip: "95150"},
			"14 rue x 95150 Pontoise"},
		{GeocodeQuery{Address: "10 rue test 75007 Paris", City: "Paris", Zip: "75007"},
			"10 rue test 75007 Paris"},
	}
	for _, tc := range cases {
		got := tc.q.String()
		if got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
}

func TestBANClient_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(banFixture))
	}))
	defer srv.Close()
	BANEndpoint = srv.URL + "/search/"
	defer func() { BANEndpoint = "https://api-adresse.data.gouv.fr/search/" }()

	hc, err := httpx.New(httpx.Options{})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	defer func() { _ = hc.Close() }()

	c := NewBANClient(hc)
	res, err := c.Geocode(context.Background(), GeocodeQuery{Address: "14 rue de la Tournelle 95150 Pontoise"})
	if err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	if res.Lat == 0 || res.Lon == 0 {
		t.Fatalf("expected non-zero coords, got %+v", res)
	}
	if res.CityCode != "95500" {
		t.Fatalf("expected citycode 95500, got %q", res.CityCode)
	}
	if res.Score < 0.7 {
		t.Fatalf("expected score > 0.7, got %v", res.Score)
	}
	if res.Label == "" {
		t.Fatalf("expected non-empty label")
	}
}

// TestBANClient_TruncatesOverlongQuery — overlong-input safety.
//
// Upstream callers occasionally pass an entire prose blob as the address
// (e.g. "Vente du 09 avril 2026 à 14h30 - Un appartement à
// Asnières-sur-Seine (92600)..."), producing 600+ char URLs that BAN
// rejects with HTTP 400. The client now truncates to the API's
// documented 200-char cap before issuing the GET, so a pathological
// input degrades to a best-effort partial query rather than a hard
// error.
func TestBANClient_TruncatesOverlongQuery(t *testing.T) {
	var seenQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"FeatureCollection","features":[]}`))
	}))
	defer srv.Close()
	BANEndpoint = srv.URL + "/search/"
	defer func() { BANEndpoint = "https://api-adresse.data.gouv.fr/search/" }()

	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	c := NewBANClient(hc)

	// Replicate one of the real-world failures: a long prose blob
	// stuffed into the address field. Before truncation this hit BAN
	// with a ~600-char `q` and got HTTP 400.
	huge := "Vente du 09 avril 2026 à 14h30 - Un appartement à Asnières-sur-Seine (92600) - 79 quai Aulagnier - Mise à prix 175.000 € - Visite le 30 mars 2026 de 09h30 à 10h30, frais préalables 9377.40€, adjugé 259 000 € 92600 Asnières-sur-Seine"
	if len(huge) <= banMaxQueryLen {
		t.Fatalf("test fixture too short: %d <= %d", len(huge), banMaxQueryLen)
	}
	_, err := c.Geocode(context.Background(), GeocodeQuery{Address: huge})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if got := len(seenQuery); got > banMaxQueryLen {
		t.Fatalf("BAN saw q of len %d, want <= %d (truncation broken)", got, banMaxQueryLen)
	}
}

func TestBANClient_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"FeatureCollection","features":[]}`))
	}))
	defer srv.Close()
	BANEndpoint = srv.URL + "/search/"
	defer func() { BANEndpoint = "https://api-adresse.data.gouv.fr/search/" }()

	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	c := NewBANClient(hc)
	_, err := c.Geocode(context.Background(), GeocodeQuery{Address: "nowhere"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// reverseFixture mirrors the BAN reverse-geocoding response shape for
// (lat=48.876, lon=2.296) → 17e arrondissement (75117).
const reverseFixture = `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[2.296,48.876]},"properties":{"label":"5 Rue Brey 75017 Paris","score":0.95,"citycode":"75117","postcode":"75017"}}]}`

// TestCachedGeocoder_ImplementsReverseGeocoder is the A20 reproducer:
// when a Geocoder is wrapped by CachedGeocoder, the INSEEResolver
// cascade must still be able to invoke reverse on it. Before A20, the
// wrapper did not implement ReverseGeocoder, so DVF/pappersimmo
// resolveINSEE failed-fast on low-score forwards.
func TestCachedGeocoder_ImplementsReverseGeocoder(t *testing.T) {
	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	cached := NewCachedGeocoder(NewBANClient(hc), memcache.New(), 0)

	if _, ok := any(cached).(ReverseGeocoder); !ok {
		t.Fatalf("CachedGeocoder does not implement ReverseGeocoder")
	}
}

// TestINSEEResolver_CachedGeocoderUnlocksReverse is the integration-flavour
// reproducer: when the only handle the resolver has is a CachedGeocoder
// wrapping a BANClient, a low-score forward must fall through to the
// reverse step instead of returning ErrNotFound.
func TestINSEEResolver_CachedGeocoderUnlocksReverse(t *testing.T) {
	// Forward: returns score=0.4 (below 0.7 threshold) → cascade should fall through.
	// Reverse: returns the correct INSEE.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/":
			_, _ = w.Write([]byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[2.296,48.876]},"properties":{"label":"x","score":0.4,"citycode":"75017","postcode":"75017"}}]}`))
		case "/reverse/":
			_, _ = w.Write([]byte(reverseFixture))
		default:
			http.NotFound(w, r)
		}
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
	cached := NewCachedGeocoder(NewBANClient(hc), memcache.New(), 0)

	// The INSEEResolver only sees `cached` as Forward. For the cascade
	// to reach reverse, cached must also satisfy ReverseGeocoder.
	rev, ok := any(cached).(ReverseGeocoder)
	if !ok {
		t.Fatalf("CachedGeocoder must implement ReverseGeocoder for INSEEResolver cascade to work")
	}
	resolver := &INSEEResolver{Forward: cached, Reverse: rev}
	got, err := resolver.Resolve(context.Background(), INSEEQuery{
		Address: "Résidence X, Lot Y", Lat: 48.876, Lon: 2.296,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Source != "ban_reverse" {
		t.Errorf("Source = %q, want ban_reverse", got.Source)
	}
	if got.INSEE != "75117" {
		t.Errorf("INSEE = %q, want 75117", got.INSEE)
	}
}

// TestCachedGeocoder_ReverseHitsCache verifies that a second reverse
// lookup on the same coords does not hit the upstream server.
func TestCachedGeocoder_ReverseHitsCache(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reverseFixture))
	}))
	defer srv.Close()
	BANReverseEndpoint = srv.URL + "/reverse/"
	defer func() { BANReverseEndpoint = "https://api-adresse.data.gouv.fr/reverse/" }()

	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	cached := NewCachedGeocoder(NewBANClient(hc), memcache.New(), 0)
	rev, ok := any(cached).(ReverseGeocoder)
	if !ok {
		t.Fatalf("CachedGeocoder must implement ReverseGeocoder")
	}

	if _, err := rev.Reverse(context.Background(), 48.876, 2.296); err != nil {
		t.Fatalf("first reverse: %v", err)
	}
	if _, err := rev.Reverse(context.Background(), 48.876, 2.296); err != nil {
		t.Fatalf("second reverse: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 underlying reverse call, got %d", got)
	}
}

func TestCachedGeocoder_HitsCache(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(banFixture))
	}))
	defer srv.Close()
	BANEndpoint = srv.URL + "/search/"
	defer func() { BANEndpoint = "https://api-adresse.data.gouv.fr/search/" }()

	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	cached := NewCachedGeocoder(NewBANClient(hc), memcache.New(), 0)

	q := GeocodeQuery{Address: "14 rue de la Tournelle 95150 Pontoise"}
	_, err := cached.Geocode(context.Background(), q)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	_, err = cached.Geocode(context.Background(), q)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 underlying call, got %d", got)
	}
}

// TestValidateCoherence is a unit test for the dept-prefix predicate
// covering all branches: match, Corsica, DOM-TOM, mismatch, empty.
func TestValidateCoherence(t *testing.T) {
	cases := []struct {
		name    string
		res     GeocodeResult
		wantErr bool
	}{
		{"match", GeocodeResult{CityCode: "75056", PostCode: "75011"}, false},
		{"corsica_2A", GeocodeResult{CityCode: "2A004", PostCode: "20000"}, false},
		{"corsica_2B", GeocodeResult{CityCode: "2B033", PostCode: "20200"}, false},
		{"domtom_97_97", GeocodeResult{CityCode: "97411", PostCode: "97400"}, false},
		{"domtom_97_98", GeocodeResult{CityCode: "97123", PostCode: "98000"}, false},
		{"empty_citycode", GeocodeResult{CityCode: "", PostCode: "75011"}, false},
		{"empty_postcode", GeocodeResult{CityCode: "75056", PostCode: ""}, false},
		{"both_empty", GeocodeResult{}, false},
		{"mismatch_75_22", GeocodeResult{CityCode: "75117", PostCode: "22680"}, true},
		{"mismatch_corsica_to_metro", GeocodeResult{CityCode: "2A004", PostCode: "75011"}, true},
		{"mismatch_metro_to_domtom", GeocodeResult{CityCode: "75056", PostCode: "97400"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCoherence(tc.res)
			if tc.wantErr {
				if !errors.Is(err, ErrIncoherentBANResponse) {
					t.Errorf("got %v, want ErrIncoherentBANResponse", err)
				}
			} else if err != nil {
				t.Errorf("got %v, want nil", err)
			}
		})
	}
}

// cacheHas reports whether a row for the given key exists in the cache.
func cacheHas(t *testing.T, c kvcache.Cache, key string) bool {
	t.Helper()
	_, err := c.Get(context.Background(), key)
	if err == nil {
		return true
	}
	if errors.Is(err, kvcache.ErrNotFound) {
		return false
	}
	t.Fatalf("kvcache get: %v", err)
	return false
}

// TestZipsShareDepartment covers the exported dept-prefix predicate used
// by the BAN cache layer (and any future geo consumer that wants a
// single helper instead of re-implementing the 2-digit / DOM-TOM
// branching). Mirrors the semantics of the unexported helpers already
// present in castorus / bienici / meilleursagents (memory
// `zipmatch_enricher_protocol`).
func TestZipsShareDepartment(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		// Métropolitain — same dept, same prefix.
		{"identical_92", "92100", "92100", true},
		// Métropolitain — same dept, different commune.
		{"same_dept_92", "92100", "92500", true},
		// Paris arrondissements share dept 75.
		{"paris_arr_11_19", "75011", "75019", true},
		{"paris_arr_1_20", "75001", "75020", true},
		// Métropolitain — different dept.
		{"adverse_92_62", "92100", "62100", false},
		{"adverse_78_42", "78550", "42xxx", false}, // Bazainville case
		{"adverse_77_57", "77160", "57100", false}, // Chenoise case
		// DOM-TOM — same territory (3-digit prefix).
		{"dom_974_same", "97400", "97490", true},
		{"dom_971_same", "97110", "97180", true},
		// DOM-TOM — different territory inside DOM block.
		{"dom_974_vs_972", "97400", "97200", false},
		{"dom_971_vs_976", "97110", "97600", false},
		// Cross-zone: métropolitain vs DOM.
		{"metro_vs_dom", "75011", "97400", false},
		// Empty inputs → no anchor → no rejection.
		{"empty_a", "", "75011", true},
		{"empty_b", "92100", "", true},
		{"empty_both", "", "", true},
		// Whitespace handling.
		{"whitespace_a", " 92100 ", "92500", true},
		// Malformed shorter than 2 → falls back to equality.
		{"malformed_short_eq", "9", "9", true},
		{"malformed_short_ne", "9", "8", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ZipsShareDepartment(tc.a, tc.b); got != tc.want {
				t.Errorf("ZipsShareDepartment(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			// Symmetric.
			if got := ZipsShareDepartment(tc.b, tc.a); got != tc.want {
				t.Errorf("ZipsShareDepartment(%q, %q) = %v, want %v (symmetry)", tc.b, tc.a, got, tc.want)
			}
		})
	}
}

// TestCachedGeocoder_DeptGuard covers the input-vs-output dept-guard:
// when the caller anchors the query with an explicit zip and BAN returns
// a candidate whose PostCode is in a different département, the cache
// layer returns ErrDepartmentMismatch and skips the write. Reproduces
// the four real-world IDs (110726 Bazainville 78xxx → Loire 42xxx, etc.)
// from the round-11 bug hunt.
func TestCachedGeocoder_DeptGuard(t *testing.T) {
	cases := []struct {
		name      string
		inputZip  string
		banPC     string
		wantMatch bool
	}{
		// Adverse: Bazainville 78550 → Loire commune in dept 42.
		{"bazainville_drift", "78550", "42050", false},
		// Adverse: Chenoise 77160 → Moselle 57xxx.
		{"chenoise_drift", "77160", "57100", false},
		// Adverse: typical homonyme 92100 → 62100.
		{"adverse_92_62", "92100", "62100", false},
		// Matching: Paris arrondissement (intra-75).
		{"paris_arr_intra", "75011", "75019", true},
		// Matching: same dept, different commune.
		{"same_dept_match", "92100", "92500", true},
		// DOM-TOM match (Réunion).
		{"dom_974_match", "97400", "97490", true},
		// DOM-TOM adverse (974 ↔ 972).
		{"dom_974_vs_972", "97400", "97200", false},
		// Empty input zip → guard does not engage; result returned.
		{"no_input_anchor", "", "42050", true},
		// Empty BAN postcode → guard does not engage (nothing to compare).
		{"no_ban_postcode", "78550", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := GeocodeResult{
				Lat:      45.5,
				Lon:      4.7,
				CityCode: tc.banPC, // keep dept-coherent so validateCoherence doesn't pre-empt
				PostCode: tc.banPC,
				Score:    0.9,
				Source:   "ban",
			}
			cache := memcache.New()
			stub := &stubGeocoder{res: res}
			cached := NewCachedGeocoder(stub, cache, 0)
			q := GeocodeQuery{Address: "rue test " + tc.name, Zip: tc.inputZip}

			got, err := cached.Geocode(context.Background(), q)
			if tc.wantMatch {
				if err != nil {
					t.Fatalf("Geocode: unexpected error %v", err)
				}
				if got.PostCode != tc.banPC {
					t.Errorf("PostCode = %q, want %q", got.PostCode, tc.banPC)
				}
				if !cacheHas(t, cache, CacheKey(q)) {
					t.Errorf("expected cache write on matching dept")
				}
			} else {
				if !errors.Is(err, ErrDepartmentMismatch) {
					t.Fatalf("expected ErrDepartmentMismatch, got %v", err)
				}
				if got.PostCode != "" {
					t.Errorf("expected zero-value result on mismatch, got %+v", got)
				}
				if cacheHas(t, cache, CacheKey(q)) {
					t.Errorf("expected NO cache write on dept mismatch")
				}
			}

			// Re-call to confirm no memoization of rejected response.
			_, err2 := cached.Geocode(context.Background(), q)
			wantCalls := int32(1)
			if !tc.wantMatch {
				wantCalls = 2
				if !errors.Is(err2, ErrDepartmentMismatch) {
					t.Errorf("second Geocode: expected ErrDepartmentMismatch, got %v", err2)
				}
			}
			if got := stub.calls.Load(); got != wantCalls {
				t.Errorf("delegate calls = %d, want %d", got, wantCalls)
			}
		})
	}
}

// TestCachedGeocoder_ReadSideDeptGuard covers the read-side dept-coherence
// guard: even if a row was persisted before the write-side guard existed
// (or by a code path that geocoded with a polluted query — typically a
// lawyer-office address leaking into a property auction), a cached hit
// whose PostCode disagrees with the caller's input zip département must
// be treated as a MISS so the delegate gets a chance to re-resolve.
//
// The setup writes a known "poisoned" row directly via the kv backend
// (bypassing CachedGeocoder.Geocode so the write-side guard does not
// reject it), then issues a Geocode call with an input zip in a
// different département and verifies that:
//   - the delegate IS invoked (cache hit was rejected),
//   - the call ultimately returns the delegate's response (which the
//     normal Geocode path may itself reject via the input-vs-output
//     guard — exercised in the wantInputGuardReject sub-case).
func TestCachedGeocoder_ReadSideDeptGuard(t *testing.T) {
	cases := []struct {
		name              string
		inputZip          string
		cachedPostCode    string
		cachedCityCode    string
		delegateRes       GeocodeResult
		expectDelegateHit bool // true → guard fires → delegate called
		expectErr         error
	}{
		// Adverse: cached row has PostCode 62xxx but caller asks 92xxx.
		// Classic lawyer-office (92) leaked into a Pas-de-Calais (62)
		// property auction — guard MUST treat as miss.
		{
			name:              "lawyer_leak_92_into_62",
			inputZip:          "92100",
			cachedPostCode:    "62100",
			cachedCityCode:    "62100",
			delegateRes:       GeocodeResult{Lat: 48.0, Lon: 2.0, CityCode: "92012", PostCode: "92100", Source: "ban"},
			expectDelegateHit: true,
		},
		// Adverse: Bazainville drift (78 → 42).
		{
			name:              "bazainville_legacy_drift_78_42",
			inputZip:          "78550",
			cachedPostCode:    "42050",
			cachedCityCode:    "42050",
			delegateRes:       GeocodeResult{Lat: 48.8, Lon: 1.7, CityCode: "78050", PostCode: "78550", Source: "ban"},
			expectDelegateHit: true,
		},
		// DOM-TOM 3-digit semantics: cached 97200 (972 Martinique)
		// vs input 97400 (974 Réunion) → mismatch → MISS.
		{
			name:              "domtom_974_vs_972",
			inputZip:          "97400",
			cachedPostCode:    "97200",
			cachedCityCode:    "97200",
			delegateRes:       GeocodeResult{Lat: -21.0, Lon: 55.5, CityCode: "97411", PostCode: "97400", Source: "ban"},
			expectDelegateHit: true,
		},
		// Happy path: cached PostCode matches input zip → cache HIT.
		{
			name:              "same_dept_hit",
			inputZip:          "92100",
			cachedPostCode:    "92500",
			cachedCityCode:    "92500",
			delegateRes:       GeocodeResult{}, // unused
			expectDelegateHit: false,
		},
		// Paris arrondissement intra-75 → cache HIT.
		{
			name:              "paris_arr_intra_hit",
			inputZip:          "75011",
			cachedPostCode:    "75019",
			cachedCityCode:    "75019",
			delegateRes:       GeocodeResult{},
			expectDelegateHit: false,
		},
		// DOM-TOM same territory (974) → cache HIT.
		{
			name:              "domtom_974_intra_hit",
			inputZip:          "97400",
			cachedPostCode:    "97490",
			cachedCityCode:    "97490",
			delegateRes:       GeocodeResult{},
			expectDelegateHit: false,
		},
		// No input zip anchor → guard does not engage → cache HIT.
		{
			name:              "no_input_zip_anchor",
			inputZip:          "",
			cachedPostCode:    "62100",
			cachedCityCode:    "62100",
			delegateRes:       GeocodeResult{},
			expectDelegateHit: false,
		},
		// Cached row with empty PostCode → guard cannot compare → HIT.
		{
			name:              "empty_cached_postcode",
			inputZip:          "92100",
			cachedPostCode:    "",
			cachedCityCode:    "",
			delegateRes:       GeocodeResult{},
			expectDelegateHit: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache := memcache.New()
			stub := &stubGeocoder{res: tc.delegateRes}
			cached := NewCachedGeocoder(stub, cache, 0)

			q := GeocodeQuery{Address: "rue test " + tc.name, Zip: tc.inputZip}
			// Pre-populate the cache with a poisoned row at q's key,
			// bypassing the write-side guard so the read-side path is
			// exercised in isolation.
			poisoned := GeocodeResult{
				Lat:      45.5,
				Lon:      4.7,
				CityCode: tc.cachedCityCode,
				PostCode: tc.cachedPostCode,
				Score:    0.6,
				Label:    "poisoned " + tc.name,
				Source:   "ban",
			}
			b, err := json.Marshal(poisoned)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			exp := time.Now().Add(time.Hour)
			if err := cache.Set(context.Background(), kvcache.Entry{
				Key:       CacheKey(q),
				Value:     b,
				FetchedAt: time.Now(),
				ExpiresAt: &exp,
			}); err != nil {
				t.Fatalf("cache.Set: %v", err)
			}

			got, err := cached.Geocode(context.Background(), q)
			if tc.expectDelegateHit {
				if stub.calls.Load() != 1 {
					t.Fatalf("expected delegate to be called once (guard fired), got %d calls", stub.calls.Load())
				}
				// The returned value should be the delegate's response
				// (or an ErrDepartmentMismatch if the delegate's
				// PostCode happens to mismatch input — not our case
				// here as fixtures align delegate.PostCode to inputZip).
				if err != nil {
					t.Fatalf("unexpected error after read-guard miss: %v", err)
				}
				if got.PostCode != tc.delegateRes.PostCode {
					t.Errorf("PostCode after refetch = %q, want %q (delegate)", got.PostCode, tc.delegateRes.PostCode)
				}
			} else {
				if stub.calls.Load() != 0 {
					t.Fatalf("expected cache HIT (delegate untouched), got %d calls", stub.calls.Load())
				}
				if err != nil {
					t.Fatalf("Geocode on cache hit: %v", err)
				}
				if got.PostCode != tc.cachedPostCode {
					t.Errorf("PostCode on hit = %q, want cached %q", got.PostCode, tc.cachedPostCode)
				}
			}
		})
	}
}

// TestCachedGeocoder_ReadSideDeptGuard_EmptyCacheIsMiss is the trivial
// baseline: an empty cache must still behave as a MISS regardless of the
// guard (the guard is a no-op when no row exists for the key).
func TestCachedGeocoder_ReadSideDeptGuard_EmptyCacheIsMiss(t *testing.T) {
	cache := memcache.New()
	stub := &stubGeocoder{res: GeocodeResult{Lat: 48, Lon: 2, CityCode: "75056", PostCode: "75011", Source: "ban"}}
	cached := NewCachedGeocoder(stub, cache, 0)
	q := GeocodeQuery{Address: "rue absente", Zip: "75011"}
	if _, err := cached.Geocode(context.Background(), q); err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	if stub.calls.Load() != 1 {
		t.Fatalf("expected exactly 1 delegate call on empty cache, got %d", stub.calls.Load())
	}
}

// TestCachedGeocoder_WriteGuard exercises the four documented cases of
// the bug-30989 write-time guard.
func TestCachedGeocoder_WriteGuard(t *testing.T) {
	cases := []struct {
		name      string
		res       GeocodeResult
		wantCache bool
	}{
		{
			name:      "coherent_forward",
			res:       GeocodeResult{Lat: 48.86, Lon: 2.37, CityCode: "75056", PostCode: "75011", Score: 0.9, Source: "ban"},
			wantCache: true,
		},
		{
			name:      "incoherent_forward",
			res:       GeocodeResult{Lat: 48.86, Lon: 2.37, CityCode: "75117", PostCode: "22680", Score: 0.9, Source: "ban"},
			wantCache: false,
		},
		{
			name:      "corsica_forward",
			res:       GeocodeResult{Lat: 41.9, Lon: 8.7, CityCode: "2A004", PostCode: "20000", Score: 0.9, Source: "ban"},
			wantCache: true,
		},
		{
			name:      "domtom_forward",
			res:       GeocodeResult{Lat: -21.0, Lon: 55.5, CityCode: "97411", PostCode: "97400", Score: 0.9, Source: "ban"},
			wantCache: true,
		},
		{
			name:      "empty_citycode",
			res:       GeocodeResult{Lat: 48.0, Lon: 2.0, CityCode: "", PostCode: "75011", Score: 0.9, Source: "ban"},
			wantCache: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache := memcache.New()
			stub := &stubGeocoder{res: tc.res}
			cached := NewCachedGeocoder(stub, cache, 0)
			q := GeocodeQuery{Address: "rue test " + tc.name, Zip: tc.res.PostCode}

			got, err := cached.Geocode(context.Background(), q)
			if err != nil {
				t.Fatalf("Geocode: %v", err)
			}
			// Caller MUST receive the result regardless of guard outcome.
			if got.CityCode != tc.res.CityCode || got.PostCode != tc.res.PostCode {
				t.Errorf("returned result mismatch: got %+v want %+v", got, tc.res)
			}

			has := cacheHas(t, cache, CacheKey(q))
			if has != tc.wantCache {
				t.Errorf("cache presence = %v, want %v", has, tc.wantCache)
			}

			// Re-call: if cache wrote, the delegate is hit once total.
			// If cache skipped, the delegate is hit twice (no memoization
			// of poisoned data).
			if _, err := cached.Geocode(context.Background(), q); err != nil {
				t.Fatalf("second Geocode: %v", err)
			}
			wantCalls := int32(1)
			if !tc.wantCache {
				wantCalls = 2
			}
			if got := stub.calls.Load(); got != wantCalls {
				t.Errorf("delegate calls = %d, want %d", got, wantCalls)
			}
		})
	}
}
