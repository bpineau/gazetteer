package iris

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded contours.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := idx.Count(); got < 4000 {
		t.Errorf("Count = %d, want ≥ 4000", got)
	}
}

// TestResolveIRIS resolves a known IDF coordinate to its IRIS (the resolver path
// the Normalizer uses).
func TestResolveIRIS(t *testing.T) {
	t.Parallel()
	s := NewSource(Options{})
	code, ok := s.ResolveIRIS(48.9355, 2.3590) // Basilique de Saint-Denis
	if !ok {
		t.Fatalf("ResolveIRIS returned ok=false for Saint-Denis")
	}
	if !strings.HasPrefix(code, "93066") || len(code) != 9 {
		t.Errorf("code = %q, want a 9-digit IRIS in commune 93066", code)
	}
	// Outside the IDF perimeter.
	if _, ok := s.ResolveIRIS(43.2965, 5.3698); ok { // Marseille
		t.Error("ResolveIRIS resolved a Marseille point (out of IDF)")
	}
}

// TestQuery_Geometry resolves coordinates to a full Result.
func TestQuery_Geometry(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: new(48.8627), Lon: new(2.4436)}) // Montreuil
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() || !strings.HasPrefix(res.CodeIRIS, "93048") {
		t.Errorf("CodeIRIS = %q, want a 93048 IRIS", res.CodeIRIS)
	}
	if res.TypIRIS == "" || res.Confidence != ConfidenceHigh || res.Evidence.Source != "geometry" {
		t.Errorf("typ/conf/source = %q/%q/%q", res.TypIRIS, res.Confidence, res.Evidence.Source)
	}
}

// TestQuery_ListingFastPath reuses a pre-resolved Listing.IRIS without a second
// point-in-polygon pass.
func TestQuery_ListingFastPath(t *testing.T) {
	t.Parallel()
	// First resolve a real code, then feed it back via Listing.IRIS.
	code, ok := NewSource(Options{}).ResolveIRIS(48.8627, 2.4436)
	if !ok {
		t.Fatalf("setup resolve failed")
	}
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{IRIS: code})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.CodeIRIS != code || res.Evidence.Source != "listing" {
		t.Errorf("fast path: code=%q source=%q, want %q/listing", res.CodeIRIS, res.Evidence.Source, code)
	}
	if res.NomIRIS == "" {
		t.Error("fast path should still populate NomIRIS via LookupCode")
	}
}

// TestQuery_CarriedCodeNoCoords surfaces a Listing.IRIS that was resolved
// elsewhere (outside this perimeter) even without coordinates, rather than
// dropping it on an insufficient-inputs error.
func TestQuery_CarriedCodeNoCoords(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{IRIS: "010010000"}) // Ain, not in IDF
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() || res.CodeIRIS != "010010000" {
		t.Errorf("CodeIRIS = %q, want the carried code 010010000", res.CodeIRIS)
	}
	if res.Evidence.Source != "listing" {
		t.Errorf("Evidence.Source = %q, want listing", res.Evidence.Source)
	}
}

// TestQuery_OutOfPerimeter returns empty outside Île-de-France.
func TestQuery_OutOfPerimeter(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{Lat: new(43.2965), Lon: new(5.3698)})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.IsEmpty() {
		t.Error("IsEmpty() = false, want true outside the IDF perimeter")
	}
}

// TestQuery_MissingCoords skips with neither a pre-resolved IRIS nor coordinates.
func TestQuery_MissingCoords(t *testing.T) {
	t.Parallel()
	if _, err := Query(context.Background(), Options{}, gazetteer.Listing{}); !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("err = %v, want ErrInsufficientInputs", err)
	}
}
