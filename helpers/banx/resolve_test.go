package banx

import (
	"context"
	"errors"
	"testing"
)

// fakeFullGeocoder scripts both the forward and reverse responses —
// implementing ReverseGeocoder unlocks ResolveINSEE's reverse step.
type fakeFullGeocoder struct {
	fwd    GeocodeResult
	fwdErr error
	rev    GeocodeResult
	revErr error
}

func (f *fakeFullGeocoder) Geocode(_ context.Context, _ GeocodeQuery) (GeocodeResult, error) {
	return f.fwd, f.fwdErr
}

func (f *fakeFullGeocoder) Reverse(_ context.Context, _, _ float64) (GeocodeResult, error) {
	return f.rev, f.revErr
}

func TestResolveLatLon_HappyPath(t *testing.T) {
	g := &fakeGeocoder{res: GeocodeResult{Lat: 48.86, Lon: 2.35}}
	lat, lon, err := ResolveLatLon(context.Background(), g, "1 rue de Rivoli 75001 Paris", "Paris", "75001")
	if err != nil {
		t.Fatalf("ResolveLatLon: %v", err)
	}
	if lat != 48.86 || lon != 2.35 {
		t.Errorf("ResolveLatLon = (%v, %v), want (48.86, 2.35)", lat, lon)
	}
}

func TestResolveLatLon_NilGeocoder(t *testing.T) {
	_, _, err := ResolveLatLon(context.Background(), nil, "addr", "", "")
	if err == nil {
		t.Fatal("ResolveLatLon(nil geocoder) = nil error, want error")
	}
}

func TestResolveLatLon_GeocodeErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	g := &fakeGeocoder{err: sentinel}
	if _, _, err := ResolveLatLon(context.Background(), g, "addr", "", ""); !errors.Is(err, sentinel) {
		t.Errorf("ResolveLatLon(geocode err) = %v, want wrapped sentinel", err)
	}
}

func TestResolveLatLon_ZeroCoordsRejected(t *testing.T) {
	g := &fakeGeocoder{res: GeocodeResult{Lat: 0, Lon: 0}}
	if _, _, err := ResolveLatLon(context.Background(), g, "addr", "", ""); err == nil {
		t.Error("ResolveLatLon(zero coords) = nil error, want error")
	}
}

func TestResolveINSEE_ForwardWins(t *testing.T) {
	g := &fakeFullGeocoder{
		fwd: GeocodeResult{CityCode: "93048", Score: 0.92},
		rev: GeocodeResult{CityCode: "99999"}, // wrong on purpose
	}
	insee, source, err := ResolveINSEE(context.Background(), g, "12 rue X 93100 Montreuil", "Montreuil", "93100", 48.86, 2.44)
	if err != nil {
		t.Fatalf("ResolveINSEE: %v", err)
	}
	if insee != "93048" || source != "ban_forward" {
		t.Errorf("ResolveINSEE = (%q, %q), want (93048, ban_forward)", insee, source)
	}
}

func TestResolveINSEE_ReverseFallbackOnLowScore(t *testing.T) {
	g := &fakeFullGeocoder{
		fwd: GeocodeResult{CityCode: "75117", Score: 0.32}, // below the 0.7 gate
		rev: GeocodeResult{CityCode: "22055"},
	}
	insee, source, err := ResolveINSEE(context.Background(), g, "rue vague", "", "", 48.63, -2.83)
	if err != nil {
		t.Fatalf("ResolveINSEE: %v", err)
	}
	if insee != "22055" || source != "ban_reverse" {
		t.Errorf("ResolveINSEE = (%q, %q), want (22055, ban_reverse)", insee, source)
	}
}

func TestResolveINSEE_ForwardOnlyGeocoderSkipsReverse(t *testing.T) {
	// fakeGeocoder does NOT implement ReverseGeocoder: a low-score
	// forward with coords must fail with ErrNotFound instead of
	// reverse-resolving.
	g := &fakeGeocoder{res: GeocodeResult{CityCode: "75117", Score: 0.32}}
	if _, _, err := ResolveINSEE(context.Background(), g, "rue vague", "", "", 48.63, -2.83); !errors.Is(err, ErrNotFound) {
		t.Errorf("ResolveINSEE(forward-only, low score) = %v, want ErrNotFound", err)
	}
}

func TestResolveINSEE_NilGeocoder(t *testing.T) {
	if _, _, err := ResolveINSEE(context.Background(), nil, "addr", "", "", 0, 0); err == nil {
		t.Error("ResolveINSEE(nil geocoder) = nil error, want error")
	}
}

func TestResolveINSEE_NoInputs(t *testing.T) {
	g := &fakeFullGeocoder{}
	if _, _, err := ResolveINSEE(context.Background(), g, "", "", "", 0, 0); err == nil {
		t.Error("ResolveINSEE(no text, no coords) = nil error, want error")
	}
}
