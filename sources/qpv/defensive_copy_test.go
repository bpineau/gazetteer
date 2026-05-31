package qpv

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestDefensiveCopy ensures a caller mutating Result.QPVs cannot corrupt
// the shared singleton index used by subsequent queries. Exercised on the
// commune-fallback path (no coordinates), which returns the commune's list
// straight from the index.
func TestDefensiveCopy(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "93066"} // no coords → commune fallback
	res1, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res1.QPVs) == 0 {
		t.Fatalf("no QPVs to test copy semantics")
	}
	res1.QPVs[0].Code = "MUTATED"
	res2, _ := Query(context.Background(), Options{}, l)
	if res2.QPVs[0].Code == "MUTATED" {
		t.Fatalf("mutation leaked into shared index")
	}
}
