package filoiris

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
	if got := idx.Count(); got < 12000 {
		t.Errorf("Count = %d, want ≥ 12 000 IRIS", got)
	}
	if idx.Meta.NationalMedianEUR < 18000 || idx.Meta.NationalMedianEUR > 30000 {
		t.Errorf("NationalMedianEUR = %d, want in [18 000, 30 000]", idx.Meta.NationalMedianEUR)
	}
}

// TestQuery_HappyPath exercises the full Source.Query path against the
// embedded data for a known Paris IRIS.
func TestQuery_HappyPath(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{IRIS: "751010201"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for a populated Paris IRIS")
	}
	if res.MedianEUR < 15000 || res.MedianEUR > 60000 {
		t.Errorf("MedianEUR = %d, want in [15 000, 60 000]", res.MedianEUR)
	}
	if res.Flag == RiskUnknown {
		t.Errorf("Flag = %q, want a classified bucket", res.Flag)
	}
	if res.Evidence.IRIS != "751010201" || res.Evidence.DataYear < 2018 {
		t.Errorf("Evidence = %+v, want IRIS set + DataYear ≥ 2018", res.Evidence)
	}
}

// TestQuery_MissingIRIS returns an empty (not error) result for an IRIS
// absent from the dataset.
func TestQuery_MissingIRIS(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{IRIS: "999999999"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || !res.IsEmpty() || res.Flag != RiskUnknown {
		t.Errorf("res = %+v, want empty + RiskUnknown", res)
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

// TestClassifyRisk pins the income-risk thresholds.
func TestClassifyRisk(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		e    Entry
		want RiskFlag
	}{
		{"wealthy low-poverty", Entry{MedianEUR: 30000, PovertyRatePct: 6}, RiskLow},
		{"poor median", Entry{MedianEUR: 17000, PovertyRatePct: 8}, RiskHigh},
		{"high poverty despite ok median", Entry{MedianEUR: 21000, PovertyRatePct: 25}, RiskHigh},
		{"middle", Entry{MedianEUR: 22000, PovertyRatePct: 14}, RiskMedium},
		{"high median but poverty unknown still low", Entry{MedianEUR: 27000}, RiskLow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyRisk(c.e); got != c.want {
				t.Errorf("classifyRisk(%+v) = %q, want %q", c.e, got, c.want)
			}
		})
	}
}

// TestQuery_StubIndex drives Query with an injected index (no embed).
func TestQuery_StubIndex(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Meta: Meta{DataYear: 2021, NationalMedianEUR: 22380},
		IRIS: map[string]Entry{"930480604": {MedianEUR: 19270, PovertyRatePct: 25, Gini: 0.293}},
	}
	res, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{IRIS: "930480604"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.MedianEUR != 19270 || res.PovertyRatePct != 25 || res.Flag != RiskHigh {
		t.Errorf("res = %+v, want median 19270 / poverty 25 / high", res)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want high (median + poverty present)", res.Confidence)
	}
}
