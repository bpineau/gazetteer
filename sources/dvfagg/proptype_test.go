package dvfagg

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// The aggregate is an APARTMENT median. Answering a house or a shop with it
// fed appraisal.PricePerM2 the flats' price as a second reading of the
// house's: with dvf's Maison median at 3 500 EUR/m2 and the commune's flats at
// 5 000, the blend came out at 3 962, 13 % above every house sale in the
// cohort. A non-apartment listing is now refused; an unknown type keeps the
// documented apartment default.
func TestSource_NonApartmentRefused(t *testing.T) {
	t.Parallel()
	s := NewSource(Options{})
	for _, pt := range []gazetteer.PropertyType{gazetteer.PropertyHouse, gazetteer.PropertyCommercial, gazetteer.PropertyLand} {
		_, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "95268", PropertyType: pt})
		if !errors.Is(err, gazetteer.ErrUnsupportedPropertyType) {
			t.Errorf("%q: err = %v, want ErrUnsupportedPropertyType", pt, err)
		}
	}
	for _, pt := range []gazetteer.PropertyType{gazetteer.PropertyApartment, gazetteer.PropertyUnknown} {
		if _, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "95268", PropertyType: pt}); err != nil {
			t.Errorf("%q: err = %v, want the aggregate", pt, err)
		}
	}
}
