package vacance_logements

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestQuery_ResultMutationDoesNotCorruptIndex verifies that mutating a
// returned Result does not leak back into the singleton index. Result
// is a scalar payload (no shared slice / map), so this is a smoke check.
func TestQuery_ResultMutationDoesNotCorruptIndex(t *testing.T) {
	t.Parallel()
	src := NewSource(Options{})

	first, err := src.Query(context.Background(), gazetteer.Listing{INSEE: "75101"})
	if err != nil {
		t.Fatalf("Query 1: %v", err)
	}
	r1 := first.(*Result)
	if r1.IsEmpty() {
		t.Skip("75101 missing from the embedded census; cannot exercise the defensive copy")
	}
	beforeRate := r1.VacancyRate
	r1.VacancyRate = -999

	second, err := src.Query(context.Background(), gazetteer.Listing{INSEE: "75101"})
	if err != nil {
		t.Fatalf("Query 2: %v", err)
	}
	r2 := second.(*Result)
	if r2.VacancyRate != beforeRate {
		t.Errorf("singleton index corrupted: VacancyRate expected %v after first-call mutation, got %v",
			beforeRate, r2.VacancyRate)
	}
}
