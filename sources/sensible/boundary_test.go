package sensible

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestNearby_LongEdge is the regression for the vertex-vs-edge distance. The
// Lunel/Mauguio QRR has a 2 234 m boundary edge; a point 50 m outside it is
// 1 100 m from the nearest vertex, so the old vertex reading placed it outside
// the 400 m NearbyMeters window and the Source answered "no sensitive zone
// nearby" about a perimeter 50 m away.
func TestNearby_LongEdge(t *testing.T) {
	t.Parallel()
	lat, lon := 43.576881, 4.061569
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: &lat, Lon: &lon})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.In) != 0 {
		t.Fatalf("point is outside every perimeter, got In = %+v", res.In)
	}
	var hit *Zone
	for i := range res.Nearby {
		if res.Nearby[i].Name == "Lunel/Mauguio" {
			hit = &res.Nearby[i]
		}
	}
	if hit == nil {
		t.Fatalf("Lunel/Mauguio absent from Nearby = %+v; it is ~50 m away", res.Nearby)
	}
	if hit.DistanceM <= 0 || hit.DistanceM > 150 {
		t.Errorf("DistanceM = %d m, want the real edge distance (~50), not a vertex reading", hit.DistanceM)
	}
}
