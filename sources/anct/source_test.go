package anct

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded dataset.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx == nil {
		t.Fatalf("nil index")
	}
	if got := idx.Count(); got < 1500 {
		t.Errorf("Count = %d, want ≥ 1500", got)
	}
	if idx.Meta.RowCountACV < 200 || idx.Meta.RowCountACV > 500 {
		t.Errorf("RowCountACV = %d, want in [200, 500]", idx.Meta.RowCountACV)
	}
	if idx.Meta.RowCountPVD < 1500 {
		t.Errorf("RowCountPVD = %d, want ≥ 1500", idx.Meta.RowCountPVD)
	}
}

// TestQuery_ACV_and_ORT pins Bourg-en-Bresse — Action Cœur de Ville
// + ORT signatory, no PVD.
func TestQuery_ACV_and_ORT(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "01053"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for 01053 Bourg-en-Bresse")
	}
	if !res.ACV {
		t.Errorf("ACV = false, want true")
	}
	if !res.ORT {
		t.Errorf("ORT = false, want true")
	}
	if !res.DenormandieEligible {
		t.Errorf("DenormandieEligible = false, want true (ORT signatory)")
	}
	if res.ACVSignedAt == "" {
		t.Errorf("ACVSignedAt empty, want populated")
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if len(res.Programmes) < 2 {
		t.Errorf("Programmes = %v, want at least 2 entries", res.Programmes)
	}
}

// TestQuery_PVD_only pins Languidic — PVD with no ACV.
func TestQuery_PVD_only(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "56101"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for 56101 Languidic")
	}
	if res.ACV {
		t.Errorf("ACV = true, want false")
	}
	if !res.PVD {
		t.Errorf("PVD = false, want true")
	}
	if res.PVDSignedAt == "" {
		t.Errorf("PVDSignedAt empty, want populated")
	}
}

// TestQuery_UnknownCommune returns IsEmpty (vast majority of communes
// are not in the dataset).
func TestQuery_UnknownCommune(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "75056"} // Paris — not in any programme.
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result, want non-nil empty")
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty = false, want true (Paris is not ACV/PVD/ORT)")
	}
	if res.Confidence != ConfidenceNone {
		t.Errorf("Confidence = %q, want empty", res.Confidence)
	}
}

// TestQuery_InsufficientInputs rejects empty INSEE.
func TestQuery_InsufficientInputs(t *testing.T) {
	t.Parallel()
	_, err := Query(context.Background(), Options{}, gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("err = %v, want ErrInsufficientInputs", err)
	}
}

// TestProgrammeList pins the rendering helper.
func TestProgrammeList(t *testing.T) {
	t.Parallel()
	cases := []struct {
		acv, pvd, ort bool
		want          []string
	}{
		{false, false, false, nil},
		{true, false, false, []string{"acv"}},
		{false, true, false, []string{"pvd"}},
		{false, false, true, []string{"ort"}},
		{true, true, true, []string{"acv", "pvd", "ort"}},
	}
	for _, c := range cases {
		got := programmeList(c.acv, c.pvd, c.ort)
		if !slicesEqual(got, c.want) {
			t.Errorf("programmeList(%v,%v,%v) = %v, want %v", c.acv, c.pvd, c.ort, got, c.want)
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

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
