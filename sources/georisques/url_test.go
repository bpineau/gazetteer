package georisques

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestURLForLatLon_Happy(t *testing.T) {
	got, err := URLForLatLon(48.860874, 2.370245)
	if err != nil {
		t.Fatalf("URLForLatLon: %v", err)
	}
	want := "https://georisques.gouv.fr/api/v1/resultats_rapport_risque?latlon=2.370245,48.860874"
	if got != want {
		t.Errorf("URLForLatLon = %q\nwant %q", got, want)
	}
}

// TestURLForLatLon_OrderIsLonLat locks the ORDER of the latlon
// parameter to lon,lat — see url.go for the rationale. Inverting
// silently returns an empty rapport from the live API, which is the
// kind of bug you only catch in production. Lock it here.
func TestURLForLatLon_OrderIsLonLat(t *testing.T) {
	// Use distinct lat / lon values so the order is unambiguous in the
	// URL (lat=10 lon=99 → expected `latlon=99,10`).
	got, err := URLForLatLon(10.0, 99.0)
	if err != nil {
		t.Fatalf("URLForLatLon: %v", err)
	}
	if !strings.Contains(got, "latlon=99,10") {
		t.Errorf("URLForLatLon ordered lat,lon instead of lon,lat: %q", got)
	}
	if strings.Contains(got, "latlon=10,99") {
		t.Errorf("URLForLatLon emitted lat,lon — A23b inversion regression: %q", got)
	}
}

func TestURLForLatLon_OutOfRange(t *testing.T) {
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
			_, err := URLForLatLon(tc.lat, tc.lon)
			if !errors.Is(err, ErrInsufficientFilter) {
				t.Errorf("URLForLatLon(%v,%v) = %v, want ErrInsufficientFilter", tc.lat, tc.lon, err)
			}
		})
	}
}

func TestURLForLatLon_RespectsBaseURL(t *testing.T) {
	old := BaseURL
	BaseURL = "https://example.test/api/v1/risque"
	t.Cleanup(func() { BaseURL = old })
	got, err := URLForLatLon(48.86, 2.37)
	if err != nil {
		t.Fatalf("URLForLatLon: %v", err)
	}
	if !strings.HasPrefix(got, "https://example.test/api/v1/risque?") {
		t.Errorf("URLForLatLon ignored test-overridden BaseURL: %q", got)
	}
}

func TestClampDecimals(t *testing.T) {
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
			if got := clampDecimals(tc.in, tc.n); got != tc.want {
				t.Errorf("clampDecimals(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
		})
	}
}

func TestURLForLatLon_TruncatesPrecision(t *testing.T) {
	// 7 decimals → truncated to 6.
	got, err := URLForLatLon(48.8607421, 2.3702451)
	if err != nil {
		t.Fatalf("URLForLatLon: %v", err)
	}
	if !strings.Contains(got, "latlon=2.370245,48.860742") {
		t.Errorf("URLForLatLon did not truncate to 6 decimals: %q", got)
	}
}
