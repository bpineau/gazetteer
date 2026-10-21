package cdsr

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
)

// A commune centre sits near whatever copro neighbours the mairie: "a
// distressed copro 40 m away" would describe the town hall, not the address.
func TestQuery_CoarseCoordinatesRefused(t *testing.T) {
	t.Parallel()
	for _, p := range []banx.Precision{banx.PrecisionMunicipality, banx.PrecisionLocality} {
		_, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: new(48.907676), Lon: new(2.491884), CoordPrecision: p})
		if !errors.Is(err, gazetteer.ErrInsufficientInputs) || !errors.Is(err, banx.ErrCoarseMatch) {
			t.Errorf("precision %q: err = %v, want ErrInsufficientInputs + ErrCoarseMatch", p, err)
		}
	}
	if res, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: new(48.907676), Lon: new(2.491884), CoordPrecision: banx.PrecisionStreet}); err != nil || res.IsEmpty() {
		t.Errorf("street precision: %v, want the Bondy copro", err)
	}
}
