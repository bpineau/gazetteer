package gazetteer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/communes"
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
	// Both the not-found and dept-mismatch geocoder sentinels are raised to
	// gazetteer.ErrAddressNotFound, while errors.Is still matches the original
	// banx sentinel (double-%w). A generic geocoder error is NOT ErrAddressNotFound.
	for _, tc := range []struct {
		name string
		in   error
	}{
		{"not found", banx.ErrNotFound},
		{"department mismatch", banx.ErrDepartmentMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &stubGeocoder{err: tc.in}
			n := NewBANNormalizer(g, nil)
			_, err := n.Normalize(context.Background(), "garbage address")
			if !errors.Is(err, ErrAddressNotFound) {
				t.Errorf("err = %v, want errors.Is ErrAddressNotFound", err)
			}
			if !errors.Is(err, tc.in) {
				t.Errorf("err = %v, want errors.Is %v (original sentinel preserved)", err, tc.in)
			}
		})
	}

	t.Run("generic error is not ErrAddressNotFound", func(t *testing.T) {
		g := &stubGeocoder{err: errors.New("ban: 503")}
		n := NewBANNormalizer(g, nil)
		_, err := n.Normalize(context.Background(), "10 rue X")
		if errors.Is(err, ErrAddressNotFound) {
			t.Errorf("generic geocoder error wrongly classified as ErrAddressNotFound: %v", err)
		}
	})
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

type stubIRIS struct {
	code           string
	ok             bool
	gotLat, gotLon float64
}

func (s *stubIRIS) ResolveIRIS(lat, lon float64) (string, bool) {
	s.gotLat, s.gotLon = lat, lon
	return s.code, s.ok
}

func TestBANNormalizer_PopulatesIRIS(t *testing.T) {
	g := &stubGeocoder{res: banx.GeocodeResult{Lat: 48.9355, Lon: 2.3590, CityCode: "93066"}}
	ir := &stubIRIS{code: "930660802", ok: true}
	n := NewBANNormalizer(g, nil).WithIRIS(ir)
	got, err := n.Normalize(context.Background(), "x")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.IRIS != "930660802" {
		t.Errorf("IRIS = %q, want 930660802", got.IRIS)
	}
	if ir.gotLat != 48.9355 || ir.gotLon != 2.3590 {
		t.Errorf("resolver got (%v,%v), want the geocoded coords", ir.gotLat, ir.gotLon)
	}
}

func TestBANNormalizer_IRISMissLeavesEmpty(t *testing.T) {
	g := &stubGeocoder{res: banx.GeocodeResult{Lat: 1, Lon: 1, CityCode: "00000"}}
	n := NewBANNormalizer(g, nil).WithIRIS(&stubIRIS{ok: false})
	got, _ := n.Normalize(context.Background(), "x")
	if got.IRIS != "" {
		t.Errorf("IRIS = %q, want empty on resolver miss", got.IRIS)
	}
}

func TestBANNormalizer_NoIRISResolverLeavesEmpty(t *testing.T) {
	g := &stubGeocoder{res: banx.GeocodeResult{Lat: 48.9, Lon: 2.3, CityCode: "93066"}}
	got, _ := NewBANNormalizer(g, nil).Normalize(context.Background(), "x") // no WithIRIS
	if got.IRIS != "" {
		t.Errorf("IRIS = %q, want empty when no resolver wired", got.IRIS)
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
