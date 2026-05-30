package cartofriches

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
	if got := idx.Count(); got < 5000 {
		t.Errorf("Count = %d, want ≥ 5000", got)
	}
	if idx.Meta.RowCountSites < 20_000 {
		t.Errorf("RowCountSites = %d, want ≥ 20 000", idx.Meta.RowCountSites)
	}
}

// TestQuery_HighCount pins Marseille (13055) — known multi-site
// commune.
func TestQuery_HighCount(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "13055"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Marseille (13055)")
	}
	if res.SiteCount < 50 {
		t.Errorf("SiteCount = %d, want ≥ 50", res.SiteCount)
	}
	if len(res.ByType) == 0 {
		t.Errorf("ByType empty, want at least one type")
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if res.Evidence.CommuneLabel == "" {
		t.Errorf("Evidence.CommuneLabel empty, want populated")
	}
}

// TestQuery_LowCount exercises a rural single-site commune.
func TestQuery_LowCount(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "01135"} // Crozet — 1 friche carrière.
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Crozet (01135)")
	}
	if res.SiteCount != 1 {
		t.Errorf("SiteCount = %d, want 1", res.SiteCount)
	}
	if got := res.ByType["friche carrière ou mine"]; got != 1 {
		t.Errorf("ByType[carrière] = %d, want 1", got)
	}
}

// TestQuery_NoFriche covers a commune that hosts no friche.
func TestQuery_NoFriche(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "99999"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result")
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty = false, want true")
	}
	if res.SiteCount != 0 {
		t.Errorf("SiteCount = %d, want 0", res.SiteCount)
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

// TestSourceRegistered ensures the init() side-effect wired the
// gazetteer registry.
func TestSourceRegistered(t *testing.T) {
	t.Parallel()
	if got := gazetteer.Lookup(Name); got == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil, want factory", Name)
	}
}
