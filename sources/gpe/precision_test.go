package gpe

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
)

// The distance to the nearest future station is measured from the address; a
// commune centre would measure it from the mairie.
func TestQuery_CoarseCoordinatesRefused(t *testing.T) {
	t.Parallel()
	for _, p := range []banx.Precision{banx.PrecisionMunicipality, banx.PrecisionLocality} {
		_, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: new(48.9197), Lon: new(2.3580), CoordPrecision: p})
		if !errors.Is(err, gazetteer.ErrInsufficientInputs) || !errors.Is(err, banx.ErrCoarseMatch) {
			t.Errorf("precision %q: err = %v, want ErrInsufficientInputs + ErrCoarseMatch", p, err)
		}
	}
	if res, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: new(48.9197), Lon: new(2.3580), CoordPrecision: banx.PrecisionStreet}); err != nil || res.IsEmpty() {
		t.Errorf("street precision: %v, want a station", err)
	}
}
