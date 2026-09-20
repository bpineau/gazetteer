package qpv

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestNearest_LongEdge is the regression for the vertex-vs-edge distance.
// QPV QN97329M (Guyane) has a 3 961 m boundary edge; a point 30 m outside it
// is 1 981 m from the nearest vertex, so the old vertex reading fell outside
// the 1 000 m window and the Source reported no QPV nearby at all about a
// perimeter 30 m away.
func TestNearest_LongEdge(t *testing.T) {
	t.Parallel()
	lat, lon := 5.475543, -53.950032
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{
		INSEE: "97311", Lat: &lat, Lon: &lon,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.HasQPV {
		t.Fatalf("point is outside every QPV, got HasQPV = true")
	}
	if res.NearestCode != "QN97329M" {
		t.Errorf("NearestCode = %q, want QN97329M (its boundary is ~30 m away)", res.NearestCode)
	}
	if res.NearestMeters <= 0 || res.NearestMeters > 150 {
		t.Errorf("NearestMeters = %.0f, want the real edge distance (~30), not a vertex reading", res.NearestMeters)
	}
}
