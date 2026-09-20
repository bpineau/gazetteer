package zonescore

import (
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/carteloyers"
	"github.com/bpineau/gazetteer/sources/dvf"
	"github.com/bpineau/gazetteer/sources/encadrement"
)

// The reference figures these tests build on: carteloyers publishes charges
// comprises and converts at 0.90, so 40 CC enters the blend at 36 HC.
const (
	marketRentCC = 40.0
	marketRentHC = 36.0
	pricePerM2   = 10_000.0
)

// yieldDossier builds a Dossier with one price reading, one market-rent
// reading and, when majore > 0, an encadrement cell.
//
// reference and majore are separate arguments because encadrement feeds BOTH
// sides of the yield: its loyer de référence joins the market blend, its
// majoré sets the ceiling. Pinning the reference at the market reading is what
// lets a test vary the ceiling alone.
func yieldDossier(price, rentCC, reference, majore float64) gazetteer.Dossier {
	cents := int64(price * 100)
	res := map[string]gazetteer.Result{
		dvf.Name: {Status: gazetteer.StatusOK, Data: &dvf.Result{
			ValueEURPerM2Cents: &cents, SampleSize: 50, Confidence: dvf.ConfidenceHigh,
		}},
		carteloyers.Name: {Status: gazetteer.StatusOK, Data: &carteloyers.Result{
			LoyerMedEURPerM2CC: rentCC,
		}},
	}
	if majore > 0 {
		res[encadrement.Name] = gazetteer.Result{Status: gazetteer.StatusOK, Data: &encadrement.Result{
			LoyerRefMajEURPerM2HC: majore,
			LoyerRefEURPerM2HC:    reference,
			Zone:                  "Paris 11e",
			ZoneSource:            encadrement.ZoneSourceParis,
			Confidence:            encadrement.ConfidenceMedium,
		}}
	}
	return gazetteer.Dossier{Results: res}
}

func rendementAxis(t *testing.T, d gazetteer.Dossier) Axis {
	t.Helper()
	for _, a := range Compute(d).Axes {
		if a.Name == AxisRendement {
			return a
		}
	}
	t.Fatalf("no %s axis in the score", AxisRendement)
	return Axis{}
}

// TestScoreRendement_CappedByEncadrement pins that the heaviest axis scores
// the rent that may LEGALLY be charged, not the market blend. Scoring the raw
// blend rewarded a yield the landlord cannot collect, in exactly the zones
// where the gap is widest — and overbidding on such a yield is what the score
// exists to prevent.
func TestScoreRendement_CappedByEncadrement(t *testing.T) {
	t.Parallel()
	// Blend 36 €/m²/month HC on a 10 000 €/m² address: 4.32 % market. A
	// 30 €/m²/month ceiling makes it 3.60 % legal. The reference is held at
	// the market reading so the encadrement cell moves the ceiling only.
	capped := rendementAxis(t, yieldDossier(pricePerM2, marketRentCC, marketRentHC, 30))
	uncapped := rendementAxis(t, yieldDossier(pricePerM2, marketRentCC, marketRentHC, 100))

	if !capped.Present || !uncapped.Present {
		t.Fatalf("axis absent: capped=%v uncapped=%v", capped.Present, uncapped.Present)
	}
	if capped.Value >= uncapped.Value {
		t.Errorf("capped score %.1f, want strictly below the uncapped %.1f", capped.Value, uncapped.Value)
	}
	if !strings.Contains(capped.Reason, "3.6%") || !strings.Contains(capped.Reason, "30€/m²/mois") {
		t.Errorf("reason %q should quote the legal 3.6 %% on 30€/m²/mois", capped.Reason)
	}
	if !strings.Contains(capped.Reason, "plafonné") {
		t.Errorf("reason %q should say the rent was capped", capped.Reason)
	}

	// A ceiling above the blend must not move the score, nor claim to bind.
	slack := rendementAxis(t, yieldDossier(pricePerM2, marketRentCC, marketRentHC, 37))
	if slack.Value != uncapped.Value {
		t.Errorf("non-binding cap moved the score: %.1f vs %.1f", slack.Value, uncapped.Value)
	}
	if strings.Contains(slack.Reason, "plafonné") {
		t.Errorf("non-binding cap should not claim to bind: %q", slack.Reason)
	}

	// And the score is monotone in the ceiling itself.
	var prev float64
	for i, majore := range []float64{12, 20, 30, 37} {
		got := rendementAxis(t, yieldDossier(pricePerM2, marketRentCC, marketRentHC, majore)).Value
		if i > 0 && got < prev {
			t.Errorf("ceiling %.0f scored %.1f, below the tighter ceiling's %.1f", majore, got, prev)
		}
		prev = got
	}
}

// TestScoreRendement_MonotoneInEachIngredient pins the documented direction of
// the axis: more rent scores higher, a dearer square metre scores lower.
func TestScoreRendement_MonotoneInEachIngredient(t *testing.T) {
	t.Parallel()
	var prev float64
	for i, rent := range []float64{20, 30, 40, 50} {
		got := rendementAxis(t, yieldDossier(5_000, rent, 0, 0)).Value
		if i > 0 && got < prev {
			t.Errorf("rent %.0f scored %.1f, below the cheaper rent's %.1f", rent, got, prev)
		}
		prev = got
	}
	prev = 101
	for _, price := range []float64{2_000, 4_000, 8_000, 16_000} {
		got := rendementAxis(t, yieldDossier(price, 30, 0, 0)).Value
		if got > prev {
			t.Errorf("price %.0f scored %.1f, above the cheaper price's %.1f", price, got, prev)
		}
		prev = got
	}
}
