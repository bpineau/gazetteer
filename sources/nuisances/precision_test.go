package nuisances

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
)

// A commune centre is a real 500 m cell with a real nuisance count: the
// mairie's, not the address's. It must be refused, not read as the dwelling.
func TestQuery_CoarseCoordinatesRefused(t *testing.T) {
	t.Parallel()
	for _, p := range []banx.Precision{banx.PrecisionMunicipality, banx.PrecisionLocality} {
		_, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: new(48.8566), Lon: new(2.3522), CoordPrecision: p})
		if !errors.Is(err, gazetteer.ErrInsufficientInputs) || !errors.Is(err, banx.ErrCoarseMatch) {
			t.Errorf("precision %q: err = %v, want ErrInsufficientInputs + ErrCoarseMatch", p, err)
		}
	}
	if _, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: new(48.8566), Lon: new(2.3522), CoordPrecision: banx.PrecisionStreet}); err != nil {
		t.Errorf("street precision: %v, want a reading", err)
	}
}
