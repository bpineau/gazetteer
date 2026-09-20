package banx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

func TestPrecision_Order(t *testing.T) {
	t.Parallel()
	ordered := []Precision{PrecisionMunicipality, PrecisionLocality, PrecisionStreet, PrecisionHouseNumber}
	for i := 1; i < len(ordered); i++ {
		if !ordered[i-1].CoarserThan(ordered[i]) {
			t.Errorf("%q.CoarserThan(%q) = false, want true", ordered[i-1], ordered[i])
		}
		if ordered[i].CoarserThan(ordered[i-1]) {
			t.Errorf("%q.CoarserThan(%q) = true, want false", ordered[i], ordered[i-1])
		}
	}
	for _, p := range ordered {
		if p.CoarserThan(p) {
			t.Errorf("%q.CoarserThan(itself) = true, want false", p)
		}
		if !p.Known() {
			t.Errorf("%q.Known() = false, want true", p)
		}
	}
}

// TestPrecision_UnknownIsNotCoarse pins the convention that keeps every
// hand-built Listing, stub and proxy working: a precision nobody
// reported cannot be compared, so it is never refused. It mirrors how
// the INSEE cascade reads a missing Score.
func TestPrecision_UnknownIsNotCoarse(t *testing.T) {
	t.Parallel()
	for _, p := range []Precision{PrecisionUnknown, ParsePrecision("village")} {
		if p.CoarserThan(PrecisionHouseNumber) {
			t.Errorf("%q.CoarserThan(housenumber) = true, want false", p)
		}
		if p.Known() {
			t.Errorf("%q.Known() = true, want false", p)
		}
	}
	// A floor nobody set gates nothing either.
	if PrecisionMunicipality.CoarserThan(PrecisionUnknown) {
		t.Error("municipality.CoarserThan(unknown) = true, want false")
	}
}

func TestParsePrecision_NormalizesAndKeepsUnknowns(t *testing.T) {
	t.Parallel()
	if got := ParsePrecision("  HouseNumber "); got != PrecisionHouseNumber {
		t.Errorf("ParsePrecision = %q, want %q", got, PrecisionHouseNumber)
	}
	// An unrecognised type survives verbatim instead of being flattened,
	// so a value BAN adds tomorrow can still be read and logged.
	if got := ParsePrecision("Poi"); got != Precision("poi") {
		t.Errorf("ParsePrecision(Poi) = %q, want %q", got, "poi")
	}
}

// TestBANClient_DecodesPrecision is the whole point: the payload BAN
// returns for an address that does not exist carries everything needed
// to tell it from a doorstep, and the client used to drop it.
func TestBANClient_DecodesPrecision(t *testing.T) {
	t.Parallel()
	body := `{"features":[{"geometry":{"coordinates":[2.4397,48.8622]},
		"properties":{"label":"Montreuil","score":0.28,"citycode":"93048",
		"postcode":"93100","type":"municipality"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	old := BANEndpoint
	BANEndpoint = srv.URL
	defer func() { BANEndpoint = old }()

	hc, err := httpx.New(httpx.Options{})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	c := NewBANClient(hc)
	res, err := c.Geocode(context.Background(), GeocodeQuery{Address: "999 rue Inexistante 93100 Montreuil"})
	if err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	if res.Precision != PrecisionMunicipality {
		t.Errorf("Precision = %q, want municipality", res.Precision)
	}
	if res.Score != 0.28 {
		t.Errorf("Score = %v, want 0.28", res.Score)
	}
	// And the gated resolver refuses it while the ungated one does not.
	if _, _, _, err := ResolveLatLonAt(context.Background(), c, "999 rue Inexistante", "Montreuil", "93100", PrecisionStreet, 0); !errors.Is(err, ErrCoarseMatch) {
		t.Errorf("ResolveLatLonAt(street floor) err = %v, want ErrCoarseMatch", err)
	}
	lat, lon, err := ResolveLatLon(context.Background(), c, "999 rue Inexistante", "Montreuil", "93100")
	if err != nil || lat == 0 || lon == 0 {
		t.Errorf("ResolveLatLon = (%v, %v, %v), want the commune centre and no error", lat, lon, err)
	}
}

func TestResolveLatLonAt_ScoreFloor(t *testing.T) {
	t.Parallel()
	g := &stubGeocoder{res: GeocodeResult{Lat: 48.86, Lon: 2.44, Score: 0.31, Precision: PrecisionHouseNumber}}
	if _, _, _, err := ResolveLatLonAt(context.Background(), g, "a", "", "", PrecisionHouseNumber, 0.7); !errors.Is(err, ErrCoarseMatch) {
		t.Errorf("err = %v, want ErrCoarseMatch on a 0.31 score under a 0.7 floor", err)
	}
	// Score 0 means "unreported", not "zero confidence".
	g2 := &stubGeocoder{res: GeocodeResult{Lat: 48.86, Lon: 2.44, Precision: PrecisionHouseNumber}}
	if _, _, _, err := ResolveLatLonAt(context.Background(), g2, "a", "", "", PrecisionHouseNumber, 0.7); err != nil {
		t.Errorf("err = %v, want nil: an unreported score must not be gated", err)
	}
}

// TestINSEEResolution_CarriesPrecision: the cascade gates the INSEE on a
// 0.7 score but hands back whatever lat/lon came with it, and a
// commune-centre match passes that gate easily. Callers reading those
// coordinates at address granularity need to be told.
func TestINSEEResolution_CarriesPrecision(t *testing.T) {
	t.Parallel()
	g := &stubGeocoder{res: GeocodeResult{
		Lat: 48.8622, Lon: 2.4397, Score: 0.91,
		Precision: PrecisionMunicipality, CityCode: "93048", PostCode: "93100",
	}}
	r := &INSEEResolver{Forward: g}
	got, err := r.Resolve(context.Background(), INSEEQuery{Address: "Montreuil"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.INSEE != "93048" {
		t.Errorf("INSEE = %q, want 93048", got.INSEE)
	}
	if got.Precision != PrecisionMunicipality {
		t.Errorf("Precision = %q, want municipality - the coordinates are the commune's", got.Precision)
	}
}

// TestGeocodeResult_PrecisionSurvivesTheCache: the cached geocoder
// marshals the whole result, so a cache hit must answer with the same
// precision as the miss that filled it.
func TestGeocodeResult_PrecisionSurvivesTheCache(t *testing.T) {
	t.Parallel()
	in := GeocodeResult{Lat: 1, Lon: 2, Precision: PrecisionStreet}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out GeocodeResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Precision != PrecisionStreet {
		t.Errorf("Precision = %q after a round-trip, want street", out.Precision)
	}
}
