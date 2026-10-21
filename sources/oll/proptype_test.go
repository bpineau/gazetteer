package oll

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// The embedded OLL cells are APARTMENT rents (type_habitat "Appartement"; the
// transform keeps nothing else), but a house listing was answered with them,
// and appraisal.RentValue blended the flats' rent per m2 into the house's at
// weight 0.95, next to carteloyers' own house typology. A house is now
// refused; apartments are unchanged.
func TestQuery_HouseRefused(t *testing.T) {
	t.Parallel()
	_, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "93066", PropertyType: gazetteer.PropertyHouse, Rooms: new(4)})
	if !errors.Is(err, gazetteer.ErrUnsupportedPropertyType) {
		t.Errorf("house: err = %v, want ErrUnsupportedPropertyType", err)
	}
	if res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "93066", PropertyType: gazetteer.PropertyApartment, Rooms: new(4)}); err != nil || res.IsEmpty() {
		t.Errorf("apartment: %v, want a reading", err)
	}
}
