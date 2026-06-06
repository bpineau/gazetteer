package qpv

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestIndex_HasQPV verifies the exported commune-level boolean accessor.
func TestIndex_HasQPV(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 93066 = Saint-Denis: multiple QPVs
	if !idx.HasQPV("93066") {
		t.Fatal("HasQPV(93066) = false, want true (Saint-Denis has QPVs)")
	}
	// 75016 = Paris 16e: no QPV
	if idx.HasQPV("75016") {
		t.Fatal("HasQPV(75016) = true, want false (Paris 16e has no QPV)")
	}
}

// TestLoad smokes the embedded contours artifact.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx == nil {
		t.Fatalf("nil index")
	}
	if got := idx.PolygonCount(); got < 1400 || got > 2500 {
		t.Errorf("PolygonCount = %d, want in [1400, 2500]", got)
	}
	if got := idx.CommuneCount(); got < 700 || got > 1500 {
		t.Errorf("CommuneCount = %d, want in [700, 1500]", got)
	}
}

// TestQuery_RealParis10e_Regression is THE bug, on the real embedded data:
// a Paris 10e address sits in no QPV → HasQPV must be false.
func TestQuery_RealParis10e_Regression(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "75110", Lat: ptr(48.873128), Lon: ptr(2.353599)}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.HasQPV {
		t.Fatalf("HasQPV = true, want false (Paris 10e is not in a QPV); QPVs=%v", res.QPVs)
	}
	if res.MatchLevel != MatchLevelPoint {
		t.Errorf("MatchLevel = %q, want %q", res.MatchLevel, MatchLevelPoint)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
}

// TestQuery_RealInsideGoutteDOr proves a known true-positive containment:
// a point in La Goutte d'Or (Paris 18e) resolves inside that QPV.
func TestQuery_RealInsideGoutteDOr(t *testing.T) {
	t.Parallel()
	// Verified interior point of the real Goutte d'Or polygon (QN07511M).
	l := gazetteer.Listing{INSEE: "75118", Lat: ptr(48.88699), Lon: ptr(2.35313)}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.HasQPV {
		t.Fatalf("HasQPV = false, want true (point is inside La Goutte d'Or)")
	}
	if res.MatchLevel != MatchLevelPoint {
		t.Errorf("MatchLevel = %q, want %q", res.MatchLevel, MatchLevelPoint)
	}
	if res.QPVCount != 1 {
		t.Errorf("QPVCount = %d, want 1", res.QPVCount)
	}
	if len(res.QPVs) == 0 || res.QPVs[0].Code == "" {
		t.Fatalf("missing QPV code: %+v", res.QPVs)
	}
}

// TestQuery_RealCommuneFallback proves the embedded commune index answers
// the no-coordinates path for Saint-Denis (93066).
func TestQuery_RealCommuneFallback(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "93066"} // no coords
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.HasQPV || res.MatchLevel != MatchLevelCommune {
		t.Fatalf("got HasQPV=%t MatchLevel=%q, want true/commune", res.HasQPV, res.MatchLevel)
	}
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceMedium)
	}
	if res.QPVCount < 3 {
		t.Errorf("QPVCount = %d, want >= 3", res.QPVCount)
	}
}
