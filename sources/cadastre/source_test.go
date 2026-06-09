package cadastre

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
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

// listingAt returns a Listing whose Lat/Lon are set to the given
// coords. Used by every Source-level test.
func listingAt(lat, lon float64) gazetteer.Listing {
	return gazetteer.Listing{Lat: &lat, Lon: &lon}
}

// muxServer wires both the parcelle endpoint and the bâti endpoint
// onto a single httptest.Server (per memory `test_stubs_multi_endpoint_rate_limit.md`,
// splitting servers can hang on cascade fetches).
type muxServer struct {
	*httptest.Server
	lastParcelleURL string
	lastBatiURL     string
}

func newMuxServer(t *testing.T, parcelleStatus int, parcelleBody []byte, batiStatus int, batiBody []byte) *muxServer {
	t.Helper()
	ms := &muxServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cadastre/parcelle", func(w http.ResponseWriter, r *http.Request) {
		ms.lastParcelleURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(parcelleStatus)
		_, _ = w.Write(parcelleBody)
	})
	// The bâti path on the real upstream is
	// `/<INSEE>/geojson/batiments`; our applyBatiBaseURL rewrites the
	// `BatiBaseURL` prefix, so the suffix the server sees is
	// `/<INSEE>/geojson/batiments` (no leading bundler path).
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/geojson/batiments") {
			http.NotFound(w, r)
			return
		}
		ms.lastBatiURL = r.URL.String()
		w.Header().Set("Content-Type", "application/vnd.geo+json")
		w.WriteHeader(batiStatus)
		_, _ = w.Write(batiBody)
	})
	ms.Server = httptest.NewServer(mux)
	t.Cleanup(ms.Close)
	return ms
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

func TestSource_HappyPath_NoBati(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "parcelle_paris_1er.json")
	ms := newMuxServer(t, http.StatusOK, body, http.StatusNotFound, nil)

	parcelleURL := ms.URL + "/api/cadastre/parcelle"
	s := NewSource(Options{BaseURL: parcelleURL})
	data, err := s.Query(context.Background(), listingAt(48.8566, 2.3522))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res, ok := data.(*Result)
	if !ok {
		t.Fatalf("Query returned %T, want *Result", data)
	}
	if res.IsEmpty() {
		t.Fatal("IsEmpty() = true, want false on happy path")
	}
	if len(res.Parcels) != 1 {
		t.Fatalf("len(Parcels) = %d, want 1", len(res.Parcels))
	}
	p := res.Parcels[0]
	if len(p.ID) != 14 {
		t.Errorf("Parcels[0].ID = %q, want 14-char Etalab id", p.ID)
	}
	if p.ContenanceM2 <= 0 {
		t.Errorf("Parcels[0].ContenanceM2 = %d, want >0", p.ContenanceM2)
	}
	if p.MapURL == "" {
		t.Error("Parcels[0].MapURL is empty")
	}

	// Evidence
	ev := res.Evidence
	if ev.Lat != 48.8566 || ev.Lon != 2.3522 {
		t.Errorf("Evidence Lat/Lon = (%v, %v), want (48.8566, 2.3522)", ev.Lat, ev.Lon)
	}
	if !strings.Contains(ev.ParcelleAPIURL, "geom=") {
		t.Errorf("Evidence.ParcelleAPIURL is missing the geom param: %s", ev.ParcelleAPIURL)
	}
	if ev.BatiBaseURL != "" || ev.BatiRawCount != 0 || ev.BatiError != "" {
		t.Errorf("expected no bati evidence (IncludeBati=false), got %+v", ev)
	}
	if res.BatiM2 != nil || res.BatiCount != nil || res.EmpriseRatio != nil {
		t.Errorf("expected nil bati fields (IncludeBati=false), got %+v", res)
	}

	// Confirm the URL hit by the upstream stub embeds the geom param.
	u, err := url.Parse(ms.lastParcelleURL)
	if err != nil {
		t.Fatalf("parse captured URL: %v", err)
	}
	if u.Query().Get("geom") == "" {
		t.Error("upstream URL is missing the geom parameter")
	}
}

func TestSource_HappyPath_WithBati(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "parcelle_small_commune.json")
	// Build a synthetic bâti dump that mirrors the parcel polygon and
	// adds 4 small buildings (2 centred on the parcel, 2 elsewhere).
	batiBody := buildSyntheticBatiAroundSmallCommune(t)
	ms := newMuxServer(t, http.StatusOK, body, http.StatusOK, batiBody)

	parcelleURL := ms.URL + "/api/cadastre/parcelle"
	s := NewSource(Options{
		BaseURL:     parcelleURL,
		BatiBaseURL: ms.URL,
		IncludeBati: true,
	})
	data, err := s.Query(context.Background(), listingAt(49.01795, 1.99016))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if res.IsEmpty() {
		t.Fatal("IsEmpty() = true on happy path")
	}
	if res.BatiCount == nil || *res.BatiCount == 0 {
		t.Errorf("BatiCount = %v, want >0 in-parcel building", res.BatiCount)
	}
	if res.BatiM2 == nil || *res.BatiM2 <= 0 {
		t.Errorf("BatiM2 = %v, want >0", res.BatiM2)
	}
	if res.EmpriseRatio == nil || *res.EmpriseRatio <= 0 {
		t.Errorf("EmpriseRatio = %v, want >0", res.EmpriseRatio)
	}
	if res.Evidence.BatiError != "" {
		t.Errorf("Evidence.BatiError = %q on happy path", res.Evidence.BatiError)
	}
	if ms.lastBatiURL == "" {
		t.Error("upstream bati endpoint was not hit")
	}
}

func TestSource_BatiCachedOnSecondCall(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "parcelle_small_commune.json")
	batiBody := buildSyntheticBatiAroundSmallCommune(t)
	ms := newMuxServer(t, http.StatusOK, body, http.StatusOK, batiBody)

	parcelleURL := ms.URL + "/api/cadastre/parcelle"
	s := NewSource(Options{
		BaseURL:     parcelleURL,
		BatiBaseURL: ms.URL,
		IncludeBati: true,
	})

	if _, err := s.Query(context.Background(), listingAt(49.01795, 1.99016)); err != nil {
		t.Fatalf("first Query: %v", err)
	}
	data, err := s.Query(context.Background(), listingAt(49.01795, 1.99016))
	if err != nil {
		t.Fatalf("second Query: %v", err)
	}
	res := data.(*Result)
	if !res.Evidence.BatiCached {
		t.Error("Evidence.BatiCached = false on second call, want true")
	}
}

func TestSource_BatiSoftFailOn500(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "parcelle_small_commune.json")
	ms := newMuxServer(t, http.StatusOK, body, http.StatusInternalServerError, []byte("upstream down"))

	parcelleURL := ms.URL + "/api/cadastre/parcelle"
	s := NewSource(Options{
		BaseURL:     parcelleURL,
		BatiBaseURL: ms.URL,
		IncludeBati: true,
	})
	data, err := s.Query(context.Background(), listingAt(49.01795, 1.99016))
	if err != nil {
		t.Fatalf("Query (bati 500) returned err = %v, want nil (soft fail)", err)
	}
	res := data.(*Result)
	if res.IsEmpty() {
		t.Fatal("IsEmpty() = true, expected parcel to come through")
	}
	if res.Evidence.BatiError == "" {
		t.Error("Evidence.BatiError is empty on bati soft failure")
	}
	if res.BatiM2 != nil || res.BatiCount != nil {
		t.Errorf("bati fields populated on soft-fail: %+v", res)
	}
}

func TestSource_Empty404(t *testing.T) {
	t.Parallel()

	ms := newMuxServer(t, http.StatusNotFound, nil, http.StatusNotFound, nil)
	parcelleURL := ms.URL + "/api/cadastre/parcelle"
	s := NewSource(Options{BaseURL: parcelleURL})
	data, err := s.Query(context.Background(), listingAt(48.8566, 2.3522))
	if err != nil {
		t.Fatalf("Query(404): %v", err)
	}
	res := data.(*Result)
	if !res.IsEmpty() {
		t.Errorf("IsEmpty() = false, want true on 404 (got %+v)", res)
	}
}

func TestSource_EmptyFeatureCollection(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "parcelle_empty.json")
	ms := newMuxServer(t, http.StatusOK, body, http.StatusNotFound, nil)
	parcelleURL := ms.URL + "/api/cadastre/parcelle"
	s := NewSource(Options{BaseURL: parcelleURL})
	data, err := s.Query(context.Background(), listingAt(48.8566, 2.3522))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !data.(*Result).IsEmpty() {
		t.Error("IsEmpty() = false on empty FeatureCollection")
	}
}

func TestSource_Upstream5xx_Transient(t *testing.T) {
	t.Parallel()

	ms := newMuxServer(t, http.StatusServiceUnavailable, []byte("down"), http.StatusNotFound, nil)
	parcelleURL := ms.URL + "/api/cadastre/parcelle"
	s := NewSource(Options{BaseURL: parcelleURL})
	_, err := s.Query(context.Background(), listingAt(48.8566, 2.3522))
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("Query(5xx) = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestSource_Upstream4xx_Permanent(t *testing.T) {
	t.Parallel()

	ms := newMuxServer(t, http.StatusBadRequest, []byte("bad"), http.StatusNotFound, nil)
	parcelleURL := ms.URL + "/api/cadastre/parcelle"
	s := NewSource(Options{BaseURL: parcelleURL})
	_, err := s.Query(context.Background(), listingAt(48.8566, 2.3522))
	if !errors.Is(err, gazetteer.ErrUpstreamPermanent) {
		t.Errorf("Query(400) = %v, want ErrUpstreamPermanent", err)
	}
}

func TestSource_MissingLatLon_Insufficient(t *testing.T) {
	t.Parallel()

	s := NewSource(Options{})
	_, err := s.Query(context.Background(), gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(empty) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_ZeroLatLon_Insufficient(t *testing.T) {
	t.Parallel()

	// Listing carries (0, 0) — refuse to send the request.
	s := NewSource(Options{})
	_, err := s.Query(context.Background(), listingAt(0, 0))
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(0,0) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_GarbageBody_Transient(t *testing.T) {
	t.Parallel()

	ms := newMuxServer(t, http.StatusOK, []byte("not json"), http.StatusNotFound, nil)
	parcelleURL := ms.URL + "/api/cadastre/parcelle"
	s := NewSource(Options{BaseURL: parcelleURL})
	_, err := s.Query(context.Background(), listingAt(48.8566, 2.3522))
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("Query(garbage) = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestQuery_TypedHelper(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "parcelle_paris_1er.json")
	ms := newMuxServer(t, http.StatusOK, body, http.StatusNotFound, nil)
	parcelleURL := ms.URL + "/api/cadastre/parcelle"
	res, err := Query(context.Background(), Options{BaseURL: parcelleURL}, listingAt(48.8566, 2.3522))
	if err != nil {
		t.Fatalf("Query helper: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Errorf("Query helper returned empty result")
	}
}

func TestFrom_RoundtripFromDossier(t *testing.T) {
	t.Parallel()

	// Construction-level check: gazetteer.Lookup must return our typed
	// Result thanks to the init() Register call.
	factory := gazetteer.Lookup(Name)
	if factory == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil, expected init() to register", Name)
	}
	v := factory()
	if _, ok := v.(*Result); !ok {
		t.Errorf("factory returned %T, want *Result", v)
	}
}

// buildSyntheticBatiAroundSmallCommune crafts a small bâti
// FeatureCollection with a few polygons positioned to overlap the
// `parcelle_small_commune.json` parcel's geometry (Vaux-sur-Seine).
// Inputs come from inspecting the real fixture; the helper produces a
// dump that has at least one building whose centroid falls inside the
// real parcel polygon.
func buildSyntheticBatiAroundSmallCommune(t *testing.T) []byte {
	t.Helper()
	// The real parcel polygon (per the fixture) wraps around
	// 1.989/49.0179. Place a tiny building polygon centred on
	// (1.98975, 49.01790) — inside the parcel — and two more far away.
	return []byte(`{
  "type": "FeatureCollection",
  "features": [
    {"type":"Feature","properties":{"commune":"78638"},"geometry":{"type":"MultiPolygon","coordinates":[[[
       [1.98970,49.01785],[1.98980,49.01785],[1.98980,49.01795],[1.98970,49.01795],[1.98970,49.01785]
    ]]]}},
    {"type":"Feature","properties":{"commune":"78638"},"geometry":{"type":"MultiPolygon","coordinates":[[[
       [1.98985,49.01788],[1.98995,49.01788],[1.98995,49.01798],[1.98985,49.01798],[1.98985,49.01788]
    ]]]}},
    {"type":"Feature","properties":{"commune":"78638"},"geometry":{"type":"MultiPolygon","coordinates":[[[
       [1.10000,49.10000],[1.10010,49.10000],[1.10010,49.10010],[1.10000,49.10010],[1.10000,49.10000]
    ]]]}},
    {"type":"Feature","properties":{"commune":"78638"},"geometry":{"type":"MultiPolygon","coordinates":[[[
       [1.20000,49.20000],[1.20010,49.20000],[1.20010,49.20010],[1.20000,49.20010],[1.20000,49.20000]
    ]]]}}
  ]
}`)
}
