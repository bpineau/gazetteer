package georisques

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer"
	"github.com/bpineau/gazetteer/pkg/banx"
)

// stubGeocoder returns a fixed lat/lon. Used by tests that don't care
// about geocoding mechanics.
type stubGeocoder struct {
	lat, lon float64
	err      error
}

func (s stubGeocoder) Geocode(_ context.Context, _ banx.GeocodeQuery) (banx.GeocodeResult, error) {
	if s.err != nil {
		return banx.GeocodeResult{}, s.err
	}
	return banx.GeocodeResult{Lat: s.lat, Lon: s.lon}, nil
}

// newListingParis11 returns a Listing for "13 Rue Alphonse Baudin, 75011 Paris".
func newListingParis11() gazetteer.Listing {
	return gazetteer.Listing{
		Address:      "13 Rue Alphonse Baudin 75011 Paris",
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

// withBaseURL swaps the package-level BaseURL for the duration of a
// test. Restores it on cleanup.
func withBaseURL(t *testing.T, u string) {
	t.Helper()
	prev := BaseURL
	BaseURL = u
	t.Cleanup(func() { BaseURL = prev })
}

func TestSource_NameVersion(t *testing.T) {
	s := NewSource(Options{})
	if s.Name() != Name {
		t.Errorf("Name() = %q, want %q", s.Name(), Name)
	}
	if s.Version() != sourceVersion {
		t.Errorf("Version() = %d, want %d", s.Version(), sourceVersion)
	}
}

func TestSource_HappyPath(t *testing.T) {
	body := mustReadFixture(t, "paris11.json")
	srv := newStubServer(t, http.StatusOK, body)
	withBaseURL(t, srv.URL)

	s := NewSource(Options{
		Geocoder: stubGeocoder{lat: 48.860874, lon: 2.370245},
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
	if res.Address == nil || res.Address.Libelle == "" {
		t.Error("Address.Libelle is missing")
	}
	if res.Commune == nil || res.Commune.Insee != "75056" {
		t.Errorf("Commune.Insee = %+v, want 75056", res.Commune)
	}
	if len(res.Naturels) != 12 {
		t.Errorf("len(Naturels) = %d, want 12", len(res.Naturels))
	}
	if len(res.Technos) != 6 {
		t.Errorf("len(Technos) = %d, want 6", len(res.Technos))
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if res.LevelUsed != LevelAddress {
		t.Errorf("LevelUsed = %q, want %q", res.LevelUsed, LevelAddress)
	}

	// Evidence sidecar must be populated.
	ev := res.Evidence
	if ev.Lat != 48.860874 || ev.Lon != 2.370245 {
		t.Errorf("Evidence Lat/Lon = (%v,%v), want (48.860874, 2.370245)", ev.Lat, ev.Lon)
	}
	if !strings.Contains(ev.URL, "latlon=2.370245,48.860874") {
		t.Errorf("Evidence.URL missing lon,lat ordering: %s", ev.URL)
	}
	if ev.LevelUsed != LevelAddress {
		t.Errorf("Evidence.LevelUsed = %q, want %q", ev.LevelUsed, LevelAddress)
	}

	// Cross-check the URL the upstream actually saw.
	if !strings.Contains(srv.lastURL, "latlon=2.370245,48.860874") {
		t.Errorf("upstream URL missing lon,lat ordering: %s", srv.lastURL)
	}
}

func TestSource_NoAddress_Insufficient(t *testing.T) {
	s := NewSource(Options{Geocoder: stubGeocoder{lat: 48.86, lon: 2.37}})
	_, err := s.Query(context.Background(), gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(empty) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_GeocodeFailsFallsThrough(t *testing.T) {
	s := NewSource(Options{Geocoder: stubGeocoder{err: errors.New("no address")}})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(geocode err) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_GeocodeReturnsZero_Insufficient(t *testing.T) {
	// Geocoder succeeded but returned (0,0) — refuse to send the
	// request because Georisques would return an empty rapport.
	s := NewSource(Options{Geocoder: stubGeocoder{lat: 0, lon: 0}})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(zero coords) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_UsesListingCoordsWhenSet(t *testing.T) {
	body := mustReadFixture(t, "paris11.json")
	srv := newStubServer(t, http.StatusOK, body)
	withBaseURL(t, srv.URL)

	lat, lon := 48.86, 2.37
	s := NewSource(Options{
		// Geocoder would return different coords if consulted — verify
		// we use the listing-provided ones.
		Geocoder: stubGeocoder{lat: 99, lon: 99},
	})
	l := newListingParis11()
	l.Lat = &lat
	l.Lon = &lon
	data, err := s.Query(context.Background(), l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if res.Evidence.Lat != lat || res.Evidence.Lon != lon {
		t.Errorf("Evidence Lat/Lon = (%v,%v), want (%v,%v) — listing coords should win", res.Evidence.Lat, res.Evidence.Lon, lat, lon)
	}
}

func TestSource_Upstream5xx_Transient(t *testing.T) {
	srv := newStubServer(t, http.StatusInternalServerError, []byte("server error"))
	withBaseURL(t, srv.URL)

	s := NewSource(Options{Geocoder: stubGeocoder{lat: 48.86, lon: 2.37}})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("Query(5xx) = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestSource_Upstream4xx_Permanent(t *testing.T) {
	srv := newStubServer(t, http.StatusBadRequest, []byte("bad request"))
	withBaseURL(t, srv.URL)

	s := NewSource(Options{Geocoder: stubGeocoder{lat: 48.86, lon: 2.37}})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrUpstreamPermanent) {
		t.Errorf("Query(400) = %v, want ErrUpstreamPermanent", err)
	}
}

func TestSource_404TreatedAsEmpty(t *testing.T) {
	srv := newStubServer(t, http.StatusNotFound, nil)
	withBaseURL(t, srv.URL)

	s := NewSource(Options{Geocoder: stubGeocoder{lat: 48.86, lon: 2.37}})
	data, err := s.Query(context.Background(), newListingParis11())
	if err != nil {
		t.Fatalf("Query(404) = %v, want nil error (treated as empty body)", err)
	}
	res := data.(*Result)
	// `{}` parses → zero report → Confidence "medium" (0 risks present)
	// and no Address. The Source still returns a non-nil Result.
	if res.Address != nil {
		t.Errorf("expected nil Address on 404→empty, got %+v", res.Address)
	}
}

func TestSource_GarbageBody_Transient(t *testing.T) {
	srv := newStubServer(t, http.StatusOK, []byte("not json"))
	withBaseURL(t, srv.URL)

	s := NewSource(Options{Geocoder: stubGeocoder{lat: 48.86, lon: 2.37}})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("Query(garbage) = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestSource_EmptyBodyYieldsMediumConfidence(t *testing.T) {
	srv := newStubServer(t, http.StatusOK, []byte("{}"))
	withBaseURL(t, srv.URL)

	s := NewSource(Options{Geocoder: stubGeocoder{lat: 48.86, lon: 2.37}})
	data, err := s.Query(context.Background(), newListingParis11())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want %q for empty rapport", res.Confidence, ConfidenceMedium)
	}
	if res.LevelUsed != LevelCommune {
		t.Errorf("LevelUsed = %q, want %q on empty Adresse", res.LevelUsed, LevelCommune)
	}
}

func TestQuery_TypedHelper(t *testing.T) {
	body := mustReadFixture(t, "paris11.json")
	srv := newStubServer(t, http.StatusOK, body)
	withBaseURL(t, srv.URL)

	res, err := Query(context.Background(), Options{Geocoder: stubGeocoder{lat: 48.86, lon: 2.37}}, newListingParis11())
	if err != nil {
		t.Fatalf("Query helper: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Errorf("Query helper returned empty result")
	}
}

func TestFrom_RoundtripFromDossier(t *testing.T) {
	// Lightweight check: just ensure Register has wired the factory so
	// gazetteer.Get can build the typed Result. Construction-level test;
	// the full Dossier roundtrip is covered in gazetteer/gazetteer_test.
	factory := gazetteer.Lookup(Name)
	if factory == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil, expected init() to register", Name)
	}
	v := factory()
	if _, ok := v.(*Result); !ok {
		t.Errorf("factory returned %T, want *Result", v)
	}
}
