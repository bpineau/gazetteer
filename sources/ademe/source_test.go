package ademe

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
	postCode string
	cityCode string
	err      error
}

func (s stubGeocoder) Geocode(_ context.Context, _ banx.GeocodeQuery) (banx.GeocodeResult, error) {
	if s.err != nil {
		return banx.GeocodeResult{}, s.err
	}
	return banx.GeocodeResult{PostCode: s.postCode, CityCode: s.cityCode}, nil
}

// newListingParis11 returns a Listing for "82 Rue de la Roquette, 75011 Paris".
func newListingParis11() gazetteer.Listing {
	return gazetteer.Listing{
		Address:      "82 Rue de la Roquette 75011 Paris",
		City:         "Paris",
		Zip:          "75011",
		PropertyType: gazetteer.PropertyApartment,
	}
}

// newStubServer returns an httptest.Server that always responds with
// the given body and status code, and captures the last URL it received.
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
	s := NewSource(Options{})
	if s.Name() != Name {
		t.Errorf("Name() = %q, want %q", s.Name(), Name)
	}
	if s.Version() != sourceVersion {
		t.Errorf("Version() = %d, want %d", s.Version(), sourceVersion)
	}
}

func TestSource_HappyPath(t *testing.T) {
	body := mustReadFixture(t, "list_paris11.json")
	srv := newStubServer(t, http.StatusOK, body)

	s := NewSource(Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{postCode: "75011"},
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
	if res.DPE == nil {
		t.Fatal("DPE = nil, want populated")
	}
	if res.DPE.EtiquetteDPE == "" {
		t.Error("DPE.EtiquetteDPE is empty")
	}
	if res.DPE.NumeroDPE == "" {
		t.Error("DPE.NumeroDPE is empty")
	}
	if res.Logement == nil {
		t.Error("Logement = nil, want populated")
	}
	if res.Adresse == nil {
		t.Error("Adresse = nil, want populated")
	}
	if res.Confidence == "" {
		t.Error("Confidence is empty")
	}

	// Confirm the URL hit by the upstream stub embeds the zip filter
	// and the q_fields parameter — i.e. the URL builder is wired in.
	u, err := url.Parse(srv.lastURL)
	if err != nil {
		t.Fatalf("parse captured URL: %v", err)
	}
	if got := u.Query().Get("code_postal_ban"); got != "75011" {
		t.Errorf("code_postal_ban = %q, want 75011", got)
	}
	if got := u.Query().Get("q_fields"); got != "adresse_ban" {
		t.Errorf("q_fields = %q, want adresse_ban", got)
	}
}

func TestSource_EmptyResponse(t *testing.T) {
	srv := newStubServer(t, http.StatusOK, []byte(`{"total":0,"results":[]}`))

	s := NewSource(Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{postCode: "75011"},
	})
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
	if res.DPE != nil || res.Logement != nil || res.Adresse != nil {
		t.Errorf("expected nil sub-blobs on empty result, got %+v", res)
	}
}

func TestSource_HTTP5xx_ErrUpstreamUnavailable(t *testing.T) {
	srv := newStubServer(t, http.StatusServiceUnavailable, []byte(`{"error":"down"}`))

	s := NewSource(Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{postCode: "75011"},
	})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("Query = %v, want wrapping ErrUpstreamUnavailable", err)
	}
}

func TestSource_HTTP4xx_ErrUpstreamPermanent(t *testing.T) {
	srv := newStubServer(t, http.StatusForbidden, []byte(`{"error":"forbidden"}`))

	s := NewSource(Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{postCode: "75011"},
	})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrUpstreamPermanent) {
		t.Errorf("Query = %v, want wrapping ErrUpstreamPermanent", err)
	}
}

func TestSource_InsufficientInputs_NoAddress(t *testing.T) {
	s := NewSource(Options{Geocoder: stubGeocoder{postCode: "75011"}})
	_, err := s.Query(context.Background(), gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(empty listing) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_InsufficientInputs_ZipUnresolvable(t *testing.T) {
	// Zip missing on the listing → geocoder is invoked, returns an
	// error, which propagates as ErrInsufficientInputs.
	s := NewSource(Options{
		Geocoder: stubGeocoder{err: errors.New("no postcode")},
	})
	_, err := s.Query(context.Background(), gazetteer.Listing{
		Address: "82 Rue de la Roquette",
	})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(no zip, geocode fails) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_InsufficientInputs_NoStreetTokens(t *testing.T) {
	// Listing has a zip-only address (no street tokens). The
	// fraddr parser yields an empty query, which the Source rejects.
	srv := newStubServer(t, http.StatusOK, []byte(`{"total":0,"results":[]}`))
	s := NewSource(Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{postCode: "75011"},
	})
	_, err := s.Query(context.Background(), gazetteer.Listing{
		Address: "75011 Paris",
		Zip:     "75011",
	})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(zip-only address) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_FallsBackToGeocoderForZip(t *testing.T) {
	// Listing zip is missing — the Source must call the geocoder and
	// use its PostCode in the URL filter.
	body := mustReadFixture(t, "list_paris11.json")
	srv := newStubServer(t, http.StatusOK, body)

	s := NewSource(Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{postCode: "75011"},
	})
	_, err := s.Query(context.Background(), gazetteer.Listing{
		Address: "82 Rue de la Roquette",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	u, err := url.Parse(srv.lastURL)
	if err != nil {
		t.Fatalf("parse captured URL: %v", err)
	}
	if got := u.Query().Get("code_postal_ban"); got != "75011" {
		t.Errorf("code_postal_ban = %q, want geocoded 75011", got)
	}
}

func TestSource_PicksRowMatchingStreetNumber(t *testing.T) {
	// Synthetic body with three rows differing only in the number.
	// The listing is at "82" so we expect index 1 to be picked, and
	// the confidence high.
	body := []byte(`{"total":3,"results":[
        {"numero_dpe":"a","etiquette_dpe":"E","adresse_ban":"78 Rue X 75011 Paris"},
        {"numero_dpe":"b","etiquette_dpe":"D","adresse_ban":"82 Rue X 75011 Paris"},
        {"numero_dpe":"c","etiquette_dpe":"C","adresse_ban":"86 Rue X 75011 Paris"}
    ]}`)
	srv := newStubServer(t, http.StatusOK, body)
	s := NewSource(Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{postCode: "75011"},
	})
	data, err := s.Query(context.Background(), gazetteer.Listing{
		Address: "82 Rue X 75011 Paris",
		Zip:     "75011",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if res.DPE == nil || res.DPE.NumeroDPE != "b" {
		t.Errorf("picked DPE = %+v, want NumeroDPE=b", res.DPE)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want high", res.Confidence)
	}
}

func TestSource_ConfidenceMediumWhenNoNumberMatch(t *testing.T) {
	// None of the rows match the listing's number "82" → PickBest
	// fallback (row 0); confidence should be medium because the
	// etiquette is filled but the number did not match.
	body := []byte(`{"total":2,"results":[
        {"numero_dpe":"a","etiquette_dpe":"E","adresse_ban":"78 Rue X 75011 Paris"},
        {"numero_dpe":"b","etiquette_dpe":"D","adresse_ban":"86 Rue X 75011 Paris"}
    ]}`)
	srv := newStubServer(t, http.StatusOK, body)
	s := NewSource(Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{postCode: "75011"},
	})
	data, err := s.Query(context.Background(), gazetteer.Listing{
		Address: "82 Rue X 75011 Paris",
		Zip:     "75011",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want medium", res.Confidence)
	}
}

func TestSource_FetcherTransportError(t *testing.T) {
	// Closed server → connection refused → transport error wraps as
	// ErrUpstreamUnavailable.
	srv := newStubServer(t, http.StatusOK, []byte(`{}`))
	srv.Close()

	s := NewSource(Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{postCode: "75011"},
	})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("Query(closed server) = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestSource_ParseFailureMapsToUpstreamUnavailable(t *testing.T) {
	srv := newStubServer(t, http.StatusOK, []byte(`not json`))
	s := NewSource(Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{postCode: "75011"},
	})
	_, err := s.Query(context.Background(), newListingParis11())
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("Query(garbage body) = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestQueryAtomicHelper(t *testing.T) {
	body := mustReadFixture(t, "list_paris11.json")
	srv := newStubServer(t, http.StatusOK, body)

	res, err := Query(context.Background(), Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{postCode: "75011"},
	}, newListingParis11())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("Query atomic = %+v, want non-empty Result", res)
	}
}

func TestSource_RegistryRoundtrip(t *testing.T) {
	// Confirm the init() registration is in place: Lookup(Name) must
	// return a factory producing *Result.
	factory := gazetteer.Lookup(Name)
	if factory == nil {
		t.Fatal("gazetteer.Lookup(ademe) = nil, want a factory")
	}
	val := factory()
	if _, ok := val.(*Result); !ok {
		t.Errorf("factory() = %T, want *Result", val)
	}
}

func TestFrom_Dossier(t *testing.T) {
	// Build a Dossier manually with a synthetic Result entry and
	// verify From extracts the typed *Result.
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
	got, ok := From(d)
	if !ok || got != res {
		t.Errorf("From(d) = (%v, %v), want (%v, true)", got, ok, res)
	}
}

func TestFrom_DossierMissing(t *testing.T) {
	d := gazetteer.Dossier{Results: map[string]gazetteer.Result{}}
	got, ok := From(d)
	if ok || got != nil {
		t.Errorf("From(empty d) = (%v, %v), want (nil, false)", got, ok)
	}
}

func TestResult_JSONRoundtrip(t *testing.T) {
	// Marshal a populated Result and confirm the wire shape carries
	// the expected snake_case keys.
	body := mustReadFixture(t, "list_paris11.json")
	srv := newStubServer(t, http.StatusOK, body)

	data, err := Query(context.Background(), Options{
		BaseURL:  srv.URL,
		Geocoder: stubGeocoder{postCode: "75011"},
	}, newListingParis11())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(raw)
	for _, want := range []string{
		`"confidence":`, `"sample_size":`, `"dpe":`, `"logement":`, `"adresse":`,
		`"etiquette_dpe":`, `"code_postal_ban":"75011"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON missing %q in: %s", want, s)
		}
	}

	// Roundtrip via the registry.
	var d gazetteer.Dossier
	dossierJSON := []byte(`{"results":{"ademe":{"name":"ademe","status":"ok","data":` + string(raw) + `}}}`)
	if err := json.Unmarshal(dossierJSON, &d); err != nil {
		t.Fatalf("Dossier Unmarshal: %v", err)
	}
	got, ok := From(d)
	if !ok || got == nil {
		t.Fatalf("From(dossier) ok=%v got=%v", ok, got)
	}
	if got.Confidence == "" {
		t.Error("roundtripped Result.Confidence is empty")
	}
}

// TestSource_PopulatesEvidence pins the contract that Source.Query
// stamps Evidence on every returned Result — both happy-path (where
// MatchStrategy / Zip / Query / RawCount / PickedIndex / NumberMatched
// / URL are all set) and skipped-empty path (where PickedIndex = -1
// and RawCount is the raw row count from the upstream response).
//
// The encheridor adapter reads Evidence to fill EnrichPayload.Method.Params;
// regressions here silently null out the reproducibility blob.
func TestSource_PopulatesEvidence(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		body := []byte(`{"total":3,"results":[
            {"numero_dpe":"a","etiquette_dpe":"E","adresse_ban":"78 Rue X 75011 Paris"},
            {"numero_dpe":"b","etiquette_dpe":"D","adresse_ban":"82 Rue X 75011 Paris"},
            {"numero_dpe":"c","etiquette_dpe":"C","adresse_ban":"86 Rue X 75011 Paris"}
        ]}`)
		srv := newStubServer(t, http.StatusOK, body)
		s := NewSource(Options{
			BaseURL:  srv.URL,
			Geocoder: stubGeocoder{postCode: "75011"},
		})
		data, err := s.Query(context.Background(), gazetteer.Listing{
			Address: "82 Rue X 75011 Paris",
			Zip:     "75011",
		})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		res := data.(*Result)
		ev := res.Evidence
		if ev.MatchStrategy != MatchByZipFulltext {
			t.Errorf("Evidence.MatchStrategy = %q, want %q", ev.MatchStrategy, MatchByZipFulltext)
		}
		if ev.Zip != "75011" {
			t.Errorf("Evidence.Zip = %q, want 75011", ev.Zip)
		}
		if ev.Query == "" {
			t.Error("Evidence.Query is empty, want non-empty")
		}
		if ev.RawCount != 3 {
			t.Errorf("Evidence.RawCount = %d, want 3", ev.RawCount)
		}
		if ev.PickedIndex != 1 {
			t.Errorf("Evidence.PickedIndex = %d, want 1 (row at number 82)", ev.PickedIndex)
		}
		if !ev.NumberMatched {
			t.Error("Evidence.NumberMatched = false, want true")
		}
		if ev.URL == "" {
			t.Error("Evidence.URL is empty, want non-empty")
		}
	})

	t.Run("empty response", func(t *testing.T) {
		srv := newStubServer(t, http.StatusOK, []byte(`{"total":0,"results":[]}`))
		s := NewSource(Options{
			BaseURL:  srv.URL,
			Geocoder: stubGeocoder{postCode: "75011"},
		})
		data, err := s.Query(context.Background(), newListingParis11())
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		res := data.(*Result)
		ev := res.Evidence
		if ev.MatchStrategy != MatchByZipFulltext {
			t.Errorf("Evidence.MatchStrategy = %q, want %q", ev.MatchStrategy, MatchByZipFulltext)
		}
		if ev.Zip != "75011" {
			t.Errorf("Evidence.Zip = %q, want 75011", ev.Zip)
		}
		if ev.Query == "" {
			t.Error("Evidence.Query is empty, want non-empty")
		}
		if ev.RawCount != 0 {
			t.Errorf("Evidence.RawCount = %d, want 0", ev.RawCount)
		}
		if ev.PickedIndex != -1 {
			t.Errorf("Evidence.PickedIndex = %d, want -1 (sentinel)", ev.PickedIndex)
		}
		if ev.NumberMatched {
			t.Error("Evidence.NumberMatched = true, want false on empty result")
		}
		if ev.URL == "" {
			t.Error("Evidence.URL is empty, want non-empty (URL was built)")
		}
	})
}
