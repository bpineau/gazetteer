package locservice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bpineau/gazetteer"
	"github.com/bpineau/gazetteer/pkg/banx"
)

// stubGeocoder returns a fixed CityCode. Used by tests that don't care
// about geocoding mechanics.
type stubGeocoder struct {
	cityCode string
	err      error
}

func (s stubGeocoder) Geocode(_ context.Context, _ banx.GeocodeQuery) (banx.GeocodeResult, error) {
	if s.err != nil {
		return banx.GeocodeResult{}, s.err
	}
	return banx.GeocodeResult{CityCode: s.cityCode}, nil
}

// newListingParis7 returns a Listing for "Paris 7e arrondissement".
func newListingParis7() gazetteer.Listing {
	return gazetteer.Listing{
		Address:      "1 rue de Test",
		City:         "Paris 7",
		Zip:          "75007",
		PropertyType: gazetteer.PropertyApartment,
	}
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

func TestSource_HappyPath_AllTypes(t *testing.T) {
	body := mustReadFixture(t, "paris7_all.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/tensiometre-75107.html") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=ISO-8859-1")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/tensiometre")

	s := NewSource(Options{Geocoder: stubGeocoder{cityCode: "75107"}})
	data, err := s.Query(context.Background(), newListingParis7())
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
	if res.TensionScore == nil || *res.TensionScore != 8 {
		t.Errorf("TensionScore = %v, want 8", res.TensionScore)
	}
	if res.TensionLabel != string(LabelTresTendu) {
		t.Errorf("TensionLabel = %q, want %q", res.TensionLabel, LabelTresTendu)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if res.Evidence.INSEE != "75107" {
		t.Errorf("Evidence.INSEE = %q, want 75107", res.Evidence.INSEE)
	}
	if res.Evidence.NoData {
		t.Error("Evidence.NoData = true, want false on happy path")
	}
	if !strings.Contains(res.Evidence.URL, "tensiometre-75107.html") {
		t.Errorf("Evidence.URL = %q, want substring tensiometre-75107.html", res.Evidence.URL)
	}
}

func TestSource_LogementMapping_T2(t *testing.T) {
	body := mustReadFixture(t, "troyes_t2.html")
	hits := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.Contains(r.URL.Path, "/tensiometre-T2-10387.html") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=ISO-8859-1")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/tensiometre")

	rooms := 2
	l := gazetteer.Listing{
		City:         "Troyes",
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        &rooms,
	}
	s := NewSource(Options{Geocoder: stubGeocoder{cityCode: "10387"}})
	data, err := s.Query(context.Background(), l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if hits.Load() != 1 {
		t.Errorf("expected 1 HTTP hit, got %d", hits.Load())
	}
	if res.Evidence.Logement != "T2" {
		t.Errorf("Evidence.Logement = %q, want T2", res.Evidence.Logement)
	}
	if res.Evidence.LogementUsed != "T2" {
		t.Errorf("Evidence.LogementUsed = %q, want T2", res.Evidence.LogementUsed)
	}
	if res.Evidence.FellBack {
		t.Error("Evidence.FellBack = true, want false on direct hit")
	}
}

func TestSource_FallbackToAllTypes(t *testing.T) {
	noDataBody := mustReadFixture(t, "riom_no_data.html")
	allBody := mustReadFixture(t, "limoges_all.html")
	calls := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=ISO-8859-1")
		switch {
		case strings.Contains(r.URL.Path, "tensiometre-T5-87085.html"):
			_, _ = w.Write(noDataBody) // 1st call: no data
		case strings.HasSuffix(r.URL.Path, "tensiometre-87085.html"):
			_, _ = w.Write(allBody) // 2nd call: commune-wide
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/tensiometre")

	rooms := 5
	l := gazetteer.Listing{
		City:         "Limoges",
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        &rooms,
	}
	s := NewSource(Options{Geocoder: stubGeocoder{cityCode: "87085"}})
	data, err := s.Query(context.Background(), l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if calls.Load() != 2 {
		t.Errorf("expected 2 HTTP calls (T5 + fallback), got %d", calls.Load())
	}
	if res.Confidence != ConfidenceMedium {
		t.Errorf("after fallback, expected confidence=medium, got %q", res.Confidence)
	}
	if !res.Evidence.FellBack {
		t.Error("Evidence.FellBack = false, want true")
	}
	if res.Evidence.Logement != "T5" {
		t.Errorf("Evidence.Logement = %q, want T5", res.Evidence.Logement)
	}
	if res.Evidence.LogementUsed != "" {
		t.Errorf("Evidence.LogementUsed = %q, want empty after fallback", res.Evidence.LogementUsed)
	}
}

func TestSource_NoData(t *testing.T) {
	body := mustReadFixture(t, "riom_no_data.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=ISO-8859-1")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/tensiometre")

	l := gazetteer.Listing{City: "Riom"}
	s := NewSource(Options{Geocoder: stubGeocoder{cityCode: "63300"}})
	data, err := s.Query(context.Background(), l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if !res.IsEmpty() {
		t.Error("IsEmpty() = false, want true on no-data path")
	}
	if res.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceLow)
	}
	if res.SampleSize != 0 {
		t.Errorf("SampleSize = %d, want 0", res.SampleSize)
	}
	if !res.Evidence.NoData {
		t.Error("Evidence.NoData = false, want true")
	}
}

func TestSource_NoAddress_Insufficient(t *testing.T) {
	s := NewSource(Options{Geocoder: stubGeocoder{cityCode: "75107"}})
	_, err := s.Query(context.Background(), gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(empty) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_GeocodeFails_Insufficient(t *testing.T) {
	s := NewSource(Options{Geocoder: stubGeocoder{err: errors.New("no address")}})
	_, err := s.Query(context.Background(), newListingParis7())
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(geocode err) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_GeocodeReturnsEmptyCityCode_Insufficient(t *testing.T) {
	s := NewSource(Options{Geocoder: stubGeocoder{cityCode: ""}})
	_, err := s.Query(context.Background(), newListingParis7())
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(no city code) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_UsesListingINSEEWhenSet(t *testing.T) {
	body := mustReadFixture(t, "paris7_all.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/tensiometre-75107.html") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=ISO-8859-1")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/tensiometre")

	// Geocoder would return a different INSEE if consulted — verify
	// we use the listing-provided one.
	s := NewSource(Options{Geocoder: stubGeocoder{cityCode: "99999"}})
	l := newListingParis7()
	l.INSEE = "75107"
	data, err := s.Query(context.Background(), l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if res.Evidence.INSEE != "75107" {
		t.Errorf("Evidence.INSEE = %q, want 75107 — listing INSEE should win", res.Evidence.INSEE)
	}
}

func TestSource_Upstream5xx_Transient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/tensiometre")

	s := NewSource(Options{Geocoder: stubGeocoder{cityCode: "75107"}})
	_, err := s.Query(context.Background(), newListingParis7())
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("Query(5xx) = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestSource_Upstream4xx_Permanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/tensiometre")

	s := NewSource(Options{Geocoder: stubGeocoder{cityCode: "75107"}})
	_, err := s.Query(context.Background(), newListingParis7())
	if !errors.Is(err, gazetteer.ErrUpstreamPermanent) {
		t.Errorf("Query(400) = %v, want ErrUpstreamPermanent", err)
	}
}

func TestSource_Upstream404_Permanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/tensiometre")

	s := NewSource(Options{Geocoder: stubGeocoder{cityCode: "00000"}})
	_, err := s.Query(context.Background(), newListingParis7())
	if !errors.Is(err, gazetteer.ErrUpstreamPermanent) {
		t.Errorf("Query(404) = %v, want ErrUpstreamPermanent", err)
	}
}

func TestSource_GarbageBody_Transient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=ISO-8859-1")
		_, _ = w.Write([]byte("<html><body>nothing useful</body></html>"))
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/tensiometre")

	s := NewSource(Options{Geocoder: stubGeocoder{cityCode: "75107"}})
	_, err := s.Query(context.Background(), newListingParis7())
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("Query(garbage) = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestQuery_TypedHelper(t *testing.T) {
	body := mustReadFixture(t, "paris7_all.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=ISO-8859-1")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/tensiometre")

	res, err := Query(context.Background(), Options{Geocoder: stubGeocoder{cityCode: "75107"}}, newListingParis7())
	if err != nil {
		t.Fatalf("Query helper: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Errorf("Query helper returned empty result")
	}
}

func TestFrom_RoundtripFromDossier(t *testing.T) {
	factory := gazetteer.Lookup(Name)
	if factory == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil, expected init() to register", Name)
	}
	v := factory()
	if _, ok := v.(*Result); !ok {
		t.Errorf("factory returned %T, want *Result", v)
	}
}

func TestBuildResult(t *testing.T) {
	t.Run("has_data_no_fallback", func(t *testing.T) {
		p := ParsedResult{HasData: true, TensionScore: 7, Label: LabelTresTendu, HasBudget: true, BudgetScore: 5}
		r := BuildResult(p, false)
		if r.Confidence != ConfidenceHigh {
			t.Errorf("Confidence = %q, want %q", r.Confidence, ConfidenceHigh)
		}
		if r.TensionScore == nil || *r.TensionScore != 7 {
			t.Errorf("TensionScore = %v, want 7", r.TensionScore)
		}
		if r.SupplyScore == nil || *r.SupplyScore != 7 {
			t.Errorf("SupplyScore = %v, want 7", r.SupplyScore)
		}
		if r.BudgetScore == nil || *r.BudgetScore != 5 {
			t.Errorf("BudgetScore = %v, want 5", r.BudgetScore)
		}
		if r.SampleSize != 1 {
			t.Errorf("SampleSize = %d, want 1", r.SampleSize)
		}
	})

	t.Run("has_data_after_fallback", func(t *testing.T) {
		p := ParsedResult{HasData: true, TensionScore: 4, Label: LabelEquilibre}
		r := BuildResult(p, true)
		if r.Confidence != ConfidenceMedium {
			t.Errorf("Confidence = %q, want %q", r.Confidence, ConfidenceMedium)
		}
	})

	t.Run("no_data_low_confidence", func(t *testing.T) {
		p := ParsedResult{HasData: false}
		r := BuildResult(p, false)
		if r.Confidence != ConfidenceLow {
			t.Errorf("Confidence = %q, want %q", r.Confidence, ConfidenceLow)
		}
		if r.SampleSize != 0 {
			t.Errorf("SampleSize = %d, want 0", r.SampleSize)
		}
		if r.TensionScore != nil {
			t.Errorf("TensionScore = %v, want nil on no-data branch", r.TensionScore)
		}
		// Sentinel: LabelEquilibre stamped on no-data branch for backwards-compat.
		if r.TensionLabel != string(LabelEquilibre) {
			t.Errorf("TensionLabel = %q, want %q (sentinel on no-data branch)", r.TensionLabel, LabelEquilibre)
		}
	})
}
