package zonescore

import (
	"testing"

	"github.com/bpineau/gazetteer/sources/logiris"
	"github.com/bpineau/gazetteer/sources/vacance"
)

// TestScoreTension_PrefersLogiris pins that the tension axis reads the
// IRIS-level housing (logiris) when present and falls back to the
// commune-level vacancy (vacance) otherwise.
func TestScoreTension_PrefersLogiris(t *testing.T) {
	t.Parallel()

	// Both present → logiris is the source (vacance is not consulted).
	d := dossier(
		okResult(vacance.Name, &vacance.Result{VacancyRate: 9, Confidence: "high"}),
		okResult(logiris.Name, &logiris.Result{RenterSharePct: 84, VacancyRatePct: 6.4, TotalLogements: 1521, Confidence: "high"}),
	)
	ar := scoreTension(d)
	if !ar.present || !hasSource(ar.sources, logiris.Name) || hasSource(ar.sources, vacance.Name) {
		t.Errorf("both present: sources=%v, want logiris (not vacance)", ar.sources)
	}

	// logiris empty → falls back to commune vacance.
	d = dossier(
		okResult(vacance.Name, &vacance.Result{VacancyRate: 9, Confidence: "high"}),
		okResult(logiris.Name, &logiris.Result{}), // IsEmpty (TotalLogements 0)
	)
	ar = scoreTension(d)
	if !ar.present || !hasSource(ar.sources, vacance.Name) {
		t.Errorf("logiris empty: sources=%v, want vacance fallback", ar.sources)
	}
}

func hasSource(srcs []string, name string) bool {
	for _, s := range srcs {
		if s == name {
			return true
		}
	}
	return false
}
