package nuisances

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded grid.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := idx.Count(); got < 40000 {
		t.Errorf("Count = %d, want ≥ 40000", got)
	}
}

// TestQuery_Paris resolves a central-Paris point to a populated cell.
func TestQuery_Paris(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: new(48.8566), Lon: new(2.3522)})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() {
		t.Fatalf("empty result for central Paris")
	}
	if res.NuisanceCount < 0 || res.NuisanceCount > 4 {
		t.Errorf("NuisanceCount = %d, want 0..4", res.NuisanceCount)
	}
	if res.Tier == "" || res.Confidence != ConfidenceHigh {
		t.Errorf("tier/confidence = %q/%q", res.Tier, res.Confidence)
	}
	if res.Evidence.CellDistanceM > int(MaxCellMeters) {
		t.Errorf("cell distance %d > cap", res.Evidence.CellDistanceM)
	}
}

// TestQuery_CalmCellNotEmpty verifies a resolved count-0 cell is reported (a
// real "no nuisance" reading), not treated as empty.
func TestQuery_CalmCellNotEmpty(t *testing.T) {
	t.Parallel()
	idx := &Index{buckets: map[bucketKey][]cell{}}
	c := cell{Lat: 48.5, Lon: 2.5, Nuis: 0}
	idx.buckets[keyFor(c.Lat, c.Lon)] = []cell{c}
	idx.n = 1
	res, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{Lat: new(48.5), Lon: new(2.5)})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() {
		t.Error("a resolved calm cell must not be empty")
	}
	if res.Tier != TierCalme {
		t.Errorf("Tier = %q, want %q", res.Tier, TierCalme)
	}
}

// TestQuery_BucketBoundary covers the risky path: the nearest cell centre sits
// in a bucket ADJACENT to the query's, and a near-miss just beyond MaxCellMeters
// must not resolve.
func TestQuery_BucketBoundary(t *testing.T) {
	t.Parallel()
	c := cell{Lat: 48.7995, Lon: 2.5, Nuis: 2} // bucket 4879 (48.79..48.80)
	idx := &Index{buckets: map[bucketKey][]cell{keyFor(c.Lat, c.Lon): {c}}, n: 1}

	// ~111 m north, across the 48.80 bucket boundary (query bucket 4880) → hit.
	hit, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{Lat: new(48.8005), Lon: new(2.5)})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if hit.IsEmpty() || hit.NuisanceCount != 2 {
		t.Errorf("cross-bucket hit failed: empty=%v count=%d", hit.IsEmpty(), hit.NuisanceCount)
	}

	// ~450 m north (> MaxCellMeters 400 m) → no resolution.
	miss, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{Lat: new(48.80355), Lon: new(2.5)})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !miss.IsEmpty() {
		t.Errorf("near-miss at ~450 m resolved, want empty (cap %v m)", MaxCellMeters)
	}
}

// TestQuery_OutOfGrid returns empty well outside Île-de-France.
func TestQuery_OutOfGrid(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: new(43.2965), Lon: new(5.3698)}) // Marseille
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.IsEmpty() {
		t.Error("IsEmpty() = false, want true outside the IDF grid")
	}
}

// TestQuery_MissingCoords skips without coordinates.
func TestQuery_MissingCoords(t *testing.T) {
	t.Parallel()
	for _, l := range []gazetteer.Listing{{}, {Lat: new(0.0), Lon: new(0.0)}} {
		if _, err := Query(context.Background(), Options{}, l); !errors.Is(err, gazetteer.ErrInsufficientInputs) {
			t.Errorf("Query(%+v) err = %v, want ErrInsufficientInputs", l, err)
		}
	}
}
