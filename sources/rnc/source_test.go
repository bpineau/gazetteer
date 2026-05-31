package rnc

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

func TestQuery_RequiresINSEE(t *testing.T) {
	t.Parallel()
	_, err := Query(context.Background(), Options{Index: NewIndexForTest(nil)}, gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("err = %v, want ErrInsufficientInputs", err)
	}
}

func TestQuery_NoMatch_IsEmpty(t *testing.T) {
	t.Parallel()
	r, err := Query(context.Background(), Options{Index: NewIndexForTest(nil)}, gazetteer.Listing{INSEE: "75102", Address: "1 rue X"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !r.IsEmpty() || r.Confidence != ConfidenceNone {
		t.Errorf("want empty, got %+v", r)
	}
}

// TestQuery_StubCoarseGranularity ensures two distinct buildings in ONE
// commune resolve to DIFFERENT copros (no commune-level folding).
func TestQuery_StubCoarseGranularity(t *testing.T) {
	t.Parallel()
	idx := NewIndexForTest([]Entry{
		{Immatriculation: "AA1", INSEE: "91286", Lat: 48.6550, Lon: 2.3850, VoieNorm: normVoie("rue des fleurs"), MandatEnCours: "Pas de mandat en cours", CoproAidee: true},
		{Immatriculation: "ZZ9", INSEE: "91286", Lat: 48.6700, Lon: 2.4000, VoieNorm: normVoie("rue victor hugo"), TypeSyndic: "professionnel", MandatEnCours: "Mandat en cours"},
	})
	r1, _ := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: "91286", Lat: f64(48.65501), Lon: f64(2.38501), Address: "2 rue des fleurs"})
	r2, _ := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: "91286", Lat: f64(48.67001), Lon: f64(2.40001), Address: "9 rue victor hugo"})
	if r1.Immatriculation == r2.Immatriculation {
		t.Fatalf("folding: both resolved to %q", r1.Immatriculation)
	}
	if !r1.Attention || r1.Immatriculation != "AA1" {
		t.Errorf("r1 = %+v, want AA1 + attention", r1)
	}
	if r2.Attention || r2.Immatriculation != "ZZ9" {
		t.Errorf("r2 = %+v, want ZZ9 + no attention", r2)
	}
}

// TestLoad_Embedded smokes the embedded artifact. With the committed
// placeholder it is empty; after `gazetteer refresh --go-embed-update rnc`
// it must be a large national dataset.
func TestLoad_Embedded(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if n := idx.Count(); n > 0 && n < 100000 {
		t.Errorf("Count = %d; a real RNC artifact should hold the national set (~600k)", n)
	}
}
