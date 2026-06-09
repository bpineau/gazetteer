package dvf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/bpineau/gazetteer/helpers/geopoly"
	"github.com/bpineau/gazetteer/helpers/httpx"
)

func TestAccumulateBBox(t *testing.T) {
	cases := []struct {
		name   string
		coords string
		want   geopoly.BBox
	}{
		{
			// Polygon: one ring (3 nesting levels).
			name:   "polygon",
			coords: `[[[2.0,48.0],[2.0,48.5],[2.3,48.5],[2.3,48.0],[2.0,48.0]]]`,
			want:   geopoly.BBox{MinLon: 2.0, MinLat: 48.0, MaxLon: 2.3, MaxLat: 48.5},
		},
		{
			// MultiPolygon: two disjoint squares (4 nesting levels). The box
			// must span both, not just the first.
			name:   "multipolygon",
			coords: `[[[[1.0,40.0],[1.0,40.1],[1.1,40.1],[1.1,40.0],[1.0,40.0]]],[[[3.0,42.0],[3.0,42.2],[3.2,42.2],[3.2,42.0],[3.0,42.0]]]]`,
			want:   geopoly.BBox{MinLon: 1.0, MinLat: 40.0, MaxLon: 3.2, MaxLat: 42.2},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			box := emptyBBox()
			accumulateBBox(json.RawMessage(c.coords), &box)
			if box != c.want {
				t.Errorf("box = %+v, want %+v", box, c.want)
			}
		})
	}
}

func TestAccumulateBBox_EmptyStaysInverted(t *testing.T) {
	box := emptyBBox()
	accumulateBBox(nil, &box)
	accumulateBBox(json.RawMessage(`[]`), &box)
	// No vertex seen → still inverted-infinity → Contains is always false.
	if box.Contains(geopoly.Point{Lon: 2, Lat: 48}) {
		t.Error("empty box should Contain nothing")
	}
}

func TestPointToBBoxMeters(t *testing.T) {
	box := geopoly.BBox{MinLon: 2.0, MinLat: 48.0, MaxLon: 2.1, MaxLat: 48.1}

	// Inside the box → distance 0.
	if d := pointToBBoxMeters(48.05, 2.05, box); d != 0 {
		t.Errorf("inside-box distance = %.1f, want 0", d)
	}
	// Due east of the box edge by ~1 lon-degree at 48°N (~74 km). The
	// clamped longitude is the box's MaxLon (2.1), same latitude.
	east := pointToBBoxMeters(48.05, 3.1, box)
	if east < 60_000 || east > 90_000 {
		t.Errorf("east distance = %.0f m, want ~74 km", east)
	}
	// A nearer point is closer than a farther one (monotonicity).
	near := pointToBBoxMeters(48.05, 2.11, box)
	far := pointToBBoxMeters(48.05, 2.5, box)
	if near >= far {
		t.Errorf("near (%.0f) should be < far (%.0f)", near, far)
	}
}

func TestFetchCadastreSectionGeos_OK(t *testing.T) {
	// Section A near (48.05, 2.05); section B far at (42, 3). A duplicate
	// feature for A exercises the box union.
	const body = `{
		"type":"FeatureCollection",
		"features":[
			{"properties":{"commune":"99001","prefixe":"000","code":"A"},
			 "geometry":{"type":"Polygon","coordinates":[[[2.0,48.0],[2.0,48.1],[2.1,48.1],[2.1,48.0],[2.0,48.0]]]}},
			{"properties":{"commune":"99001","prefixe":"000","code":"A"},
			 "geometry":{"type":"Polygon","coordinates":[[[2.1,48.1],[2.1,48.2],[2.2,48.2],[2.2,48.1],[2.1,48.1]]]}},
			{"properties":{"commune":"99001","prefixe":"000","code":"B"},
			 "geometry":{"type":"Polygon","coordinates":[[[3.0,42.0],[3.0,42.1],[3.1,42.1],[3.1,42.0],[3.0,42.0]]]}},
			{"properties":{"commune":"99002","prefixe":"000","code":"Z"},
			 "geometry":{"type":"Polygon","coordinates":[[[2.0,48.0],[2.0,48.1],[2.1,48.1],[2.1,48.0],[2.0,48.0]]]}}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	old := CadastreSectionsBaseURL
	CadastreSectionsBaseURL = srv.URL + "/communes"
	defer func() { CadastreSectionsBaseURL = old }()

	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	geos, err := FetchCadastreSectionGeos(context.Background(), hc, "99001")
	if err != nil {
		t.Fatalf("FetchCadastreSectionGeos: %v", err)
	}
	if len(geos) != 2 { // A (deduped+unioned) and B; the 99002 feature is dropped
		t.Fatalf("got %d sections, want 2: %+v", len(geos), geos)
	}
	byCode := map[string]geopoly.BBox{}
	for _, g := range geos {
		byCode[g.Code] = g.Box
	}
	a, ok := byCode["0000A"]
	if !ok {
		t.Fatalf("missing section 0000A in %v", byCode)
	}
	// The union of A's two features spans lon[2.0,2.2] lat[48.0,48.2].
	want := geopoly.BBox{MinLon: 2.0, MinLat: 48.0, MaxLon: 2.2, MaxLat: 48.2}
	if a != want {
		t.Errorf("section A box = %+v, want %+v (union of both features)", a, want)
	}
}

func TestSectionsNearPoint_KeepsOnlyNearby(t *testing.T) {
	// Same fixture shape: A near (48.05,2.05), B ~670 km south. A point inside
	// A's box with a 500 m disk must keep A and drop B.
	const body = `{
		"type":"FeatureCollection",
		"features":[
			{"properties":{"commune":"99001","prefixe":"000","code":"A"},
			 "geometry":{"type":"Polygon","coordinates":[[[2.04,48.04],[2.04,48.06],[2.06,48.06],[2.06,48.04],[2.04,48.04]]]}},
			{"properties":{"commune":"99001","prefixe":"000","code":"B"},
			 "geometry":{"type":"Polygon","coordinates":[[[3.0,42.0],[3.0,42.1],[3.1,42.1],[3.1,42.0],[3.0,42.0]]]}}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	old := CadastreSectionsBaseURL
	CadastreSectionsBaseURL = srv.URL + "/communes"
	defer func() { CadastreSectionsBaseURL = old }()

	s := mustNewSource(t, Options{HTTP: newHTTPClient(t), Geocoder: stubGeocoder{}})
	secs := s.sectionsNearPoint(context.Background(), newQueryMemo(), "99001", 48.05, 2.05, 500)
	sort.Strings(secs)
	if len(secs) != 1 || secs[0] != "0000A" {
		t.Errorf("sectionsNearPoint = %v, want [0000A]", secs)
	}
}

func TestSectionsNearPoint_KeepsUnknownGeometry(t *testing.T) {
	// Section A is far from the point; section C has empty geometry (unknown
	// extent). The far A is dropped, but C must be KEPT — dropping a section of
	// unknown extent could silently lose an in-disk mutation.
	const body = `{
		"type":"FeatureCollection",
		"features":[
			{"properties":{"commune":"99001","prefixe":"000","code":"A"},
			 "geometry":{"type":"Polygon","coordinates":[[[3.0,42.0],[3.0,42.1],[3.1,42.1],[3.1,42.0],[3.0,42.0]]]}},
			{"properties":{"commune":"99001","prefixe":"000","code":"C"},
			 "geometry":{"type":"Polygon","coordinates":[]}}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	old := CadastreSectionsBaseURL
	CadastreSectionsBaseURL = srv.URL + "/communes"
	defer func() { CadastreSectionsBaseURL = old }()

	s := mustNewSource(t, Options{HTTP: newHTTPClient(t), Geocoder: stubGeocoder{}})
	secs := s.sectionsNearPoint(context.Background(), newQueryMemo(), "99001", 48.05, 2.05, 500)
	if len(secs) != 1 || secs[0] != "0000C" {
		t.Errorf("sectionsNearPoint = %v, want [0000C] (unknown-geometry section kept, far section dropped)", secs)
	}
}

func TestSectionsNearPoint_FallbackOnFetchError(t *testing.T) {
	// Cadastre 404 → nil, signalling the caller to fall back to a full
	// commune fan-out rather than fetching zero sections.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":404}`, http.StatusNotFound)
	}))
	defer srv.Close()
	old := CadastreSectionsBaseURL
	CadastreSectionsBaseURL = srv.URL + "/communes"
	defer func() { CadastreSectionsBaseURL = old }()

	s := mustNewSource(t, Options{HTTP: newHTTPClient(t), Geocoder: stubGeocoder{}})
	if secs := s.sectionsNearPoint(context.Background(), newQueryMemo(), "75056", 48.85, 2.35, 500); secs != nil {
		t.Errorf("expected nil (fall back) on cadastre 404, got %v", secs)
	}
}

func TestHostRateLimits(t *testing.T) {
	hl := HostRateLimits()
	for _, host := range []string{dvfAPIHost, cadastreHost} {
		ho, ok := hl[host]
		if !ok {
			t.Fatalf("HostRateLimits missing %q", host)
		}
		if ho.RateLimit == nil || *ho.RateLimit != hostRateLimit {
			t.Errorf("%s rate = %v, want %v", host, ho.RateLimit, hostRateLimit)
		}
		if ho.Burst == nil || *ho.Burst != hostBurst {
			t.Errorf("%s burst = %v, want %v", host, ho.Burst, hostBurst)
		}
		// Must beat the polite httpx default, else the override is pointless.
		if *ho.RateLimit <= httpx.DefaultRateLimitPerHost {
			t.Errorf("%s rate %.1f does not exceed default %.1f", host, *ho.RateLimit, httpx.DefaultRateLimitPerHost)
		}
	}
}
