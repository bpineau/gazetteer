package gpe

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded catalog.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := idx.Count(); got < 60 {
		t.Errorf("Count = %d, want ~68 GPE stations", got)
	}
}

// TestQuery_NearStadeDeFrance checks a real coordinate near the future Stade
// de France station resolves to a close GPE station.
func TestQuery_NearStadeDeFrance(t *testing.T) {
	t.Parallel()
	// ~Saint-Denis Landy.
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: new(48.9197), Lon: new(2.3580)})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() || res.Nearest == nil {
		t.Fatalf("expected a nearby GPE station, got empty")
	}
	if res.Nearest.DistanceM > 3000 || res.Nearest.Line == "" {
		t.Errorf("nearest = %+v, want a close station with a line", res.Nearest)
	}
	if res.Within3000m < res.Within1500m {
		t.Errorf("within3000 (%d) < within1500 (%d) — impossible", res.Within3000m, res.Within1500m)
	}
}

// TestQuery_NoCoords requires coordinates.
func TestQuery_NoCoords(t *testing.T) {
	t.Parallel()
	_, err := Query(context.Background(), Options{}, gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("err = %v, want ErrInsufficientInputs", err)
	}
}

// TestQuery_FarAway returns empty beyond MaxRelevantMeters (Marseille).
func TestQuery_FarAway(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: new(43.2965), Lon: new(5.3698)})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.IsEmpty() {
		t.Errorf("res = %+v, want empty far from Île-de-France", res)
	}
}

// TestNearest_StubIndex pins the nearest + radius-count logic.
func TestNearest_StubIndex(t *testing.T) {
	t.Parallel()
	idx := &Index{Stations: []stationRec{
		{Code: "A", Name: "Near", Line: "L15", Lat: 48.9, Lon: 2.35},
		{Code: "B", Name: "Far", Line: "L16", Lat: 49.0, Lon: 2.50},
	}}
	st, w1500, w3000, ok := idx.nearest(48.9008, 2.3502) // ~100 m from A
	if !ok || st.Code != "A" || st.DistanceM > 300 {
		t.Errorf("nearest = %+v ok=%v, want station A within 300 m", st, ok)
	}
	if w1500 != 1 || w3000 != 1 {
		t.Errorf("counts within1500=%d within3000=%d, want 1/1 (B is ~17 km away)", w1500, w3000)
	}
}
