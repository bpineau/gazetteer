package gazetteer

import (
	"context"
	"errors"
	"testing"
)

type fakeNormalizer struct {
	want Listing
	err  error
}

func (f *fakeNormalizer) Normalize(ctx context.Context, addr string) (Listing, error) {
	return f.want, f.err
}

func TestNormalizer_Interface(t *testing.T) {
	var _ Normalizer = (*fakeNormalizer)(nil) // compile-time check
}

func TestNormalizeAddress_NoConfiguredBackendReturnsSentinel(t *testing.T) {
	// Phase 1 has no real BAN-backed normalizer. The default facade must
	// signal that explicitly — not panic, not return a half-valid Listing.
	prev := SetDefaultNormalizer(nil)
	t.Cleanup(func() { SetDefaultNormalizer(prev) })

	_, err := NormalizeAddress(context.Background(), "10 rue de la Paix, 75002 Paris")
	if !errors.Is(err, ErrNormalizerNotConfigured) {
		t.Errorf("expected ErrNormalizerNotConfigured, got %v", err)
	}
}

func TestSetDefaultNormalizer(t *testing.T) {
	wantLat, wantLon := 48.0, 2.0
	want := Listing{Address: "x", Lat: &wantLat, Lon: &wantLon}
	prev := SetDefaultNormalizer(&fakeNormalizer{want: want})
	t.Cleanup(func() { SetDefaultNormalizer(prev) })

	got, err := NormalizeAddress(context.Background(), "x")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.Address != "x" {
		t.Errorf("Address = %q, want %q", got.Address, "x")
	}
}
