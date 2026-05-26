package gazetteer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/pkg/banx"
	"github.com/bpineau/gazetteer/pkg/communes"
)

type stubGeocoder struct {
	gotQuery banx.GeocodeQuery
	res      banx.GeocodeResult
	err      error
}

func (s *stubGeocoder) Geocode(ctx context.Context, q banx.GeocodeQuery) (banx.GeocodeResult, error) {
	s.gotQuery = q
	return s.res, s.err
}

type stubCommunes struct {
	byINSEE map[string]communes.Commune
}

func (s *stubCommunes) Lookup(insee string) (communes.Commune, bool) {
	c, ok := s.byINSEE[insee]
	return c, ok
}

func (s *stubCommunes) Neighbors(insee string, radiusKm float64) []string {
	return nil
}

func (s *stubCommunes) SameDepartment(insee string) []string {
	return nil
}

func TestBANNormalizer_HappyPath(t *testing.T) {
	g := &stubGeocoder{
		res: banx.GeocodeResult{
			Lat:       48.8696,
			Lon:       2.3322,
			Label:     "10 Rue de la Paix 75002 Paris",
			Score:     0.99,
			CityCode:  "75102",
			PostCode:  "75002",
			Source:    "ban",
			FetchedAt: time.Now(),
		},
	}
	n := NewBANNormalizer(g, nil)
	got, err := n.Normalize(context.Background(), "10 rue de la paix, paris")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if g.gotQuery.Address != "10 rue de la paix, paris" {
		t.Errorf("Geocoder received Address = %q, want raw input", g.gotQuery.Address)
	}
	if got.Address != "10 Rue de la Paix 75002 Paris" {
		t.Errorf("Address = %q, want canonical label", got.Address)
	}
	if got.INSEE != "75102" {
		t.Errorf("INSEE = %q, want 75102", got.INSEE)
	}
	if got.Zip != "75002" {
		t.Errorf("Zip = %q, want 75002", got.Zip)
	}
	if got.Lat == nil || *got.Lat != 48.8696 {
		t.Errorf("Lat = %v, want 48.8696", got.Lat)
	}
	if got.Lon == nil || *got.Lon != 2.3322 {
		t.Errorf("Lon = %v, want 2.3322", got.Lon)
	}
}

func TestBANNormalizer_GeocoderError(t *testing.T) {
	g := &stubGeocoder{err: errors.New("ban: 503")}
	n := NewBANNormalizer(g, nil)
	if _, err := n.Normalize(context.Background(), "10 rue X"); err == nil {
		t.Fatal("expected error from geocoder propagation")
	}
}

func TestBANNormalizer_NotFound(t *testing.T) {
	g := &stubGeocoder{err: banx.ErrNotFound}
	n := NewBANNormalizer(g, nil)
	_, err := n.Normalize(context.Background(), "garbage address")
	if !errors.Is(err, banx.ErrNotFound) {
		t.Errorf("err = %v, want banx.ErrNotFound", err)
	}
}

func TestBANNormalizer_PopulatesCityFromCommunes(t *testing.T) {
	g := &stubGeocoder{
		res: banx.GeocodeResult{
			Lat:      48.86,
			Lon:      2.34,
			Label:    "10 Rue X",
			CityCode: "75102",
			PostCode: "75002",
			Source:   "ban",
		},
	}
	c := &stubCommunes{
		byINSEE: map[string]communes.Commune{
			"75102": {INSEE: "75102", Dept: "75", Name: "Paris"},
		},
	}
	n := NewBANNormalizer(g, c)
	got, err := n.Normalize(context.Background(), "10 rue x")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.City != "Paris" {
		t.Errorf("City = %q, want Paris", got.City)
	}
}

func TestBANNormalizer_NilCommunesLeavesCityEmpty(t *testing.T) {
	g := &stubGeocoder{
		res: banx.GeocodeResult{CityCode: "75102", PostCode: "75002"},
	}
	n := NewBANNormalizer(g, nil) // communes nil → no lookup
	got, _ := n.Normalize(context.Background(), "x")
	if got.City != "" {
		t.Errorf("City = %q, want empty when no communes", got.City)
	}
}

func TestBANNormalizer_UnknownINSEELeavesCityEmpty(t *testing.T) {
	g := &stubGeocoder{
		res: banx.GeocodeResult{CityCode: "99999", PostCode: "99999"},
	}
	c := &stubCommunes{byINSEE: map[string]communes.Commune{}}
	n := NewBANNormalizer(g, c)
	got, _ := n.Normalize(context.Background(), "x")
	if got.City != "" {
		t.Errorf("City = %q, want empty when INSEE not found", got.City)
	}
}
