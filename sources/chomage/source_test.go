package chomage

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
	if got := idx.ZoneCount(); got < 250 {
		t.Errorf("ZoneCount = %d, want ≥ 250 (302 ZE2020 zones expected)", got)
	}
	if got := idx.CommuneCount(); got < 30000 {
		t.Errorf("CommuneCount = %d, want ≥ 30000", got)
	}
	if idx.Meta.NationalRatePct <= 0 {
		t.Errorf("Meta.NationalRatePct = %v, want > 0", idx.Meta.NationalRatePct)
	}
	if len(idx.Quarters) < 10 {
		t.Errorf("len(Quarters) = %d, want ≥ 10", len(idx.Quarters))
	}
	if len(idx.NationalRatePctSeries) != len(idx.Quarters) {
		t.Errorf("national series length %d != quarter count %d",
			len(idx.NationalRatePctSeries), len(idx.Quarters))
	}
}

// TestQuery_HappyPath pins Paris (75056) — must resolve to a Paris-area
// ZE with a populated rate.
func TestQuery_HappyPath(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "75056"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for 75056 Paris")
	}
	if res.ZECode == "" {
		t.Errorf("ZECode empty")
	}
	if res.ZELabel == "" {
		t.Errorf("ZELabel empty")
	}
	if res.RatePct <= 0 || res.RatePct > 25 {
		t.Errorf("RatePct = %v, want in (0, 25]", res.RatePct)
	}
	if res.NationalRatePct <= 0 || res.NationalRatePct > 25 {
		t.Errorf("NationalRatePct = %v, want in (0, 25]", res.NationalRatePct)
	}
	if res.QuarterLabel == "" {
		t.Errorf("QuarterLabel empty")
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	switch res.Tension {
	case TensionTight, TensionBalanced, TensionLoose:
		// OK
	default:
		t.Errorf("Tension = %q, want a populated flag", res.Tension)
	}
	if len(res.RecentTrendSeries) != len(res.Evidence.QuarterLabels) {
		t.Errorf("RecentTrendSeries len %d != QuarterLabels len %d",
			len(res.RecentTrendSeries), len(res.Evidence.QuarterLabels))
	}
}

// TestQuery_RuralCommune ensures the source returns a sensible answer
// for a rural commune (Languidic / 56101). Same ZE coverage rules apply.
func TestQuery_RuralCommune(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "56101"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for 56101 Languidic")
	}
	if res.RatePct <= 0 {
		t.Errorf("RatePct = %v, want > 0", res.RatePct)
	}
}

// TestQuery_UnknownCommune returns IsEmpty for a fake INSEE not in the
// crosswalk.
func TestQuery_UnknownCommune(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "99999"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result, want non-nil empty")
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty = false, want true (99999 not in crosswalk)")
	}
	if res.Tension != TensionUnknown {
		t.Errorf("Tension = %q, want %q", res.Tension, TensionUnknown)
	}
	if res.Confidence != ConfidenceNone {
		t.Errorf("Confidence = %q, want empty", res.Confidence)
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

// TestQuery_InjectedIndex pins the classifier on a controlled fixture
// so the test does not drift with future data refreshes.
func TestQuery_InjectedIndex(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Meta: Meta{
			Source:          "test",
			SeriesStart:     "2024-T1",
			SeriesEnd:       "2024-T4",
			NationalRatePct: 7.5,
		},
		Quarters:              []string{"2024-T1", "2024-T2", "2024-T3", "2024-T4"},
		NationalRatePctSeries: []float64{7.5, 7.5, 7.5, 7.5},
		Zones: map[string]ZoneEntry{
			"Z001": {Label: "Loose Town", RatePct: []float64{9.0, 9.2, 9.4, 9.5}},    // +2 pp → loose
			"Z002": {Label: "Tight Town", RatePct: []float64{5.0, 5.1, 5.2, 5.5}},    // -2 pp → tight
			"Z003": {Label: "Balanced Town", RatePct: []float64{7.2, 7.3, 7.4, 7.5}}, // 0 → balanced
		},
		Communes: map[string]string{
			"10001": "Z001",
			"10002": "Z002",
			"10003": "Z003",
		},
	}
	cases := []struct {
		insee   string
		wantPct float64
		wantT   TensionFlag
	}{
		{"10001", 9.5, TensionLoose},
		{"10002", 5.5, TensionTight},
		{"10003", 7.5, TensionBalanced},
	}
	for _, c := range cases {
		res, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: c.insee})
		if err != nil {
			t.Fatalf("Query(%s): %v", c.insee, err)
		}
		if res.RatePct != c.wantPct {
			t.Errorf("INSEE %s RatePct = %v, want %v", c.insee, res.RatePct, c.wantPct)
		}
		if res.Tension != c.wantT {
			t.Errorf("INSEE %s Tension = %q, want %q", c.insee, res.Tension, c.wantT)
		}
		if res.QuarterLabel != "2024-T4" {
			t.Errorf("INSEE %s QuarterLabel = %q, want 2024-T4", c.insee, res.QuarterLabel)
		}
	}
}

// TestLatestHelper pins the latest() helper on edge cases.
func TestLatestHelper(t *testing.T) {
	t.Parallel()
	quarters := []string{"q1", "q2", "q3"}
	v, q, i := latest([]float64{7, 8, 9}, quarters)
	if v != 9 || q != "q3" || i != 2 {
		t.Errorf("latest non-zero series: v=%v q=%q i=%d", v, q, i)
	}
	// Missing trailing values: pick the last populated reading.
	v, q, i = latest([]float64{7, 8, 0}, quarters)
	if v != 8 || q != "q2" || i != 1 {
		t.Errorf("latest with trailing zero: v=%v q=%q i=%d", v, q, i)
	}
	// All zeroes -> nothing.
	v, q, i = latest([]float64{0, 0, 0}, quarters)
	if v != 0 || q != "" || i != -1 {
		t.Errorf("latest all zero: v=%v q=%q i=%d", v, q, i)
	}
}

// TestClassify pins the tension thresholds.
func TestClassify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		delta float64
		want  TensionFlag
	}{
		{-3.0, TensionTight},
		{-1.0, TensionTight},
		{-0.9, TensionBalanced},
		{0.0, TensionBalanced},
		{0.9, TensionBalanced},
		{1.0, TensionLoose},
		{3.0, TensionLoose},
	}
	for _, c := range cases {
		if got := classify(c.delta); got != c.want {
			t.Errorf("classify(%v) = %q, want %q", c.delta, got, c.want)
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

// TestSource_NameVersion smokes the Source interface adapter.
func TestSource_NameVersion(t *testing.T) {
	t.Parallel()
	s := NewSource(Options{})
	if s.Name() != Name {
		t.Errorf("Name() = %q, want %q", s.Name(), Name)
	}
	if s.Version() != sourceVersion {
		t.Errorf("Version() = %d, want %d", s.Version(), sourceVersion)
	}
}
