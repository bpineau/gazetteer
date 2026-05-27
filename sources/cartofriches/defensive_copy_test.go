package cartofriches

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestQuery_ResultMapMutationDoesNotCorruptIndex verifies that mutating
// the returned Result's maps does not leak back into the singleton
// index — i.e. that the Source ships a defensive copy on the happy
// path. Without the copy, a downstream consumer corrupting ByType /
// ByStatus would silently break every subsequent Query.
func TestQuery_ResultMapMutationDoesNotCorruptIndex(t *testing.T) {
	t.Parallel()
	src := NewSource(Options{})

	first, err := src.Query(context.Background(), gazetteer.Listing{INSEE: "59350"})
	if err != nil {
		t.Fatalf("Query 1: %v", err)
	}
	r1 := first.(*Result)
	if len(r1.ByType) == 0 {
		t.Skip("Lille (59350) carries no Cartofriches entry in the embedded index; cannot exercise the defensive copy")
	}
	pickedKey := ""
	for k := range r1.ByType {
		pickedKey = k
		break
	}
	beforeMutation := r1.ByType[pickedKey]
	r1.ByType[pickedKey] = beforeMutation + 999_999

	second, err := src.Query(context.Background(), gazetteer.Listing{INSEE: "59350"})
	if err != nil {
		t.Fatalf("Query 2: %v", err)
	}
	r2 := second.(*Result)
	if r2.ByType[pickedKey] != beforeMutation {
		t.Errorf("singleton index corrupted: ByType[%q] expected %d after first-call mutation, got %d",
			pickedKey, beforeMutation, r2.ByType[pickedKey])
	}
}
