package qpv

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
)

// TestQuery_CoarseCoordsTakeTheCommunePath pins the precision floor on
// the point-in-polygon path.
//
// BAN answers every query it can parse, so an address it cannot find
// comes back as its commune's CENTRE, with a coordinate that looks like
// any other. Run point-in-polygon on that and the Source answers about
// the mairie, at MatchLevelPoint / ConfidenceHigh - a confident,
// precise-looking reading of a point nobody asked about.
//
// The fix is not to refuse: a commune-level coordinate deserves the
// commune-level answer the Source already knows how to give. It just has
// to be labelled MatchLevelCommune, which is what a consumer reads to
// decide how much to trust it.
func TestQuery_CoarseCoordsTakeTheCommunePath(t *testing.T) {
	t.Parallel()
	// A point INSIDE the Goutte d'Or square, so the two paths are
	// distinguishable only by their labels, not by the answer's content.
	inside := func(p banx.Precision) gazetteer.Listing {
		return gazetteer.Listing{INSEE: "75056", Lat: ptr(48.885), Lon: ptr(2.35), CoordPrecision: p}
	}
	cases := []struct {
		precision banx.Precision
		wantLevel string
		wantConf  string
	}{
		{banx.PrecisionMunicipality, MatchLevelCommune, ConfidenceMedium},
		{banx.PrecisionLocality, MatchLevelCommune, ConfidenceMedium},
		{banx.PrecisionStreet, MatchLevelPoint, ConfidenceHigh},
		{banx.PrecisionHouseNumber, MatchLevelPoint, ConfidenceHigh},
		{banx.PrecisionUnknown, MatchLevelPoint, ConfidenceHigh}, // unreported is not coarse
	}
	for _, tc := range cases {
		t.Run(string(tc.precision)+"_", func(t *testing.T) {
			res, err := Query(context.Background(), Options{Index: testIndex()}, inside(tc.precision))
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if res.MatchLevel != tc.wantLevel {
				t.Errorf("MatchLevel = %q, want %q", res.MatchLevel, tc.wantLevel)
			}
			if res.Confidence != tc.wantConf {
				t.Errorf("Confidence = %q, want %q", res.Confidence, tc.wantConf)
			}
		})
	}
}
