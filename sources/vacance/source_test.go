package vacance

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
	if got := idx.Count(); got < 10000 {
		t.Errorf("Count = %d, want ≥ 10 000", got)
	}
}

// TestQuery_HappyPath exercises the full Source.Query path for a
// well-known commune.
func TestQuery_HappyPath(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "75111"} // Paris 11e.
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result")
	}
	if res.IsEmpty() {
		t.Fatalf("empty result for Paris 11e — LOVAC should cover it")
	}
	if res.VacancePct < 0 || res.VacancePct > 50 {
		t.Errorf("VacancePct = %.2f, want in [0, 50]", res.VacancePct)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
}

// TestQuery_UnknownCommune returns IsEmpty.
func TestQuery_UnknownCommune(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "99999"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result, want non-nil empty")
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty = false, want true for synthetic INSEE")
	}
	if res.Confidence != ConfidenceNone {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceNone)
	}
}

// TestQuery_InsufficientInputs rejects empty INSEE.
func TestQuery_InsufficientInputs(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{}
	_, err := Query(context.Background(), Options{}, l)
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("err = %v, want ErrInsufficientInputs", err)
	}
}
