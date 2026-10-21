package osm

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
)

// A commune centre is usually its best-served point: measuring the walk to
// the nearest station from the mairie reads the town hall's access as the
// address's. It must be refused; a street centroid still reads.
func TestQuery_CoarseCoordinatesRefused(t *testing.T) {
	s := NewSource(Options{})
	lat, lon := 48.8583, 2.3470
	for _, p := range []banx.Precision{banx.PrecisionMunicipality, banx.PrecisionLocality} {
		_, err := s.QueryResult(context.Background(), gazetteer.Listing{Lat: &lat, Lon: &lon, CoordPrecision: p})
		if !errors.Is(err, gazetteer.ErrInsufficientInputs) || !errors.Is(err, banx.ErrCoarseMatch) {
			t.Errorf("precision %q: err = %v, want ErrInsufficientInputs + ErrCoarseMatch", p, err)
		}
	}
	if r, err := s.QueryResult(context.Background(), gazetteer.Listing{Lat: &lat, Lon: &lon, CoordPrecision: banx.PrecisionStreet}); err != nil || r.IsEmpty() {
		t.Errorf("street precision: %v, want a station", err)
	}
}
