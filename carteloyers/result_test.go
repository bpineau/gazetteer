package carteloyers

import (
	"testing"

	"github.com/bpineau/gazetteer/appraisal"
)

// Compile-time check: *Result satisfies appraisal.RentEstimator.
var _ appraisal.RentEstimator = (*Result)(nil)

func TestResult_RentEstimateValueMapping(t *testing.T) {
	t.Parallel()

	// 18.45 €/m²/month CC → 1845 cents. Typology + confidence carry
	// through to the appraisal envelope.
	r := &Result{
		LoyerMedEURPerM2CC: 18.45,
		Typology:           TypologyApt12,
		Confidence:         ConfidenceHigh,
	}
	got := r.RentEstimate()
	if got.EurPerM2Cents != 1845 {
		t.Errorf("EurPerM2Cents = %d, want 1845 (18.45 €/m²/month CC × 100)", got.EurPerM2Cents)
	}
	if got.Confidence != appraisal.ConfidenceHigh {
		t.Errorf("Confidence = %v, want High", got.Confidence)
	}
	if got.Method != "carteloyers_apt_1_2" {
		t.Errorf("Method = %q, want %q", got.Method, "carteloyers_apt_1_2")
	}
	// Bracket is not populated by carteloyers (no regulated cap).
	if got.Bracket != "" {
		t.Errorf("Bracket = %q, want empty (carteloyers is reference, not cap)", got.Bracket)
	}
}

func TestResult_RentEstimateEmptyResultZero(t *testing.T) {
	t.Parallel()

	// Empty Result (zero loyer) → zero estimate. Caller filters via IsEmpty.
	r := &Result{LoyerMedEURPerM2CC: 0}
	got := r.RentEstimate()
	if got.EurPerM2Cents != 0 {
		t.Errorf("EurPerM2Cents = %d, want 0 (empty Result)", got.EurPerM2Cents)
	}
}

func TestResult_RentEstimateNilReceiver(t *testing.T) {
	t.Parallel()

	var r *Result
	got := r.RentEstimate()
	if got.EurPerM2Cents != 0 || got.Confidence != appraisal.ConfidenceLow {
		t.Errorf("nil receiver = %+v, want zero estimate", got)
	}
}

func TestResult_RentEstimateConfidenceMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want appraisal.Confidence
	}{
		{ConfidenceHigh, appraisal.ConfidenceHigh},
		{ConfidenceMedium, appraisal.ConfidenceMedium},
		{ConfidenceLow, appraisal.ConfidenceLow},
		{ConfidenceNone, appraisal.ConfidenceLow},
		{"bogus", appraisal.ConfidenceLow},
		{"HIGH", appraisal.ConfidenceLow}, // case-sensitive
	}
	for _, tc := range cases {
		r := &Result{
			LoyerMedEURPerM2CC: 15.0,
			Confidence:         tc.raw,
		}
		got := r.RentEstimate()
		if got.Confidence != tc.want {
			t.Errorf("Confidence(%q) = %v, want %v", tc.raw, got.Confidence, tc.want)
		}
	}
}

func TestResult_RentEstimateMethodFallback(t *testing.T) {
	t.Parallel()

	// Empty Typology should fall back to "unknown" in the method label.
	r := &Result{
		LoyerMedEURPerM2CC: 12.0,
		Typology:           "",
	}
	got := r.RentEstimate()
	if got.Method != "carteloyers_unknown" {
		t.Errorf("Method = %q, want %q", got.Method, "carteloyers_unknown")
	}
}

func TestResult_RentEstimateAllTypologies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typ        Typology
		wantMethod string
	}{
		{TypologyApartment, "carteloyers_apt"},
		{TypologyHouse, "carteloyers_house"},
		{TypologyApt12, "carteloyers_apt_1_2"},
		{TypologyApt3, "carteloyers_apt_3_plus"},
	}
	for _, tc := range cases {
		r := &Result{LoyerMedEURPerM2CC: 10.0, Typology: tc.typ}
		got := r.RentEstimate()
		if got.Method != tc.wantMethod {
			t.Errorf("Method(%v) = %q, want %q", tc.typ, got.Method, tc.wantMethod)
		}
	}
}
