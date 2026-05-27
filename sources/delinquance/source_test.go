package delinquance

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded dataset.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx == nil {
		t.Fatalf("nil index")
	}
	if got := idx.Count(); got < 20_000 {
		t.Errorf("Count = %d, want ≥ 20 000", got)
	}
	if idx.Meta.DataYear < 2020 {
		t.Errorf("DataYear = %d, want ≥ 2020", idx.Meta.DataYear)
	}
	if idx.Meta.Unit == "" {
		t.Errorf("Meta.Unit empty, want populated")
	}
}

// TestQuery_HappyPath_Paris pins a known commune and verifies at
// least one indicator is populated.
func TestQuery_HappyPath_Paris(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "75056"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Paris (75056)")
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if len(res.Rates) == 0 {
		t.Errorf("Rates empty, want at least one indicator")
	}
	if res.Population <= 0 {
		t.Errorf("Population = %d, want > 0", res.Population)
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
		t.Errorf("IsEmpty = false, want true")
	}
	if res.Flag != RiskUnknown {
		t.Errorf("Flag = %q, want %q", res.Flag, RiskUnknown)
	}
}

// TestQuery_InsufficientInputs rejects empty INSEE.
func TestQuery_InsufficientInputs(t *testing.T) {
	t.Parallel()
	_, err := Query(context.Background(), Options{}, gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("err = %v, want ErrInsufficientInputs", err)
	}
}

// TestClassifyRisk pins the three-bucket logic.
func TestClassifyRisk(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		rates map[string]float64
		want  RiskFlag
	}{
		{"empty", map[string]float64{}, RiskUnknown},
		{"all-low", map[string]float64{"burglary": 1.0, "theft_no_violence": 4.0, "vandalism": 2.0}, RiskLow},
		{"medium", map[string]float64{"burglary": 4.0, "theft_no_violence": 10.0, "vandalism": 7.0}, RiskMedium},
		{"high-burglary", map[string]float64{"burglary": 7.0, "theft_no_violence": 3.0, "vandalism": 2.0}, RiskHigh},
		{"high-vandalism", map[string]float64{"burglary": 2.0, "theft_no_violence": 5.0, "vandalism": 15.0}, RiskHigh},
	}
	for _, c := range cases {
		if got := classifyRisk(c.rates); got != c.want {
			t.Errorf("%s: classifyRisk = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestSourceRegistered ensures the init() side-effect wired the
// gazetteer registry.
func TestSourceRegistered(t *testing.T) {
	t.Parallel()
	if got := gazetteer.Lookup(Name); got == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil, want factory", Name)
	}
}
