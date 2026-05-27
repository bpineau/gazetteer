package bdnb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
)

// mustReadFixture reads a JSON fixture from the testdata/ directory.
func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// stubGeocoder returns a fixed result. Used by tests that don't care
// about geocoding mechanics.
type stubGeocoder struct {
	cityCode string
	score    float64
	err      error
}

func (s stubGeocoder) Geocode(_ context.Context, _ banx.GeocodeQuery) (banx.GeocodeResult, error) {
	if s.err != nil {
		return banx.GeocodeResult{}, s.err
	}
	score := s.score
	if score == 0 {
		// 0 is the "unknown, trust the result" mode in INSEEResolver.
		// Set a high score to remove any ambiguity in tests that don't
		// care.
		score = 0.99
	}
	return banx.GeocodeResult{CityCode: s.cityCode, Score: score}, nil
}

// newListingParis11 returns a Listing for "3 Impasse de Mont Louis, 75011 Paris".
func newListingParis11() gazetteer.Listing {
	return gazetteer.Listing{
		Address:      "3 Impasse de Mont Louis 75011 Paris",
		City:         "Paris",
		Zip:          "75011",
		PropertyType: gazetteer.PropertyApartment,
	}
}

// stubServer wraps httptest.Server with last-URL capture.
type stubServer struct {
	*httptest.Server
	lastURL string
}

func newStubServer(t *testing.T, status int, body []byte) *stubServer {
	t.Helper()
	ss := &stubServer{}
	ss.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ss.lastURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(ss.Close)
	return ss
}

func TestSource_NameVersion(t *testing.T) {
	t.Parallel()

	s := NewSource(Options{})
	if s.Name() != Name {
		t.Errorf("Name() = %q, want %q", s.Name(), Name)
	}
	if s.Version() != sourceVersion {
		t.Errorf("Version() = %d, want %d", s.Version(), sourceVersion)
	}
}

func TestSource_HappyPath(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "list_paris11.json")
	srv := newStubServer(t, http.StatusOK, body)
	s := NewSource(Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{cityCode: "75111"},
	})
	data, err := s.Query(context.Background(), newListingParis11())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res, ok := data.(*Result)
	if !ok {
		t.Fatalf("Query returned %T, want *Result", data)
	}
	if res.IsEmpty() {
		t.Error("IsEmpty() = true, want false on happy path")
	}
	if res.SampleSize != 1 {
		t.Errorf("SampleSize = %d, want 1", res.SampleSize)
	}
	if res.Identity == nil || res.Identity.BatimentGroupeID == "" {
		t.Error("Identity is missing")
	}
	if res.Building == nil {
		t.Error("Building is missing")
	}
	if res.DPE == nil {
		t.Error("DPE is missing")
	}
	if res.Risks == nil {
		t.Error("Risks is missing")
	}
	if res.Confidence == "" {
		t.Error("Confidence is empty")
	}

	// Evidence sidecar must be populated.
	ev := res.Evidence
	if ev.MatchStrategy != MatchByAddressILike {
		t.Errorf("Evidence.MatchStrategy = %q, want %q", ev.MatchStrategy, MatchByAddressILike)
	}
	if ev.INSEE != "75111" {
		t.Errorf("Evidence.INSEE = %q, want 75111", ev.INSEE)
	}
	if ev.INSEEResolutionSource != "ban_forward" {
		t.Errorf("Evidence.INSEEResolutionSource = %q, want ban_forward", ev.INSEEResolutionSource)
	}
	if ev.AddressPattern == "" {
		t.Error("Evidence.AddressPattern is empty")
	}
	if ev.RawCount != 2 {
		t.Errorf("Evidence.RawCount = %d, want 2 (fixture has 2 rows)", ev.RawCount)
	}
	if ev.URL == "" {
		t.Error("Evidence.URL is empty")
	}

	// Confirm the URL hit by the upstream stub embeds the INSEE filter
	// and the ilike on libelle_adr_principale_ban.
	u, err := url.Parse(srv.lastURL)
	if err != nil {
		t.Fatalf("parse captured URL: %v", err)
	}
	if got := u.Query().Get("code_commune_insee"); got != "eq.75111" {
		t.Errorf("code_commune_insee = %q, want eq.75111", got)
	}
	if got := u.Query().Get("libelle_adr_principale_ban"); !strings.HasPrefix(got, "ilike.") {
		t.Errorf("libelle_adr_principale_ban = %q, want ilike.* prefix", got)
	}
}

func TestSource_EmptyResponse(t *testing.T) {
	t.Parallel()

	srv := newStubServer(t, http.StatusOK, []byte(`[]`))
	s := NewSource(Options{BaseURL: srv.URL, Geocoder: stubGeocoder{cityCode: "75111"}})
	data, err := s.Query(context.Background(), newListingParis11())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res, ok := data.(*Result)
	if !ok {
		t.Fatalf("Query returned %T, want *Result", data)
	}
	if !res.IsEmpty() {
		t.Error("IsEmpty() = false, want true on empty response")
	}
	if res.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low", res.Confidence)
	}
	if res.SkipReason != SkipReasonNoMatch {
		t.Errorf("SkipReason = %q, want %q", res.SkipReason, SkipReasonNoMatch)
	}
	if !res.Skipped {
		t.Error("Skipped = false, want true")
	}
	if res.Identity != nil || res.Building != nil || res.DPE != nil {
		t.Errorf("expected nil sub-blobs on empty result, got %+v", res)
	}
	// Evidence still populated.
	if res.Evidence.PickedIndex != -1 {
		t.Errorf("Evidence.PickedIndex = %d, want -1 (sentinel)", res.Evidence.PickedIndex)
	}
}

func TestSource_HTTP5xx_ErrUpstreamUnavailable(t *testing.T) {
	t.Parallel()

	srv := newStubServer(t, http.StatusServiceUnavailable, []byte(`{"error":"down"}`))
	s := NewSource(Options{BaseURL: srv.URL, Geocoder: stubGeocoder{cityCode: "75111"}})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("Query = %v, want wrapping ErrUpstreamUnavailable", err)
	}
}

func TestSource_HTTP4xx_ErrUpstreamPermanent(t *testing.T) {
	t.Parallel()

	srv := newStubServer(t, http.StatusForbidden, []byte(`{"error":"forbidden"}`))
	s := NewSource(Options{BaseURL: srv.URL, Geocoder: stubGeocoder{cityCode: "75111"}})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrUpstreamPermanent) {
		t.Errorf("Query = %v, want wrapping ErrUpstreamPermanent", err)
	}
}

func TestSource_InsufficientInputs_NoAddress(t *testing.T) {
	t.Parallel()

	s := NewSource(Options{Geocoder: stubGeocoder{cityCode: "75111"}})
	_, err := s.Query(context.Background(), gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(empty listing) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_InsufficientInputs_NoGeocoderNoINSEE(t *testing.T) {
	t.Parallel()

	// Listing has address but no INSEE; Source has no Geocoder.
	s := NewSource(Options{})
	_, err := s.Query(context.Background(), gazetteer.Listing{Address: "82 Rue X"})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(no geocoder, no INSEE) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_InsufficientInputs_GeocoderFails(t *testing.T) {
	t.Parallel()

	s := NewSource(Options{
		Geocoder: stubGeocoder{err: errors.New("no commune")},
	})
	_, err := s.Query(context.Background(), gazetteer.Listing{
		Address: "82 Rue de la Roquette",
	})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(geocode fails) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_InsufficientInputs_EmptyPattern(t *testing.T) {
	t.Parallel()

	// Address is zip-only — fraddr yields no street tokens, so the
	// ilike pattern is empty.
	srv := newStubServer(t, http.StatusOK, []byte(`[]`))
	s := NewSource(Options{BaseURL: srv.URL, Geocoder: stubGeocoder{cityCode: "75111"}})
	_, err := s.Query(context.Background(), gazetteer.Listing{
		Address: "75011 Paris",
		Zip:     "75011",
	})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(zip-only address) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_ListingINSEEShortCircuit(t *testing.T) {
	t.Parallel()

	// Listing carries its own INSEE — Source must use it verbatim and
	// not call the Geocoder.
	body := mustReadFixture(t, "list_paris11.json")
	srv := newStubServer(t, http.StatusOK, body)
	called := false
	gc := stubGeocoder{cityCode: "WRONG"}
	_ = called

	s := NewSource(Options{
		BaseURL:  srv.URL,
		Geocoder: gcCallTrack{stubGeocoder: gc, called: &called},
	})
	listing := newListingParis11()
	listing.INSEE = "75111"
	data, err := s.Query(context.Background(), listing)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if res.Evidence.INSEEResolutionSource != "listing" {
		t.Errorf("INSEEResolutionSource = %q, want listing", res.Evidence.INSEEResolutionSource)
	}
	if called {
		t.Error("Geocoder was called even though Listing.INSEE was set")
	}
}

type gcCallTrack struct {
	stubGeocoder
	called *bool
}

func (g gcCallTrack) Geocode(ctx context.Context, q banx.GeocodeQuery) (banx.GeocodeResult, error) {
	*g.called = true
	return g.stubGeocoder.Geocode(ctx, q)
}

func TestSource_PicksRowMatchingStreetNumber(t *testing.T) {
	t.Parallel()

	// Synthetic body with three rows differing only in the number.
	// The listing is at "9" so we expect row index 1 to be picked.
	body := []byte(`[
        {"batiment_groupe_id":"a","cle_interop_adr_principale_ban":"X_8","libelle_adr_principale_ban":"8 Rue Aubert 93200 Saint-Denis","code_commune_insee":"93066"},
        {"batiment_groupe_id":"b","cle_interop_adr_principale_ban":"X_9","libelle_adr_principale_ban":"9 Rue Aubert 93200 Saint-Denis","code_commune_insee":"93066"},
        {"batiment_groupe_id":"c","cle_interop_adr_principale_ban":"X_10","libelle_adr_principale_ban":"10 Rue Aubert 93200 Saint-Denis","code_commune_insee":"93066"}
    ]`)
	srv := newStubServer(t, http.StatusOK, body)
	s := NewSource(Options{BaseURL: srv.URL, Geocoder: stubGeocoder{cityCode: "93066"}})
	data, err := s.Query(context.Background(), gazetteer.Listing{
		Address: "9, rue Aubert 93200 Saint-Denis",
		Zip:     "93200",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if res.Identity == nil || res.Identity.BatimentGroupeID != "b" {
		t.Errorf("picked row id = %v, want b", res.Identity)
	}
	if res.Evidence.PickedIndex != 1 {
		t.Errorf("Evidence.PickedIndex = %d, want 1", res.Evidence.PickedIndex)
	}
}

func TestSource_FetcherTransportError(t *testing.T) {
	t.Parallel()

	// Closed server → connection refused → transport error wraps as
	// ErrUpstreamUnavailable.
	srv := newStubServer(t, http.StatusOK, []byte(`[]`))
	srv.Close()
	s := NewSource(Options{BaseURL: srv.URL, Geocoder: stubGeocoder{cityCode: "75111"}})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("Query(closed server) = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestSource_ParseFailureMapsToUpstreamUnavailable(t *testing.T) {
	t.Parallel()

	srv := newStubServer(t, http.StatusOK, []byte(`not json`))
	s := NewSource(Options{BaseURL: srv.URL, Geocoder: stubGeocoder{cityCode: "75111"}})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("Query(garbage body) = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestQueryAtomicHelper(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "list_paris11.json")
	srv := newStubServer(t, http.StatusOK, body)
	res, err := Query(context.Background(), Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{cityCode: "75111"},
	}, newListingParis11())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("Query atomic = %+v, want non-empty Result", res)
	}
}

func TestSource_RegistryRoundtrip(t *testing.T) {
	t.Parallel()

	// Confirm the init() registration is in place: Lookup(Name) must
	// return a factory producing *Result.
	factory := gazetteer.Lookup(Name)
	if factory == nil {
		t.Fatal("gazetteer.Lookup(bdnb) = nil, want a factory")
	}
	val := factory()
	if _, ok := val.(*Result); !ok {
		t.Errorf("factory() = %T, want *Result", val)
	}
}

func TestFrom_Dossier(t *testing.T) {
	t.Parallel()

	res := &Result{Confidence: ConfidenceHigh, SampleSize: 1}
	d := gazetteer.Dossier{
		Results: map[string]gazetteer.Result{
			Name: {
				Name:   Name,
				Status: gazetteer.StatusOK,
				Data:   res,
			},
		},
	}
	got, ok := gazetteer.Get[*Result](d, Name)
	if !ok || got != res {
		t.Errorf("From(d) = (%v, %v), want (%v, true)", got, ok, res)
	}
}

func TestFrom_DossierMissing(t *testing.T) {
	t.Parallel()

	d := gazetteer.Dossier{Results: map[string]gazetteer.Result{}}
	got, ok := gazetteer.Get[*Result](d, Name)
	if ok || got != nil {
		t.Errorf("From(empty d) = (%v, %v), want (nil, false)", got, ok)
	}
}

func TestResult_JSONShape(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "list_paris11.json")
	srv := newStubServer(t, http.StatusOK, body)
	res, err := Query(context.Background(), Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{cityCode: "75111"},
	}, newListingParis11())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(raw)
	for _, want := range []string{
		`"confidence":`, `"sample_size":`, `"identity":`, `"building":`,
		`"dpe":`, `"risks":`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON missing %q in: %s", want, s)
		}
	}
}
