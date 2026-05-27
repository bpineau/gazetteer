package qpv

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestQuery_ResultSliceMutationDoesNotCorruptIndex verifies that
// mutating the returned Result.QPVs slice does not leak back into the
// singleton index — i.e. that the Source ships a defensive copy on the
// happy path. Without the copy, a downstream consumer rewriting a
// QPV entry would silently corrupt every subsequent Query.
func TestQuery_ResultSliceMutationDoesNotCorruptIndex(t *testing.T) {
	t.Parallel()
	src := NewSource(Options{})

	first, err := src.Query(context.Background(), gazetteer.Listing{INSEE: "93066"})
	if err != nil {
		t.Fatalf("Query 1: %v", err)
	}
	r1 := first.(*Result)
	if len(r1.QPVs) == 0 {
		t.Skip("Saint-Denis (93066) carries no QPV entry in the embedded index; cannot exercise the defensive copy")
	}
	beforeCode := r1.QPVs[0].Code
	r1.QPVs[0].Code = "MUTATED"

	second, err := src.Query(context.Background(), gazetteer.Listing{INSEE: "93066"})
	if err != nil {
		t.Fatalf("Query 2: %v", err)
	}
	r2 := second.(*Result)
	if r2.QPVs[0].Code != beforeCode {
		t.Errorf("singleton index corrupted: QPVs[0].Code expected %q after first-call mutation, got %q",
			beforeCode, r2.QPVs[0].Code)
	}
}
