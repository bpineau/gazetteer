package zonescore

import (
	"testing"

	"github.com/bpineau/gazetteer/sources/dvf"
	"github.com/bpineau/gazetteer/sources/oll"
	gzosm "github.com/bpineau/gazetteer/sources/osm"
)

func TestWeightsForProfile(t *testing.T) {
	t.Parallel()
	for _, name := range ProfileNames() {
		w, ok := WeightsForProfile(name)
		if !ok || len(w) != len(DefaultWeights) {
			t.Errorf("profile %q: ok=%v, %d weights, want all 6 axes", name, ok, len(w))
		}
	}
	if _, ok := WeightsForProfile("nope"); ok {
		t.Error("unknown profile returned ok=true")
	}
	// The yield profile IS the default thesis.
	if w, _ := WeightsForProfile(ProfileYield); w[AxisRendement] != DefaultWeights[AxisRendement] {
		t.Error("yield profile should equal DefaultWeights")
	}
	// The transport profile up-weights access vs the default.
	if w, _ := WeightsForProfile(ProfileTransport); w[AxisAcces] <= DefaultWeights[AxisAcces] {
		t.Errorf("transport profile acces weight %.2f, want > default %.2f", w[AxisAcces], DefaultWeights[AxisAcces])
	}
}

// TestProfile_ShiftsComposite confirms a profile actually changes the score:
// a low-yield / high-access zone scores higher under `transport` than under
// the yield-first default.
func TestProfile_ShiftsComposite(t *testing.T) {
	t.Parallel()
	// A poor 2.4 % gross yield (price 8000 / rent 16) but excellent transit
	// (4 min to a station). The yield-first default punishes the low yield;
	// the transport profile, weighting access far higher, rewards the zone.
	d := dossier(
		okResult(dvf.Name, &dvf.Result{ValueEURPerM2Cents: new(int64(800000)), SampleSize: 20}),
		okResult(oll.Name, &oll.Result{ObservedMedianEURPerM2: 16, SampleSize: 100, Confidence: "high"}),
		okResult(gzosm.Name, &gzosm.Result{NearestTransitWalkMin: 4, NearestTransitType: "metro", NearestTransitName: "X"}),
	)
	def := Compute(d)
	tr := Compute(d, Options{Weights: Personas[ProfileTransport]})
	if tr.Composite <= def.Composite {
		t.Errorf("transport composite %.1f should exceed default %.1f for a low-yield/high-tension zone",
			tr.Composite, def.Composite)
	}
}
