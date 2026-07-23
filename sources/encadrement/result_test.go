package encadrement

import (
	"testing"

	"github.com/bpineau/gazetteer/appraisal"
)

// Compile-time check: *Result satisfies appraisal.RentEstimator and
// appraisal.RentCapper.
var (
	_ appraisal.RentEstimator = (*Result)(nil)
	_ appraisal.RentCapper    = (*Result)(nil)
)

func TestResult_RentCap(t *testing.T) {
	t.Parallel()
	// 32.16 €/m²/month HC majoré → 3216 cents.
	r := &Result{LoyerRefEURPerM2HC: 26.80, LoyerRefMajEURPerM2HC: 32.16}
	cents, ok := r.RentCap()
	if !ok || cents != 3216 {
		t.Errorf("RentCap() = %d, %v, want 3216, true", cents, ok)
	}
	// No majoré → not a cap.
	if cents, ok := (&Result{}).RentCap(); ok || cents != 0 {
		t.Errorf("empty RentCap() = %d, %v, want 0, false", cents, ok)
	}
	if _, ok := (*Result)(nil).RentCap(); ok {
		t.Error("nil RentCap() ok = true, want false")
	}
}

func TestResult_RentEstimateValueMapping(t *testing.T) {
	t.Parallel()

	// 26.80 €/m²/month HC reference → 2680 cents. Bracket carries the
	// (zone_source, zone) tag; Method records the rooms bucket.
	r := &Result{
		LoyerRefEURPerM2HC:    26.80,
		LoyerRefMajEURPerM2HC: 32.16,
		Zone:                  "Paris 11e",
		ZoneSource:            ZoneSourceParis,
		Confidence:            ConfidenceMedium,
		Evidence:              Evidence{Piece: 2},
	}
	got := r.RentEstimate()
	if got.EurPerM2Cents != 2680 {
		t.Errorf("EurPerM2Cents = %d, want 2680 (26.80 €/m²/month HC ref × 100)", got.EurPerM2Cents)
	}
	if got.Confidence != appraisal.ConfidenceMedium {
		t.Errorf("Confidence = %v, want Medium", got.Confidence)
	}
	if got.Bracket != "encadrement_paris_Paris 11e" {
		t.Errorf("Bracket = %q, want %q", got.Bracket, "encadrement_paris_Paris 11e")
	}
	if got.Method != "encadrement_paris_p2" {
		t.Errorf("Method = %q, want %q", got.Method, "encadrement_paris_p2")
	}
}

func TestResult_RentEstimateEmptyResultZero(t *testing.T) {
	t.Parallel()

	// Empty Result (zero loyer + no zone) → zero estimate + empty
	// Bracket. Caller filters via IsEmpty.
	r := &Result{}
	got := r.RentEstimate()
	if got.EurPerM2Cents != 0 {
		t.Errorf("EurPerM2Cents = %d, want 0 (empty Result)", got.EurPerM2Cents)
	}
	if got.Bracket != "" {
		t.Errorf("Bracket = %q, want empty", got.Bracket)
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
		{ConfidenceMedium, appraisal.ConfidenceMedium},
		{ConfidenceNone, appraisal.ConfidenceLow},
		{"", appraisal.ConfidenceLow},
		{"bogus", appraisal.ConfidenceLow},
		{"high", appraisal.ConfidenceLow}, // encadrement doesn't emit "high"
	}
	for _, tc := range cases {
		r := &Result{
			LoyerRefEURPerM2HC: 20.0,
			Confidence:         tc.raw,
		}
		got := r.RentEstimate()
		if got.Confidence != tc.want {
			t.Errorf("Confidence(%q) = %v, want %v", tc.raw, got.Confidence, tc.want)
		}
	}
}

func TestResult_RentEstimateBracketShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		zoneSource  string
		zone        string
		wantBracket string
	}{
		{"both populated", ZoneSourceLyonVilleurbanne, "Lyon 3e", "encadrement_lyon_villeurbanne_Lyon 3e"},
		{"zone only", "", "Lyon 3e", "encadrement_Lyon 3e"},
		{"zone_source only", ZoneSourcePlaineCommune, "", "encadrement_plaine_commune"},
		{"both empty", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &Result{
				LoyerRefEURPerM2HC: 15.0,
				ZoneSource:         tc.zoneSource,
				Zone:               tc.zone,
			}
			got := r.RentEstimate()
			if got.Bracket != tc.wantBracket {
				t.Errorf("Bracket = %q, want %q", got.Bracket, tc.wantBracket)
			}
		})
	}
}

func TestResult_RentEstimateMethodFallback(t *testing.T) {
	t.Parallel()

	// Empty ZoneSource should fall back to "unknown" in the method label.
	r := &Result{
		LoyerRefEURPerM2HC: 15.0,
		ZoneSource:         "",
		Evidence:           Evidence{Piece: 0},
	}
	got := r.RentEstimate()
	if got.Method != "encadrement_unknown_p0" {
		t.Errorf("Method = %q, want %q", got.Method, "encadrement_unknown_p0")
	}
}
