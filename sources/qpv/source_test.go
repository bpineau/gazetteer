package qpv

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
	if got := idx.Count(); got < 700 || got > 1500 {
		t.Errorf("Count = %d, want in [700, 1500]", got)
	}
	if idx.Meta.RowCountQPV < 1400 {
		t.Errorf("RowCountQPV = %d, want ≥ 1400", idx.Meta.RowCountQPV)
	}
}

// TestQuery_QPV_SaintDenis pins a known multi-QPV commune.
func TestQuery_QPV_SaintDenis(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "93066"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Saint-Denis (93066)")
	}
	if !res.HasQPV {
		t.Errorf("HasQPV = false, want true")
	}
	if res.QPVCount < 3 {
		t.Errorf("QPVCount = %d, want ≥ 3", res.QPVCount)
	}
	if len(res.QPVs) != res.QPVCount {
		t.Errorf("len(QPVs) = %d != QPVCount %d", len(res.QPVs), res.QPVCount)
	}
	for _, q := range res.QPVs {
		if q.Code == "" {
			t.Errorf("QPV with empty Code: %+v", q)
		}
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
}

// TestQuery_NoQPV_NeuillySurSeine pins a commune known to host no QPV.
// Neuilly-sur-Seine (92051) is a high-income inner-ring commune with
// no priority neighbourhood.
func TestQuery_NoQPV_NeuillySurSeine(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "92051"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result")
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty = false, want true (Neuilly hosts no QPV)")
	}
	if res.HasQPV {
		t.Errorf("HasQPV = true, want false")
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

// TestSourceRegistered ensures the init() side-effect wired the
// gazetteer registry.
func TestSourceRegistered(t *testing.T) {
	t.Parallel()
	if got := gazetteer.Lookup(Name); got == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil, want factory", Name)
	}
}
