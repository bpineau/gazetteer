package taxefonciere

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/bpineau/gazetteer"
)

// TestLoad smokes the embedded datasets.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx == nil || idx.V1 == nil || idx.V2 == nil {
		t.Fatalf("nil index components: %+v", idx)
	}
	if got := idx.V2.CountCommunesV2(); got < 30000 {
		t.Errorf("CountCommunesV2 = %d, want ≥ 30 000", got)
	}
	if got := idx.V1.CountCommunesV1(); got < 5000 {
		t.Errorf("CountCommunesV1 = %d, want ≥ 5 000", got)
	}
	if idx.V2.Meta.VLCTariffEURPerM2 <= 0 {
		t.Errorf("VLCTariffEURPerM2 = %.2f, want > 0", idx.V2.Meta.VLCTariffEURPerM2)
	}
	if idx.V2.Meta.VLCAbattement <= 0 || idx.V2.Meta.VLCAbattement > 1 {
		t.Errorf("VLCAbattement = %.2f, want in (0,1]", idx.V2.Meta.VLCAbattement)
	}
}

// TestQuery_V2HappyPath exercises the V2 commune-hit happy path.
func TestQuery_V2HappyPath(t *testing.T) {
	t.Parallel()
	surf := 60.0
	l := gazetteer.Listing{
		INSEE:        "91228", // Évry-Courcouronnes.
		SurfaceM2:    &surf,
		PropertyType: gazetteer.PropertyApartment,
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Évry-Courcouronnes 60 m²")
	}
	if res.UsedV1Fallback {
		t.Fatalf("unexpected V1 fallback")
	}
	if res.EstimatedEURPerYear < 500 || res.EstimatedEURPerYear > 4000 {
		t.Errorf("EstimatedEURPerYear = %.0f, want in [500, 4000]", res.EstimatedEURPerYear)
	}
	if res.TEOMEURPerYear < 50 || res.TEOMEURPerYear > 1000 {
		t.Errorf("TEOMEURPerYear = %.0f, want in [50, 1000]", res.TEOMEURPerYear)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if res.Evidence.PathUsed != "v2_commune" {
		t.Errorf("PathUsed = %q, want v2_commune", res.Evidence.PathUsed)
	}
}

// TestQuery_V2DeptFallback exercises the dept fallback when the
// commune row is missing in V2. Synthetic INSEE in dept 91 (Essonne)
// should still resolve via the V2 dept median.
func TestQuery_V2DeptFallback(t *testing.T) {
	t.Parallel()
	idx, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Find a (synthetic) INSEE NOT in V2 communes but where dept is
	// covered by the V2 fallback. We pick a fictional Essonne INSEE.
	syntheticINSEE := "91999"
	if _, hit := idx.V2.Communes[syntheticINSEE]; hit {
		t.Skip("91999 actually exists in V2 communes")
	}
	if _, hit := idx.V2.DeptFallback["91"]; !hit {
		t.Skip("V2 dept fallback missing dept 91")
	}
	surf := 50.0
	l := gazetteer.Listing{INSEE: syntheticINSEE, SurfaceM2: &surf}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result, want dept fallback")
	}
	if !res.UsedDeptFallback {
		t.Errorf("UsedDeptFallback = false, want true")
	}
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceMedium)
	}
	if res.Evidence.PathUsed != "v2_dept" {
		t.Errorf("PathUsed = %q, want v2_dept", res.Evidence.PathUsed)
	}
}

// TestQuery_NoneFound returns a none-confidence result for a synthetic
// INSEE where even the dept lookup misses.
func TestQuery_NoneFound(t *testing.T) {
	t.Parallel()
	idx, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 99xxx is not a real dept in the V2 + V1 indexes.
	syntheticINSEE := "99999"
	if _, hit := idx.V2.Communes[syntheticINSEE]; hit {
		t.Skip("99999 unexpectedly in V2")
	}
	if _, hit := idx.V2.DeptFallback["99"]; hit {
		t.Skip("dept 99 in V2 fallback — adjust test fixture")
	}
	surf := 50.0
	l := gazetteer.Listing{INSEE: syntheticINSEE, SurfaceM2: &surf}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result, want non-nil empty result")
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty = false, want true for synthetic INSEE")
	}
	if res.Confidence != ConfidenceNone {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceNone)
	}
}

// TestQuery_InsufficientInputs rejects empty INSEE / zero surface.
func TestQuery_InsufficientInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		l    gazetteer.Listing
	}{
		{"empty INSEE", gazetteer.Listing{}},
		{"zero surface", gazetteer.Listing{INSEE: "91228"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Query(context.Background(), Options{}, c.l)
			if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
				t.Fatalf("err = %v, want ErrInsufficientInputs", err)
			}
		})
	}
}

// TestQuery_FiniteOutput guards against non-finite outputs.
func TestQuery_FiniteOutput(t *testing.T) {
	t.Parallel()
	surf := 60.0
	l := gazetteer.Listing{INSEE: "91228", SurfaceM2: &surf}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if math.IsNaN(res.EstimatedEURPerYear) || math.IsInf(res.EstimatedEURPerYear, 0) {
		t.Errorf("non-finite EstimatedEURPerYear: %v", res.EstimatedEURPerYear)
	}
	if math.IsNaN(res.TEOMEURPerYear) || math.IsInf(res.TEOMEURPerYear, 0) {
		t.Errorf("non-finite TEOMEURPerYear: %v", res.TEOMEURPerYear)
	}
}

// TestDeptFromInsee pins the dept-extraction logic for métropole +
// Corsica + DOM-TOM.
func TestDeptFromInsee(t *testing.T) {
	t.Parallel()
	cases := []struct {
		insee string
		want  string
	}{
		{"75056", "75"},
		{"91228", "91"},
		{"2A004", "2A"},
		{"2B001", "2B"},
		{"97411", "974"},
		{"98801", "988"},
		{"", ""},
		{"1", ""},
	}
	for _, c := range cases {
		if got := deptFromInsee(c.insee); got != c.want {
			t.Errorf("deptFromInsee(%q) = %q, want %q", c.insee, got, c.want)
		}
	}
}
