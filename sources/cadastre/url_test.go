package cadastre

import (
	"errors"
	"math"
	"net/url"
	"strings"
	"testing"
)

func TestURLForLatLon_Happy(t *testing.T) {
	t.Parallel()

	got, err := URLForLatLon(48.8566, 2.3522)
	if err != nil {
		t.Fatalf("URLForLatLon: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !strings.HasPrefix(got, "https://apicarto.ign.fr/api/cadastre/parcelle?") {
		t.Errorf("URL prefix wrong: %s", got)
	}
	geom := u.Query().Get("geom")
	if geom == "" {
		t.Fatal(`URL is missing "geom" param`)
	}
	// GeoJSON Position order is [lon, lat] (RFC 7946 §3.1.1).
	want := `{"type":"Point","coordinates":[2.3522,48.8566]}`
	if geom != want {
		t.Errorf("geom = %q\nwant %q", geom, want)
	}
}

// TestURLForLatLon_OrderIsLonLat locks the GeoJSON Point coordinate
// order: lon FIRST, lat SECOND. Inverting silently returns a parcel
// in the wrong place (or zero features in oceans) — the kind of bug
// you only catch in production. Lock it here.
func TestURLForLatLon_OrderIsLonLat(t *testing.T) {
	t.Parallel()

	// Distinct values so the order is unambiguous: lat=10, lon=99 →
	// expected `coordinates":[99,10]`.
	got, err := URLForLatLon(10.0, 99.0)
	if err != nil {
		t.Fatalf("URLForLatLon: %v", err)
	}
	if !strings.Contains(got, `coordinates%22%3A%5B99%2C10%5D`) &&
		!strings.Contains(got, `coordinates":[99,10]`) {
		// Either the percent-encoded or raw form (depending on
		// url.Values's encoder) must contain lon,lat ordering.
		t.Errorf("URL ordered lat,lon instead of lon,lat: %q", got)
	}
}

func TestURLForLatLon_OutOfRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lat, lon float64
	}{
		{"lat>90", 91.0, 0.0},
		{"lat<-90", -91.0, 0.0},
		{"lon>180", 0.0, 181.0},
		{"lon<-180", 0.0, -181.0},
		{"NaN_lat", math.NaN(), 0.0},
		{"Inf_lon", 0.0, math.Inf(1)},
		{"both_zero", 0.0, 0.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := URLForLatLon(tc.lat, tc.lon)
			if !errors.Is(err, ErrInsufficientFilter) {
				t.Errorf("URLForLatLon(%v,%v) = %v, want ErrInsufficientFilter", tc.lat, tc.lon, err)
			}
		})
	}
}

func TestURLForLatLon_TruncatesPrecision(t *testing.T) {
	t.Parallel()

	got, err := URLForLatLon(48.8607421, 2.3702451)
	if err != nil {
		t.Fatalf("URLForLatLon: %v", err)
	}
	u, _ := url.Parse(got)
	if !strings.Contains(u.Query().Get("geom"), `[2.370245,48.860742]`) {
		t.Errorf("URLForLatLon did not truncate to 6 decimals: %q", u.Query().Get("geom"))
	}
}

func TestBatimentsURLForINSEE_Happy(t *testing.T) {
	t.Parallel()

	got, err := BatimentsURLForINSEE("75104")
	if err != nil {
		t.Fatalf("BatimentsURLForINSEE: %v", err)
	}
	want := "https://cadastre.data.gouv.fr/bundler/cadastre-etalab/communes/75104/geojson/batiments"
	if got != want {
		t.Errorf("BatimentsURLForINSEE = %q\nwant %q", got, want)
	}
}

func TestBatimentsURLForINSEE_Corsica(t *testing.T) {
	t.Parallel()

	// Corsica INSEE codes carry a letter at position 1 ("2A" / "2B").
	got, err := BatimentsURLForINSEE("2A004")
	if err != nil {
		t.Fatalf("BatimentsURLForINSEE: %v", err)
	}
	if !strings.HasSuffix(got, "/2A004/geojson/batiments") {
		t.Errorf("path = %q, want INSEE preserved verbatim", got)
	}
}

func TestBatimentsURLForINSEE_Reject(t *testing.T) {
	t.Parallel()

	tests := []string{"", "7510", "751040", "ABCDE", "1234X"}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			if _, err := BatimentsURLForINSEE(in); !errors.Is(err, ErrInsufficientFilter) {
				t.Errorf("BatimentsURLForINSEE(%q) err = %v, want ErrInsufficientFilter", in, err)
			}
		})
	}
}

func TestClampDecimals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"48.8607421", 6, "48.860742"},
		{"48.86", 6, "48.86"},
		{"48", 6, "48"},
		{"-2.0123456789", 4, "-2.0123"},
		{"0.0", 6, "0.0"},
		{"1.234567", 6, "1.234567"},
		{"1.2345678", 6, "1.234567"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := clampDecimals(tc.in, tc.n); got != tc.want {
				t.Errorf("clampDecimals(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
		})
	}
}
