package dvf

import (
	"testing"

	"github.com/bpineau/gazetteer/appraisal"
)

// Compile-time check: *Result satisfies appraisal.PriceEstimator.
var _ appraisal.PriceEstimator = (*Result)(nil)

func TestResult_PriceEstimateValueMapping(t *testing.T) {
	t.Parallel()

	v := int64(9_500_00)
	r := &Result{
		ValueEURPerM2Cents: &v,
		SampleSize:         42,
		Confidence:         ConfidenceHigh,
		Evidence: Evidence{
			LevelUsed:   "commune",
			WindowYears: 5,
		},
	}
	got := r.PriceEstimate()
	if got.EurPerM2Cents != 9_500_00 {
		t.Errorf("EurPerM2Cents = %d, want %d", got.EurPerM2Cents, 9_500_00)
	}
	if got.Confidence != appraisal.ConfidenceHigh {
		t.Errorf("Confidence = %v, want High", got.Confidence)
	}
	if got.SampleSize != 42 {
		t.Errorf("SampleSize = %d, want 42", got.SampleSize)
	}
	if got.Method != "dvf_commune_5y" {
		t.Errorf("Method = %q, want %q", got.Method, "dvf_commune_5y")
	}
}

func TestResult_PriceEstimateNilValue(t *testing.T) {
	t.Parallel()

	// ValueEURPerM2Cents nil → EurPerM2Cents 0 (caller should check IsEmpty).
	r := &Result{
		ValueEURPerM2Cents: nil,
		SampleSize:         0,
		Confidence:         ConfidenceLow,
		Evidence: Evidence{
			LevelUsed:   "department",
			WindowYears: 5,
		},
	}
	got := r.PriceEstimate()
	if got.EurPerM2Cents != 0 {
		t.Errorf("EurPerM2Cents = %d, want 0 (nil ValueEURPerM2Cents)", got.EurPerM2Cents)
	}
	if got.Confidence != appraisal.ConfidenceLow {
		t.Errorf("Confidence = %v, want Low", got.Confidence)
	}
	if got.Method != "dvf_department_5y" {
		t.Errorf("Method = %q, want %q", got.Method, "dvf_department_5y")
	}
}

func TestResult_PriceEstimateConfidenceMapping(t *testing.T) {
	t.Parallel()

	v := int64(1_000_00)
	cases := []struct {
		raw  string
		want appraisal.Confidence
	}{
		{ConfidenceHigh, appraisal.ConfidenceHigh},
		{ConfidenceMedium, appraisal.ConfidenceMedium},
		{ConfidenceLow, appraisal.ConfidenceLow},
		{"", appraisal.ConfidenceLow},      // unknown → low
		{"bogus", appraisal.ConfidenceLow}, // unknown → low
		{"HIGH", appraisal.ConfidenceLow},  // case-sensitive: unknown → low
	}
	for _, tc := range cases {
		r := &Result{
			ValueEURPerM2Cents: &v,
			Confidence:         tc.raw,
		}
		got := r.PriceEstimate()
		if got.Confidence != tc.want {
			t.Errorf("Confidence(%q) = %v, want %v", tc.raw, got.Confidence, tc.want)
		}
	}
}

func TestResult_PriceEstimateMethodFallback(t *testing.T) {
	t.Parallel()

	// Empty LevelUsed should fall back to "unknown" in the method label.
	v := int64(2_000_00)
	r := &Result{
		ValueEURPerM2Cents: &v,
		Evidence: Evidence{
			LevelUsed:   "",
			WindowYears: 0,
		},
	}
	got := r.PriceEstimate()
	if got.Method != "dvf_unknown_0y" {
		t.Errorf("Method = %q, want %q", got.Method, "dvf_unknown_0y")
	}
}
