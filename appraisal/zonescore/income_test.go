package zonescore

import (
	"testing"

	"github.com/bpineau/gazetteer/sources/filoiris"
	"github.com/bpineau/gazetteer/sources/filosofi"
)

// TestIncomeSubscore_PrefersIRIS pins that the solvabilité income component
// reads the IRIS-level Filosofi (filoiris) when present, falling back to the
// commune-level (filosofi) otherwise.
func TestIncomeSubscore_PrefersIRIS(t *testing.T) {
	t.Parallel()

	// Both present → IRIS wins (its median drives the score + it's the source).
	d := dossier(
		okResult(filosofi.Name, &filosofi.Result{MedianEUR: 28000, Flag: filosofi.RiskLow, Confidence: "high"}),
		okResult(filoiris.Name, &filoiris.Result{MedianEUR: 19270, PovertyRatePct: 25, Flag: filoiris.RiskHigh, Confidence: "high"}),
	)
	_, src, median, ok := incomeSubscore(d)
	if !ok || src != filoiris.Name || median != 19270 {
		t.Errorf("both present: src=%q median=%d ok=%v, want filoiris/19270", src, median, ok)
	}

	// IRIS empty → falls back to commune.
	d = dossier(
		okResult(filosofi.Name, &filosofi.Result{MedianEUR: 28000, Flag: filosofi.RiskLow, Confidence: "high"}),
		okResult(filoiris.Name, &filoiris.Result{Flag: filoiris.RiskUnknown}), // IsEmpty
	)
	_, src, median, ok = incomeSubscore(d)
	if !ok || src != filosofi.Name || median != 28000 {
		t.Errorf("IRIS empty: src=%q median=%d ok=%v, want filosofi/28000", src, median, ok)
	}

	// Neither present → not ok.
	if _, _, _, ok := incomeSubscore(dossier()); ok {
		t.Error("no income source: ok=true, want false")
	}
}
