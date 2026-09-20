package gpe

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestQuery_NullIslandSentinel pins the (0, 0) guard every other spatial
// Source already had. Lat/Lon pointers TO ZERO are how "no coordinates"
// survives a JSON round-trip or a struct built from a partially-filled
// record, and gpe used to take them at face value: it measured from Null
// Island, found no station within MaxRelevantMeters, and answered
// IsEmpty() - "no future Grand Paris Express station near this address",
// about an address it never located.
//
// gazetteer.Listing.Coords is the canonical test and now answers it.
func TestQuery_NullIslandSentinel(t *testing.T) {
	t.Parallel()
	zero := 0.0
	_, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: &zero, Lon: &zero})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(0, 0) err = %v, want ErrInsufficientInputs", err)
	}
}
