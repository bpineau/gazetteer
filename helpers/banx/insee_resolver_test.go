package banx

import (
	"context"
	"errors"
	"testing"
)

// fakeGeocoder lets us script the BAN forward responses per call.
type fakeGeocoder struct {
	res GeocodeResult
	err error
}

func (f *fakeGeocoder) Geocode(_ context.Context, _ GeocodeQuery) (GeocodeResult, error) {
	return f.res, f.err
}

type fakeReverse struct {
	res GeocodeResult
	err error
}

func (f *fakeReverse) Reverse(_ context.Context, _, _ float64) (GeocodeResult, error) {
	return f.res, f.err
}

func TestINSEEResolver_BANForwardWins(t *testing.T) {
	// Forward returns a high-confidence result → ban_forward wins, reverse not invoked.
	r := &INSEEResolver{
		Forward: &fakeGeocoder{res: GeocodeResult{CityCode: "75008", Score: 0.92, Lat: 48.87, Lon: 2.32}},
		Reverse: &fakeReverse{res: GeocodeResult{CityCode: "75999"}}, // wrong on purpose
	}
	got, err := r.Resolve(context.Background(), INSEEQuery{
		Address: "5 rue Brey", Zip: "75017", Lat: 48.876, Lon: 2.296,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got.INSEE != "75008" {
		t.Errorf("INSEE = %q, want 75008 (forward)", got.INSEE)
	}
	if got.Source != "ban_forward" {
		t.Errorf("Source = %q, want ban_forward", got.Source)
	}
}

func TestINSEEResolver_FallbackToReverseOnLowScore(t *testing.T) {
	// Forward returns low confidence (below 0.7) → reverse takes over with input coords.
	r := &INSEEResolver{
		Forward: &fakeGeocoder{res: GeocodeResult{CityCode: "75008", Score: 0.4}},
		Reverse: &fakeReverse{res: GeocodeResult{CityCode: "75017"}},
	}
	got, err := r.Resolve(context.Background(), INSEEQuery{
		Address: "Résidence X, Lot Y", Lat: 48.876, Lon: 2.296,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got.INSEE != "75017" {
		t.Errorf("INSEE = %q, want 75017 (reverse)", got.INSEE)
	}
	if got.Source != "ban_reverse" {
		t.Errorf("Source = %q, want ban_reverse", got.Source)
	}
	// The input coords must be preserved (the reverse may return slightly
	// different ones — typically the centroid of the matched address).
	if got.Lat != 48.876 || got.Lon != 2.296 {
		t.Errorf("coords not preserved: got (%f, %f)", got.Lat, got.Lon)
	}
}

func TestINSEEResolver_FallbackToReverseOnEmptyCityCode(t *testing.T) {
	// Forward returns score≥threshold but empty citycode → reverse used.
	r := &INSEEResolver{
		Forward: &fakeGeocoder{res: GeocodeResult{CityCode: "", Score: 0.9}},
		Reverse: &fakeReverse{res: GeocodeResult{CityCode: "75017"}},
	}
	got, err := r.Resolve(context.Background(), INSEEQuery{
		Address: "...", Lat: 48.87, Lon: 2.30,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got.Source != "ban_reverse" {
		t.Errorf("Source = %q, want ban_reverse", got.Source)
	}
}

func TestINSEEResolver_FallbackToReverseOnForwardError(t *testing.T) {
	// Forward returns ErrNotFound → reverse takes over.
	r := &INSEEResolver{
		Forward: &fakeGeocoder{err: ErrNotFound},
		Reverse: &fakeReverse{res: GeocodeResult{CityCode: "75017"}},
	}
	got, err := r.Resolve(context.Background(), INSEEQuery{
		Address: "Lieu-dit Y", Lat: 48.87, Lon: 2.30,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got.INSEE != "75017" || got.Source != "ban_reverse" {
		t.Errorf("got %+v, want INSEE=75017 source=ban_reverse", got)
	}
}

func TestINSEEResolver_NoCoords_ForwardFailureBubbles(t *testing.T) {
	// Forward fails AND no coords → ErrNotFound (we don't have a zip-table
	// fallback today since the embedded communes.csv has no zip column).
	r := &INSEEResolver{
		Forward: &fakeGeocoder{err: ErrNotFound},
	}
	_, err := r.Resolve(context.Background(), INSEEQuery{Address: "Atlantis"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestINSEEResolver_NoCoords_LowScoreBubbles(t *testing.T) {
	// Forward returns low-score result, no coords → ErrNotFound.
	r := &INSEEResolver{
		Forward: &fakeGeocoder{res: GeocodeResult{CityCode: "X", Score: 0.3}},
	}
	_, err := r.Resolve(context.Background(), INSEEQuery{Address: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestINSEEResolver_HardForwardErrorWithoutCoords_Propagates(t *testing.T) {
	// Forward returns a transport error AND no coords → bubble the error.
	hard := errors.New("transport blew up")
	r := &INSEEResolver{
		Forward: &fakeGeocoder{err: hard},
	}
	_, err := r.Resolve(context.Background(), INSEEQuery{Address: "x"})
	if !errors.Is(err, hard) {
		t.Errorf("want transport err, got %v", err)
	}
}

func TestINSEEResolver_NilForward_Errors(t *testing.T) {
	r := &INSEEResolver{}
	_, err := r.Resolve(context.Background(), INSEEQuery{Address: "x"})
	if err == nil {
		t.Errorf("expected error on nil Forward")
	}
}
