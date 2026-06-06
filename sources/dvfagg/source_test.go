package dvfagg

import (
	"context"
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
	if !ok || r.IsEmpty() || r.PriceMedianSmallEURM2 != 2549 {
		t.Fatalf("bad query result: %#v (ok=%v)", got, ok)
	}
}
