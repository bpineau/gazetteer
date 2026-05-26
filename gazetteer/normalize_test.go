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

func TestClientNormalize_NoNormalizerReturnsSentinel(t *testing.T) {
	c, err := NewBuilder().Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = c.Normalize(context.Background(), "10 rue de la Paix, 75002 Paris")
	if !errors.Is(err, ErrNormalizerNotConfigured) {
		t.Errorf("expected ErrNormalizerNotConfigured, got %v", err)
	}
}

func TestClientNormalize_DelegatesToInstalledNormalizer(t *testing.T) {
	wantLat, wantLon := 48.0, 2.0
	want := Listing{Address: "x", Lat: &wantLat, Lon: &wantLon}
	c, err := NewBuilder().WithNormalizer(&fakeNormalizer{want: want}).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	got, err := c.Normalize(context.Background(), "x")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.Address != "x" {
		t.Errorf("Address = %q, want %q", got.Address, "x")
	}
}

func TestClientNormalize_NilClientReturnsSentinel(t *testing.T) {
	var c *Client
	if _, err := c.Normalize(context.Background(), "x"); !errors.Is(err, ErrNormalizerNotConfigured) {
		t.Errorf("nil Client should return ErrNormalizerNotConfigured, got %v", err)
	}
}
