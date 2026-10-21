package georisques

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
)

// coarseGeocoder answers every address with a commune centre, as BAN does for
// a street it cannot find.
type coarseGeocoder struct{}

func (coarseGeocoder) Geocode(context.Context, banx.GeocodeQuery) (banx.GeocodeResult, error) {
	return banx.GeocodeResult{Lat: 48.86, Lon: 2.37, Precision: banx.PrecisionMunicipality}, nil
}

// The rapport-risque is a point reading (flood zone, clay, cavities, nearby
// sites). Read at a commune centre it is the mairie's plot, so neither the
// listing's own coarse coordinates nor a coarse geocoder answer may reach the
// upstream. Before: both returned the mairie's report as the address's.
func TestSource_CoarseCoordinatesRefused(t *testing.T) {
	t.Parallel()
	body := mustReadFixture(t, "paris11.json")
	srv := newStubServer(t, http.StatusOK, body)

	lat, lon := 48.86, 2.37
	withCoords := newListingParis11()
	withCoords.Lat, withCoords.Lon, withCoords.CoordPrecision = &lat, &lon, banx.PrecisionMunicipality
	s := NewSource(Options{BaseURL: srv.URL, Geocoder: stubGeocoder{lat: 1, lon: 1}, HTTPClient: srv.Client()})
	if _, err := s.Query(context.Background(), withCoords); !errors.Is(err, gazetteer.ErrInsufficientInputs) || !errors.Is(err, banx.ErrCoarseMatch) {
		t.Errorf("coarse listing coordinates: err = %v, want ErrInsufficientInputs + ErrCoarseMatch", err)
	}

	geocoded := NewSource(Options{BaseURL: srv.URL, Geocoder: coarseGeocoder{}, HTTPClient: srv.Client()})
	if _, err := geocoded.Query(context.Background(), newListingParis11()); !errors.Is(err, gazetteer.ErrInsufficientInputs) || !errors.Is(err, banx.ErrCoarseMatch) {
		t.Errorf("coarse geocoder answer: err = %v, want ErrInsufficientInputs + ErrCoarseMatch", err)
	}

	withCoords.CoordPrecision = banx.PrecisionStreet
	if _, err := s.Query(context.Background(), withCoords); err != nil {
		t.Errorf("street precision: %v, want the report", err)
	}
}
