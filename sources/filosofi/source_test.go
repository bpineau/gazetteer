package filosofi

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
	if got := idx.Count(); got < 25000 {
		t.Errorf("Count = %d, want ≥ 25 000", got)
	}
	if idx.Meta.NationalMedianEUR < 18000 || idx.Meta.NationalMedianEUR > 30000 {
		t.Errorf("NationalMedianEUR = %d, want in [18 000, 30 000]", idx.Meta.NationalMedianEUR)
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
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Paris 11e")
	}
	if res.MedianEUR < 15000 || res.MedianEUR > 50000 {
		t.Errorf("MedianEUR = %d, want in [15 000, 50 000]", res.MedianEUR)
	}
	if res.Flag == RiskUnknown {
		t.Errorf("Flag = %q, want non-unknown for Paris 11e", res.Flag)
	}
	if res.Evidence.DataYear < 2018 {
		t.Errorf("DataYear = %d, want ≥ 2018", res.Evidence.DataYear)
	}
}

// TestQuery_GoldenCases pins risk-flag classification for a handful of
// reference communes.
func TestQuery_GoldenCases(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := []struct {
		insee   string
		wantOK  bool
		minFlag RiskFlag // sanity floor — different communes have different flag profiles.
	}{
		{"75116", true, RiskLow},      // Paris 16e — high income.
		{"93066", true, RiskHigh},     // Saint-Denis (93) — high minima.
		{"99999", false, RiskUnknown}, // Synthetic.
	}
	for _, c := range cases {
		l := gazetteer.Listing{INSEE: c.insee}
		res, err := Query(context.Background(), Options{Index: idx}, l)
		if err != nil {
			t.Errorf("insee %s: Query: %v", c.insee, err)
			continue
		}
		if res == nil {
			t.Errorf("insee %s: nil result", c.insee)
			continue
		}
		if c.wantOK && res.IsEmpty() {
			t.Errorf("insee %s: IsEmpty, want non-empty", c.insee)
		}
		if !c.wantOK && !res.IsEmpty() {
			t.Errorf("insee %s: not empty, want empty", c.insee)
		}
		if c.wantOK && res.Flag != c.minFlag {
			t.Errorf("insee %s: Flag = %q, want %q", c.insee, res.Flag, c.minFlag)
		}
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

// TestClassifyRisk pins the four buckets.
func TestClassifyRisk(t *testing.T) {
	t.Parallel()
	cases := []struct {
		entry Entry
		want  RiskFlag
	}{
		{Entry{MedianEUR: 30000, MinimaPct: 1.0}, RiskLow},
		{Entry{MedianEUR: 25000, MinimaPct: 2.5}, RiskLow},
		{Entry{MedianEUR: 30000, MinimaPct: 0}, RiskLow}, // missing minima → still low if median high enough.
		{Entry{MedianEUR: 17000, MinimaPct: 0}, RiskHigh},
		{Entry{MedianEUR: 22000, MinimaPct: 5.0}, RiskHigh},
		{Entry{MedianEUR: 22000, MinimaPct: 3.0}, RiskMedium},
	}
	for _, c := range cases {
		if got := classifyRisk(c.entry); got != c.want {
			t.Errorf("classifyRisk(%+v) = %q, want %q", c.entry, got, c.want)
		}
	}
}
