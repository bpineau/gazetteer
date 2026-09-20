package cadastre

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/circuit"
)

// coarseGeocoder answers every query with a commune centre, the way BAN
// answers a street that does not exist: a plausible coordinate, a low
// score, and "municipality" as the only sign that it is the mairie's.
type coarseGeocoder struct {
	precision banx.Precision
	calls     int
}

func (g *coarseGeocoder) Geocode(_ context.Context, _ banx.GeocodeQuery) (banx.GeocodeResult, error) {
	g.calls++
	return banx.GeocodeResult{
		Lat:       49.01795,
		Lon:       1.99016,
		Label:     "Vaux-sur-Seine",
		Score:     0.28,
		Precision: g.precision,
		CityCode:  "78638",
		PostCode:  "78740",
		Source:    "ban",
	}, nil
}

// TestSource_RefusesCoarseGeocode pins the parcel lookup's precision
// floor on the geocoder fallback. A commune-centre match has a parcel of
// its own - the mairie's, or whoever owns the square - and returning it
// is indistinguishable from returning the property's.
func TestSource_RefusesCoarseGeocode(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "parcelle_small_commune.json")
	cases := []struct {
		name      string
		precision banx.Precision
		wantErr   bool
	}{
		{"municipality_refused", banx.PrecisionMunicipality, true},
		{"locality_refused", banx.PrecisionLocality, true},
		{"street_accepted", banx.PrecisionStreet, false},
		{"housenumber_accepted", banx.PrecisionHouseNumber, false},
		{"unreported_accepted", banx.PrecisionUnknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &coarseGeocoder{precision: tc.precision}
			s := NewSource(Options{
				Geocoder: g,
				Fetcher:  circuit.FuncFetcher(func(context.Context, string) ([]byte, error) { return body, nil }),
			})
			// No coordinates on the listing: the geocoder is the only way in.
			data, err := s.Query(context.Background(), gazetteer.Listing{
				Address: "12 rue Inexistante", Zip: "78740", City: "Vaux-sur-Seine",
			})
			if tc.wantErr {
				if !errors.Is(err, banx.ErrCoarseMatch) {
					t.Fatalf("Query err = %v, want ErrCoarseMatch", err)
				}
				if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
					t.Errorf("Query err = %v, want it to also classify as ErrInsufficientInputs", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			res := data.(*Result)
			if res.IsEmpty() {
				t.Fatal("IsEmpty() = true, want a parcel")
			}
			if res.Evidence.CoordPrecision != tc.precision {
				t.Errorf("Evidence.CoordPrecision = %q, want %q", res.Evidence.CoordPrecision, tc.precision)
			}
		})
	}
}

// TestSource_RefusesCoarseListingCoords covers the other half of the
// floor: a Listing that already carries coordinates, and says they are a
// commune centre. Those come from Client.Normalize, so this is the path
// a whole Dossier takes.
func TestSource_RefusesCoarseListingCoords(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "parcelle_small_commune.json")
	newSource := func() *Source {
		return NewSource(Options{
			Fetcher: circuit.FuncFetcher(func(context.Context, string) ([]byte, error) { return body, nil }),
		})
	}

	l := listingAt(49.01795, 1.99016)
	l.CoordPrecision = banx.PrecisionMunicipality
	if _, err := newSource().Query(context.Background(), l); !errors.Is(err, banx.ErrCoarseMatch) {
		t.Fatalf("Query(municipality coords) err = %v, want ErrCoarseMatch", err)
	}

	l.CoordPrecision = banx.PrecisionHouseNumber
	data, err := newSource().Query(context.Background(), l)
	if err != nil {
		t.Fatalf("Query(housenumber coords): %v", err)
	}
	if got := data.(*Result).Evidence.CoordPrecision; got != banx.PrecisionHouseNumber {
		t.Errorf("Evidence.CoordPrecision = %q, want housenumber", got)
	}

	// An opted-out caller gets the old behaviour back.
	l.CoordPrecision = banx.PrecisionMunicipality
	s := NewSource(Options{
		MinCoordPrecision: banx.PrecisionMunicipality,
		Fetcher:           circuit.FuncFetcher(func(context.Context, string) ([]byte, error) { return body, nil }),
	})
	if _, err := s.Query(context.Background(), l); err != nil {
		t.Errorf("Query with MinCoordPrecision=municipality: %v, want the parcel", err)
	}
}

// TestSource_MatchDistanceM pins the near-miss signal: a point inside the
// parcel reads 0, a point outside reads the real distance, and the caller
// can finally tell them apart.
func TestSource_MatchDistanceM(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "parcelle_small_commune.json")
	s := NewSource(Options{
		Fetcher: circuit.FuncFetcher(func(context.Context, string) ([]byte, error) { return body, nil }),
	})

	inside, err := s.Query(context.Background(), listingAt(49.01795, 1.99016))
	if err != nil {
		t.Fatalf("Query(inside): %v", err)
	}
	res := inside.(*Result)
	if res.MatchDistanceM == nil || *res.MatchDistanceM != 0 {
		t.Errorf("MatchDistanceM = %v, want 0 inside the parcel", res.MatchDistanceM)
	}
	if !res.Evidence.ParcelContains {
		t.Error("Evidence.ParcelContains = false, want true")
	}

	// Same canned body, a point in another country: the Source still
	// answers (the upstream is stubbed) but now says how far off it is.
	far, err := s.Query(context.Background(), listingAt(50.0, 10.0))
	if err != nil {
		t.Fatalf("Query(far): %v", err)
	}
	res = far.(*Result)
	if res.Evidence.ParcelContains {
		t.Error("Evidence.ParcelContains = true, want false")
	}
	if res.MatchDistanceM == nil || *res.MatchDistanceM < 500_000 {
		t.Errorf("MatchDistanceM = %v, want the real (huge) distance", res.MatchDistanceM)
	}
}
