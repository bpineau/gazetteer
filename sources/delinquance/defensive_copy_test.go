package delinquance

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestQuery_ResultMapMutationDoesNotCorruptIndex verifies that mutating
// the returned Result.Rates map does not leak back into the singleton
// index — i.e. that the Source ships a defensive copy on the happy
// path. Without the copy, a downstream consumer corrupting Rates
// would silently break every subsequent Query against the same
// commune.
func TestQuery_ResultMapMutationDoesNotCorruptIndex(t *testing.T) {
	t.Parallel()
	src := NewSource(Options{})

	first, err := src.Query(context.Background(), gazetteer.Listing{INSEE: "75119"})
	if err != nil {
		t.Fatalf("Query 1: %v", err)
	}
	r1 := first.(*Result)
	if len(r1.Rates) == 0 {
		t.Skip("Paris 19e (75119) carries no SSMSI entry in the embedded index; cannot exercise the defensive copy")
	}
	pickedKey := ""
	for k := range r1.Rates {
		pickedKey = k
		break
	}
	beforeMutation := r1.Rates[pickedKey]
	r1.Rates[pickedKey] = beforeMutation + 999_999.0

	second, err := src.Query(context.Background(), gazetteer.Listing{INSEE: "75119"})
	if err != nil {
		t.Fatalf("Query 2: %v", err)
	}
	r2 := second.(*Result)
	if r2.Rates[pickedKey] != beforeMutation {
		t.Errorf("singleton index corrupted: Rates[%q] expected %g after first-call mutation, got %g",
			pickedKey, beforeMutation, r2.Rates[pickedKey])
	}
}
