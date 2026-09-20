package dvfagg

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

func TestSource_Basics(t *testing.T) {
	s := NewSource(Options{})
	if s.Name() != Name || s.Version() != Version {
		t.Fatalf("identity mismatch: %q v%d", s.Name(), s.Version())
	}
	sets := s.Datasets()
	if len(sets) != 1 || sets[0].Source != Name || sets[0].Processed.Name != embedName {
		t.Fatalf("Datasets() wrong: %+v", sets)
	}
}

func TestSource_QueryKnownCommune(t *testing.T) {
	s := NewSource(Options{})
	got, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "95268"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	r, ok := got.(*Result)
	// Embedded data is the live national artifact (refreshed ~2×/year), so
	// assert shape, not an exact value (exact-value coverage is in the
	// injected-Index test). Garges (95268) is a populated 95 commune.
	if !ok || r.IsEmpty() || r.PriceMedianSmallEURM2 <= 0 || r.N <= 0 || r.Dept != "95" {
		t.Fatalf("bad query result: %#v (ok=%v)", got, ok)
	}
}

func TestSource_QueryWithInjectedIndex(t *testing.T) {
	idx := &Index{byINSEE: map[string]Result{
		"99999": {N: 7, PriceMedianSmallEURM2: 1234, Dept: "99"},
	}}
	s := NewSource(Options{Index: idx})
	got, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "99999"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	r, ok := got.(*Result)
	if !ok {
		t.Fatalf("want *Result, got %T", got)
	}
	if r.PriceMedianSmallEURM2 != 1234 {
		t.Fatalf("want PriceMedianSmallEURM2=1234, got %v", r.PriceMedianSmallEURM2)
	}
	if r.Evidence.INSEE != "99999" {
		t.Fatalf("want Evidence.INSEE=99999, got %q", r.Evidence.INSEE)
	}
}

// A missing INSEE must surface as ErrInsufficientInputs (uniform contract),
// not a silent empty Result.
func TestSource_MissingINSEE(t *testing.T) {
	s := NewSource(Options{Index: &Index{byINSEE: map[string]Result{}}})
	if _, err := s.Query(context.Background(), gazetteer.Listing{}); !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("want ErrInsufficientInputs for blank INSEE, got %v", err)
	}
}

// The atomic Query helper mirrors the other sources' contract.
func TestQuery_AtomicHelper(t *testing.T) {
	idx := &Index{byINSEE: map[string]Result{"95268": {N: 9, PriceMedianEURM2: 4200, Dept: "95"}}}
	r, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: "95268"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if r.IsEmpty() || r.PriceMedianEURM2 != 4200 || r.Evidence.INSEE != "95268" {
		t.Fatalf("unexpected Result: %+v (evidence %+v)", r, r.Evidence)
	}
}

// TestQuery_ArrondissementParentIsRefused pins the loud failure: geo-dvf ships
// no row for 75056 / 69123 / 13055, and answering "no qualifying sale" for
// Paris, Lyon or Marseille would be believed. communes.ResolveINSEE("Paris",
// "75000") hands a caller exactly one of these codes.
func TestQuery_ArrondissementParentIsRefused(t *testing.T) {
	t.Parallel()
	for _, insee := range []string{"75056", "69123", "13055"} {
		_, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: insee})
		if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
			t.Errorf("Query(%s) err = %v, want ErrInsufficientInputs", insee, err)
		}
	}
	// The arrondissement itself answers normally.
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "75101"})
	if err != nil {
		t.Fatalf("Query(75101): %v", err)
	}
	if res.IsEmpty() || res.PriceMedianEURM2 <= 0 {
		t.Errorf("Query(75101) = %+v, want a populated Paris 1er aggregate", res)
	}
}
