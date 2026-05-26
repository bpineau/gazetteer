package dvf

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bpineau/gazetteer"
	"github.com/bpineau/gazetteer/pkg/banx"
	"github.com/bpineau/gazetteer/pkg/circuit"
	"github.com/bpineau/gazetteer/pkg/httpx"
	"github.com/bpineau/gazetteer/pkg/kvcache/memcache"
)

// stubGeocoder returns a fixed result. Used by tests that don't care
// about geocoding mechanics.
type stubGeocoder struct {
	res banx.GeocodeResult
	err error
}

func (s stubGeocoder) Geocode(_ context.Context, _ banx.GeocodeQuery) (banx.GeocodeResult, error) {
	if s.err != nil {
		return banx.GeocodeResult{}, s.err
	}
	return s.res, nil
}

// loadFixtureMutations reads the DVF API envelope from testdata.
func loadFixtureMutations(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// withBaseURL swaps APIBaseURL for the duration of a test.
func withBaseURL(t *testing.T, u string) {
	t.Helper()
	prev := APIBaseURL
	APIBaseURL = u
	t.Cleanup(func() { APIBaseURL = prev })
}

// newHTTPClient builds an httpx client suitable for tests (no retries
// so failures surface fast).
func newHTTPClient(t *testing.T) *httpx.Client {
	t.Helper()
	c, err := httpx.New(httpx.Options{})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestSource_NameVersion(t *testing.T) {
	hc := newHTTPClient(t)
	s := NewSource(Options{HTTP: hc, Geocoder: stubGeocoder{}})
	if s.Name() != Name {
		t.Errorf("Name() = %q, want %q", s.Name(), Name)
	}
	if s.Version() != sourceVersion {
		t.Errorf("Version() = %d, want %d", s.Version(), sourceVersion)
	}
}

func TestSource_HappyPath_Commune(t *testing.T) {
	body := loadFixtureMutations(t, "dvfapi_mutations_75107_AD.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/75107/000AD") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/mutations")

	hc := newHTTPClient(t)
	s := NewSource(Options{
		HTTP:     hc,
		Geocoder: stubGeocoder{res: banx.GeocodeResult{Lat: 48.85, Lon: 2.31, CityCode: "75107"}},
	})
	// Pre-seed sections so the cadastre primer doesn't hit the network.
	if err := s.Sections().PrimeFromList(context.Background(), "75107", []string{"000AD"}); err != nil {
		t.Fatalf("PrimeFromList: %v", err)
	}

	l := gazetteer.Listing{
		Address:      "1 rue de Test",
		City:         "Paris 7e",
		Zip:          "75007",
		PropertyType: gazetteer.PropertyApartment,
	}
	data, err := s.Query(context.Background(), l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res, ok := data.(*Result)
	if !ok {
		t.Fatalf("Query returned %T, want *Result", data)
	}
	if res.IsEmpty() {
		t.Error("IsEmpty() = true, want false")
	}
	if res.SampleSize < 30 {
		t.Errorf("SampleSize = %d, want ≥ 30 (Paris 7e AD)", res.SampleSize)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if res.ValueEURPerM2Cents == nil {
		t.Fatal("nil ValueEURPerM2Cents")
	}
	if *res.ValueEURPerM2Cents < 1_000_000 { // 10 000 €/m² in cents
		t.Errorf("Paris 7e price/m² too low: %d cents", *res.ValueEURPerM2Cents)
	}
	if res.Evidence.LevelUsed != "commune" {
		t.Errorf("Evidence.LevelUsed = %q, want commune", res.Evidence.LevelUsed)
	}
	if res.Evidence.PrimaryINSEE != "75107" {
		t.Errorf("Evidence.PrimaryINSEE = %q, want 75107", res.Evidence.PrimaryINSEE)
	}
	if res.Evidence.TypeLocalFilter != "Appartement" {
		t.Errorf("Evidence.TypeLocalFilter = %q, want Appartement", res.Evidence.TypeLocalFilter)
	}
	if res.Evidence.INSEEResolutionSource != "ban_forward" {
		t.Errorf("Evidence.INSEEResolutionSource = %q, want ban_forward", res.Evidence.INSEEResolutionSource)
	}
}

func TestSource_UnsupportedPropertyType(t *testing.T) {
	hc := newHTTPClient(t)
	s := NewSource(Options{HTTP: hc, Geocoder: stubGeocoder{}})
	l := gazetteer.Listing{
		Address:      "x",
		City:         "Paris",
		PropertyType: gazetteer.PropertyLand,
	}
	_, err := s.Query(context.Background(), l)
	if !errors.Is(err, gazetteer.ErrUnsupportedPropertyType) {
		t.Errorf("Query(land) = %v, want wraps ErrUnsupportedPropertyType", err)
	}
}

func TestSource_InsufficientInputs_NoGeocoder(t *testing.T) {
	hc := newHTTPClient(t)
	s := NewSource(Options{HTTP: hc})
	l := gazetteer.Listing{
		Address:      "1 rue test",
		City:         "Paris",
		PropertyType: gazetteer.PropertyApartment,
	}
	_, err := s.Query(context.Background(), l)
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(no geocoder) = %v, want wraps ErrInsufficientInputs", err)
	}
}

func TestSource_InsufficientInputs_NoAddress(t *testing.T) {
	hc := newHTTPClient(t)
	s := NewSource(Options{HTTP: hc, Geocoder: stubGeocoder{}})
	l := gazetteer.Listing{
		PropertyType: gazetteer.PropertyApartment,
	}
	_, err := s.Query(context.Background(), l)
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(empty) = %v, want wraps ErrInsufficientInputs", err)
	}
}

func TestSource_INSEEListingShortCircuit(t *testing.T) {
	body := loadFixtureMutations(t, "dvfapi_mutations_75107_AD.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/75107/000AD") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/mutations")

	hc := newHTTPClient(t)
	s := NewSource(Options{HTTP: hc}) // no Geocoder
	if err := s.Sections().PrimeFromList(context.Background(), "75107", []string{"000AD"}); err != nil {
		t.Fatalf("PrimeFromList: %v", err)
	}

	l := gazetteer.Listing{
		INSEE:        "75107",
		PropertyType: gazetteer.PropertyApartment,
	}
	data, err := s.Query(context.Background(), l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if res.Evidence.INSEEResolutionSource != "listing" {
		t.Errorf("Evidence.INSEEResolutionSource = %q, want listing", res.Evidence.INSEEResolutionSource)
	}
}

func TestSource_AddressRadius(t *testing.T) {
	// Synthesize a tight 16-mutation pool clustered around (lat, lon)
	// so the address_radius tier wins.
	mLat := 48.8580
	mLon := 2.3050
	const insee = "75107"
	const sec = "000AD"
	resp := MutationsResponse{}
	for i := range 16 {
		s := 50.0
		v := 12000.0 * s
		lat := mLat + float64(i)*0.00003
		lon := mLon + float64(i)*0.00003
		resp.Data = append(resp.Data, Mutation{
			IDMutation:        "ar-" + strings.Repeat("x", i+1),
			DateMutation:      "2024-06-15",
			NatureMutation:    NatureMutationVente,
			ValeurFonciere:    &v,
			TypeLocal:         "Appartement",
			SurfaceReelleBati: &s,
			CodeCommune:       insee,
			IDParcelle:        insee + "000AD000" + string(rune('A'+i)),
			SectionPrefixe:    sec,
			Latitude:          &lat,
			Longitude:         &lon,
		})
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/"+insee+"/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/mutations")

	hc := newHTTPClient(t)
	src := NewSource(Options{
		HTTP:     hc,
		Geocoder: stubGeocoder{res: banx.GeocodeResult{Lat: mLat, Lon: mLon, CityCode: insee}},
	})
	if err := src.Sections().PrimeFromList(context.Background(), insee, []string{sec}); err != nil {
		t.Fatalf("PrimeFromList: %v", err)
	}

	lat := mLat
	lon := mLon
	surf := 50.0
	l := gazetteer.Listing{
		Address:      "10 rue Test",
		City:         "Paris 7e",
		Zip:          "75007",
		PropertyType: gazetteer.PropertyApartment,
		Lat:          &lat,
		Lon:          &lon,
		SurfaceM2:    &surf,
	}
	data, err := src.Query(context.Background(), l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if res.Evidence.LevelUsed != "address_radius" {
		t.Errorf("LevelUsed = %q, want address_radius", res.Evidence.LevelUsed)
	}
	if res.Evidence.RadiusM != DVFAddressRadiusMeters {
		t.Errorf("RadiusM = %v, want %v", res.Evidence.RadiusM, DVFAddressRadiusMeters)
	}
	if res.Evidence.AuctionLat == nil || *res.Evidence.AuctionLat != mLat {
		t.Errorf("AuctionLat = %v, want %v", res.Evidence.AuctionLat, mLat)
	}
	if res.Evidence.NUniqueParcelles == 0 {
		t.Error("NUniqueParcelles = 0, want > 0")
	}
}

func TestSource_CircuitTripped_ShortCircuits(t *testing.T) {
	hc := newHTTPClient(t)
	tripped := &atomic.Bool{}
	s := NewSource(Options{
		HTTP:           hc,
		Geocoder:       stubGeocoder{res: banx.GeocodeResult{Lat: 48.85, Lon: 2.31, CityCode: "75107"}},
		CircuitTripped: tripped,
	})
	tripped.Store(true)

	l := gazetteer.Listing{
		Address:      "1 rue test",
		PropertyType: gazetteer.PropertyApartment,
	}
	_, err := s.Query(context.Background(), l)
	if !errors.Is(err, ErrCircuitTripped) {
		t.Errorf("Query(circuit tripped) = %v, want ErrCircuitTripped", err)
	}
}

func TestSource_CustomSectionCache(t *testing.T) {
	body := loadFixtureMutations(t, "dvfapi_mutations_75107_AD.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/75107/000AD") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/mutations")

	cache := memcache.New()
	hc := newHTTPClient(t)
	s := NewSource(Options{
		HTTP:         hc,
		Geocoder:     stubGeocoder{res: banx.GeocodeResult{CityCode: "75107"}},
		SectionCache: cache,
	})
	if err := s.Sections().PrimeFromList(context.Background(), "75107", []string{"000AD"}); err != nil {
		t.Fatalf("PrimeFromList: %v", err)
	}
	// The same key should be visible through the custom cache.
	got, err := cache.Get(context.Background(), CacheKeyPrefix+"75107")
	if err != nil {
		t.Fatalf("cache.Get: %v", err)
	}
	if len(got.Value) == 0 {
		t.Error("cache row has empty value")
	}
}

func TestSource_TransportCircuit_TripsOnDeadline(t *testing.T) {
	// Drive the API client through an httptest server that hangs;
	// per-call deadline timeouts tick the TransportCircuit and flip
	// the shared atomic after the threshold. 5xx responses are NOT
	// transport-level and (correctly) do not advance the streak.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/mutations")

	prev := APICallTimeout
	APICallTimeout = 50 * time.Millisecond
	t.Cleanup(func() { APICallTimeout = prev })

	hc, err := httpx.New(httpx.Options{MaxRetries: -1})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	t.Cleanup(func() { _ = hc.Close() })
	tripped := &atomic.Bool{}
	tc := circuit.NewTransportCircuit(Name, 2, tripped, nil)
	api := NewAPI(hc, tc)

	for i := 0; i < 3; i++ {
		_, _ = api.GetMutations(context.Background(), "75107", "000AD")
	}
	if !tripped.Load() {
		t.Error("expected circuit tripped after 3 consecutive deadlines, got false")
	}
}

func TestSource_Registry(t *testing.T) {
	factory := gazetteer.Lookup(Name)
	if factory == nil {
		t.Fatal("gazetteer.Lookup(dvf) = nil, want registered factory")
	}
	v := factory()
	if _, ok := v.(*Result); !ok {
		t.Errorf("factory() = %T, want *Result", v)
	}
}

func TestSource_From(t *testing.T) {
	d := gazetteer.Dossier{
		Results: map[string]gazetteer.Result{
			Name: {
				Name: Name,
				Data: &Result{SampleSize: 5, Confidence: ConfidenceLow},
			},
		},
	}
	r, ok := From(d)
	if !ok {
		t.Fatal("From() = false, want true")
	}
	if r.SampleSize != 5 {
		t.Errorf("SampleSize = %d, want 5", r.SampleSize)
	}
}

// TestEnricher_Enrich_HappyPath is a smoke-test for the atomic Query
// helper that mirrors the source-test paths but at the package level.
func TestQueryHelper_Smoke(t *testing.T) {
	body := loadFixtureMutations(t, "dvfapi_mutations_75107_AD.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/75107/000AD") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/mutations")

	hc := newHTTPClient(t)
	cache := memcache.New()
	// Pre-prime sections directly into cache.
	d := NewSectionDiscoverer(cache, nil)
	if err := d.PrimeFromList(context.Background(), "75107", []string{"000AD"}); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	res, err := Query(context.Background(), Options{
		HTTP:         hc,
		Geocoder:     stubGeocoder{res: banx.GeocodeResult{CityCode: "75107"}},
		SectionCache: cache,
	}, gazetteer.Listing{
		INSEE:        "75107",
		PropertyType: gazetteer.PropertyApartment,
		AsOf:         time.Now(),
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() {
		t.Error("Result IsEmpty=true, want false")
	}
}
