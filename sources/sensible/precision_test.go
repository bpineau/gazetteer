package sensible

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/geopoly"
)

// TestQuery_NullIslandSentinel pins the (0, 0) guard every other spatial
// Source already had. Lat/Lon pointers TO ZERO are how "no coordinates"
// survives a JSON round-trip, and this Source used to take them at face
// value: it measured from Null Island, found no perimeter within
// NearbyMeters and answered IsEmpty() - "not in or near a sensitive
// zone", about an address it never located.
func TestQuery_NullIslandSentinel(t *testing.T) {
	zero := 0.0
	idx := NewIndexForTest(map[string]geopoly.MultiPolygon{"Zone Test": square(48.94, 2.52)})
	_, err := NewSource(Options{Index: idx}).Query(context.Background(),
		gazetteer.Listing{Lat: &zero, Lon: &zero})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(0, 0) err = %v, want ErrInsufficientInputs", err)
	}
}

// TestQuery_RefusesCoarseCoords covers the other way to get a confident
// answer about the wrong point: coordinates that are real, but are the
// commune's centre rather than the address's. BAN returns those for any
// address it cannot find, and the mairie is either inside a QRR or not,
// with nothing on the Result to say the question was never asked.
func TestQuery_RefusesCoarseCoords(t *testing.T) {
	idx := NewIndexForTest(map[string]geopoly.MultiPolygon{"Zone Test": square(48.94, 2.52)})
	cases := []struct {
		precision banx.Precision
		wantErr   bool
	}{
		{banx.PrecisionMunicipality, true},
		{banx.PrecisionLocality, true},
		{banx.PrecisionStreet, false},
		{banx.PrecisionHouseNumber, false},
		{banx.PrecisionUnknown, false}, // unreported is not coarse
	}
	for _, tc := range cases {
		t.Run(string(tc.precision)+"_", func(t *testing.T) {
			l := gazetteer.Listing{Lat: ptr(48.94), Lon: ptr(2.52), CoordPrecision: tc.precision}
			r, err := NewSource(Options{Index: idx}).QueryResult(context.Background(), l)
			if tc.wantErr {
				if !errors.Is(err, banx.ErrCoarseMatch) {
					t.Fatalf("err = %v, want ErrCoarseMatch", err)
				}
				if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
					t.Errorf("err = %v, want it to also classify as ErrInsufficientInputs", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if !r.Sensitive {
				t.Errorf("Sensitive = false, want the point inside Zone Test")
			}
		})
	}
}
