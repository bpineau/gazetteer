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

// A commune-centre match is a perfectly ordinary coordinate inside some IRIS,
// usually the mairie's. Attributing that IRIS to the address hands filoiris,
// logiris and the Paris encadrement quartier the town hall's neighbourhood.
// The IRIS is a neighbourhood-scale reading: a street centroid is fine, a
// commune or locality centre is not. An unreported precision is never
// refused (a hand-built geocoder stub reports none).
func TestBANNormalizer_CoarseMatchGetsNoIRIS(t *testing.T) {
	for _, c := range []struct {
		precision banx.Precision
		want      string
	}{
		{banx.PrecisionMunicipality, ""},
		{banx.PrecisionLocality, ""},
		{banx.PrecisionStreet, "930660802"},
		{banx.PrecisionHouseNumber, "930660802"},
		{banx.PrecisionUnknown, "930660802"},
	} {
		g := &stubGeocoder{res: banx.GeocodeResult{Lat: 48.93, Lon: 2.35, CityCode: "93066", Precision: c.precision}}
		got, err := NewBANNormalizer(g, nil).WithIRIS(&stubIRIS{code: "930660802", ok: true}).Normalize(context.Background(), "x")
		if err != nil {
			t.Fatalf("%s: Normalize: %v", c.precision, err)
		}
		if got.IRIS != c.want {
			t.Errorf("precision %q: IRIS = %q, want %q", c.precision, got.IRIS, c.want)
		}
	}
}

// TestBANNormalizer_DepartmentGuard pins that a postcode written in the
// free text anchors the query and refuses a candidate in another
// département, through the real cache wrapper as well as bare.
func TestBANNormalizer_DepartmentGuard(t *testing.T) {
	t.Parallel()
	g := &stubGeocoder{res: banx.GeocodeResult{
		Lat: 44.84, Lon: -0.58, Label: "1 Rue Exemple 33000 Bordeaux", Score: 0.95,
		Precision: banx.PrecisionHouseNumber, CityCode: "33063", PostCode: "33000",
	}}
	_, err := NewBANNormalizer(g, nil).Normalize(context.Background(), "1 rue Exemple 75001 Paris")
	if !errors.Is(err, ErrAddressNotFound) || !errors.Is(err, banx.ErrDepartmentMismatch) {
		t.Errorf("cross-département candidate: err = %v, want ErrAddressNotFound wrapping ErrDepartmentMismatch", err)
	}
	if g.gotQuery.Zip != "75001" {
		t.Errorf("GeocodeQuery.Zip = %q, want the free-text postcode 75001", g.gotQuery.Zip)
	}
}

// TestBANNormalizer_ScoreFloor pins the weak-match policy: in the input's
// postcode the commune is kept and the coordinates demoted; elsewhere the
// match is refused; a strong match is untouched and carries its score.
func TestBANNormalizer_ScoreFloor(t *testing.T) {
	t.Parallel()
	weak := banx.GeocodeResult{
		Lat: 48.86, Lon: 2.38, Label: "3 Rue Autre 75011 Paris", Score: 0.12,
		Precision: banx.PrecisionHouseNumber, CityCode: "75111", PostCode: "75011",
	}
	l, err := NewBANNormalizer(&stubGeocoder{res: weak}, nil).Normalize(context.Background(), "3 rue Exemple 75011 Paris")
	if err != nil {
		t.Fatalf("weak match in the input's postcode: %v", err)
	}
	if l.INSEE != "75111" || l.CoordPrecision != banx.PrecisionMunicipality || l.Address != "3 rue Exemple 75011 Paris" || l.GeocodeScore != 0.12 {
		t.Errorf("weak match: INSEE %q precision %q address %q score %v; want the commune, municipality precision, the input text, 0.12",
			l.INSEE, l.CoordPrecision, l.Address, l.GeocodeScore)
	}

	if _, err := NewBANNormalizer(&stubGeocoder{res: weak}, nil).Normalize(context.Background(), "3 rue Exemple Paris"); !errors.Is(err, ErrAddressNotFound) {
		t.Errorf("weak match without a postcode anchor: err = %v, want ErrAddressNotFound", err)
	}

	strong := weak
	strong.Score = 0.97
	l, err = NewBANNormalizer(&stubGeocoder{res: strong}, nil).Normalize(context.Background(), "3 rue Autre 75011 Paris")
	if err != nil || l.CoordPrecision != banx.PrecisionHouseNumber || l.Address != strong.Label || l.GeocodeScore != 0.97 {
		t.Errorf("strong match: %+v, %v", l, err)
	}

	// The floor can be disabled.
	l, err = NewBANNormalizer(&stubGeocoder{res: weak}, nil).WithMinScore(0).Normalize(context.Background(), "3 rue Exemple Paris")
	if err != nil || l.CoordPrecision != banx.PrecisionHouseNumber {
		t.Errorf("floor disabled: %+v, %v", l, err)
	}
}

// TestBANNormalizer_StreetTypeContradiction pins that a candidate on a
// differently typed street of the commune keeps the commune only.
func TestBANNormalizer_StreetTypeContradiction(t *testing.T) {
	t.Parallel()
	g := &stubGeocoder{res: banx.GeocodeResult{
		Lat: 48.86, Lon: 2.44, Label: "12 Place Exemple 93100 Montreuil", Score: 0.9,
		Precision: banx.PrecisionHouseNumber, CityCode: "93048", PostCode: "93100",
	}}
	l, err := NewBANNormalizer(g, nil).Normalize(context.Background(), "12 av Exemple Montreuil")
	if err != nil {
		t.Fatal(err)
	}
	if l.INSEE != "93048" || l.CoordPrecision != banx.PrecisionMunicipality {
		t.Errorf("INSEE %q precision %q, want 93048 at municipality precision", l.INSEE, l.CoordPrecision)
	}
}
