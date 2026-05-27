package pinel

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded dataset.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx == nil {
		t.Fatalf("nil index")
	}
	if got := idx.Count(); got < 30_000 {
		t.Errorf("Count = %d, want ≥ 30 000", got)
	}
}

// TestQuery_HappyPath_Paris11 pins a known high-tension commune.
func TestQuery_HappyPath_Paris11(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "75111"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Paris 11e")
	}
	if res.Zone != ZoneAbis {
		t.Errorf("Zone = %q, want %q for Paris 11e", res.Zone, ZoneAbis)
	}
	if !res.PinelEligible {
		t.Errorf("PinelEligible = false, want true for Abis")
	}
	if res.TensionLabel != "very_high" {
		t.Errorf("TensionLabel = %q, want %q", res.TensionLabel, "very_high")
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if res.Evidence.CommuneLabel == "" {
		t.Errorf("Evidence.CommuneLabel empty, want populated")
	}
	if res.Evidence.RowCount < 30_000 {
		t.Errorf("Evidence.RowCount = %d, want ≥ 30 000", res.Evidence.RowCount)
	}
}

// TestQuery_GoldenZones pins zonage for a handful of reference
// communes covering the five buckets.
func TestQuery_GoldenZones(t *testing.T) {
	t.Parallel()
	cases := []struct {
		insee    string
		wantZone Zone
		wantElig bool
	}{
		{"75101", ZoneAbis, true}, // Paris 1er
		{"69123", ZoneA, true},    // Lyon — A
		{"33063", ZoneB1, true},   // Bordeaux — B1
		{"35238", ZoneB1, true},   // Rennes — B1
		{"99999", ZoneUnknown, false},
	}
	for _, c := range cases {
		l := gazetteer.Listing{INSEE: c.insee}
		res, err := Query(context.Background(), Options{}, l)
		if err != nil {
			t.Errorf("insee %s: Query: %v", c.insee, err)
			continue
		}
		if res == nil {
			t.Errorf("insee %s: nil result", c.insee)
			continue
		}
		if res.Zone != c.wantZone {
			t.Errorf("insee %s: Zone = %q, want %q", c.insee, res.Zone, c.wantZone)
		}
		if res.PinelEligible != c.wantElig {
			t.Errorf("insee %s: PinelEligible = %v, want %v", c.insee, res.PinelEligible, c.wantElig)
		}
		if c.wantZone == ZoneUnknown && !res.IsEmpty() {
			t.Errorf("insee %s: IsEmpty = false, want true (synthetic INSEE)", c.insee)
		}
	}
}

// TestQuery_InsufficientInputs rejects an empty INSEE.
func TestQuery_InsufficientInputs(t *testing.T) {
	t.Parallel()
	_, err := Query(context.Background(), Options{}, gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("err = %v, want ErrInsufficientInputs", err)
	}
}

// TestNormaliseZone pins the upstream-label normalisation logic.
func TestNormaliseZone(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want Zone
	}{
		{"A", ZoneA},
		{" A", ZoneA},
		{"A bis", ZoneAbis},
		{"Abis", ZoneAbis},
		{"B1", ZoneB1},
		{"B2", ZoneB2},
		{"C", ZoneC},
		{"unknown", ZoneUnknown},
		{"", ZoneUnknown},
	}
	for _, c := range cases {
		if got := normaliseZone(c.in); got != c.want {
			t.Errorf("normaliseZone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestPinelEligibility verifies the four-bucket eligibility rule.
func TestPinelEligibility(t *testing.T) {
	t.Parallel()
	cases := map[Zone]bool{
		ZoneAbis:    true,
		ZoneA:       true,
		ZoneB1:      true,
		ZoneB2:      false,
		ZoneC:       false,
		ZoneUnknown: false,
	}
	for z, want := range cases {
		if got := pinelEligible(z); got != want {
			t.Errorf("pinelEligible(%q) = %v, want %v", z, got, want)
		}
	}
}

// TestSourceRegistered ensures the init() side-effect wired the
// gazetteer registry.
func TestSourceRegistered(t *testing.T) {
	t.Parallel()
	if got := gazetteer.Lookup(Name); got == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil, want factory", Name)
	}
}
