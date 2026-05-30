package logiris

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
	if got := idx.Count(); got < 4000 {
		t.Errorf("Count = %d, want ≥ 4000 IDF IRIS", got)
	}
}

// TestQuery_HappyPath exercises Query against the embedded data for a known
// Montreuil IRIS (84 % renters, 59 % HLM, 6.4 % vacancy).
func TestQuery_HappyPath(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{IRIS: "930480604"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for a populated Montreuil IRIS")
	}
	if res.RenterSharePct < 50 || res.RenterSharePct > 100 {
		t.Errorf("RenterSharePct = %.1f, want a high renter share", res.RenterSharePct)
	}
	if res.VacancyRatePct <= 0 || res.VacancyRatePct > 30 {
		t.Errorf("VacancyRatePct = %.1f, want a plausible rate", res.VacancyRatePct)
	}
	if res.Confidence != ConfidenceHigh || res.Evidence.IRIS != "930480604" {
		t.Errorf("res = %+v, want high confidence + IRIS set", res)
	}
}

// TestQuery_MissingIRIS returns an empty (not error) result outside IDF.
func TestQuery_MissingIRIS(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{IRIS: "130010101"}) // Marseille (not IDF)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || !res.IsEmpty() {
		t.Errorf("res = %+v, want empty for a non-IDF IRIS", res)
	}
}

// TestQuery_NoIRIS requires Listing.IRIS.
func TestQuery_NoIRIS(t *testing.T) {
	t.Parallel()
	_, err := Query(context.Background(), Options{}, gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("err = %v, want ErrInsufficientInputs", err)
	}
}

// TestQuery_StubIndex drives Query with an injected index.
func TestQuery_StubIndex(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Meta: Meta{DataYear: 2021},
		IRIS: map[string]Entry{"930480604": {RenterSharePct: 83.6, SocialHousingSharePct: 59, VacancyRatePct: 6.4, TotalLogements: 1521}},
	}
	res, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{IRIS: "930480604"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RenterSharePct != 83.6 || res.SocialHousingSharePct != 59 || res.VacancyRatePct != 6.4 {
		t.Errorf("res = %+v, want the stub values", res)
	}
}
