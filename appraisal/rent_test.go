package appraisal

import (
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// fakeRentEstimator is a minimal Result.Data type that satisfies
// RentEstimator. Used by the tests so they don't depend on real Sources.
type fakeRentEstimator struct {
	eurPerM2Cents int64
	confidence    Confidence
	bracket       string
	method        string
}

func (f fakeRentEstimator) RentEstimate() RentEstimate {
	return RentEstimate{
		EurPerM2Cents: f.eurPerM2Cents,
		Confidence:    f.confidence,
		Bracket:       f.bracket,
		Method:        f.method,
	}
}

// nonRentData satisfies neither RentEstimator nor anything else; it
// exists to verify that non-implementing entries are silently skipped.
type nonRentData struct{}

// Compile-time check: fakeRentEstimator must satisfy RentEstimator.
var _ RentEstimator = fakeRentEstimator{}

func TestRentValue_SingleSource(t *testing.T) {
	t.Parallel()

	// MinSources defaults to 1, so a single source CAN reach Medium /
	// High based on its per-input Confidence.
	d := buildDossier(map[string]fakeEntry{
		"encadrement": {data: fakeRentEstimator{eurPerM2Cents: 30_00, confidence: ConfidenceMedium}},
	})

	got := RentValue(d)
	if got.EurPerM2Cents != 30_00 {
		t.Errorf("EurPerM2Cents = %d, want %d", got.EurPerM2Cents, 30_00)
	}
	if got.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %v, want Medium (single source, MinSources=1, Medium per-input)", got.Confidence)
	}
	if len(got.Inputs) != 1 {
		t.Errorf("Inputs len = %d, want 1", len(got.Inputs))
	}
	if got.Inputs[0].Source != "encadrement" {
		t.Errorf("Inputs[0].Source = %q, want %q", got.Inputs[0].Source, "encadrement")
	}
}

func TestRentValue_MultipleSourcesWeighted(t *testing.T) {
	t.Parallel()

	// DefaultRentWeights: encadrement=1.0, carteloyers=0.9.
	// encadrement at 30 (w=1.0), carteloyers at 28 (w=0.9) →
	// (3000*1.0 + 2800*0.9) / 1.9 = 5520/1.9 ≈ 2905.26 cents.
	d := buildDossier(map[string]fakeEntry{
		"encadrement": {data: fakeRentEstimator{eurPerM2Cents: 30_00, confidence: ConfidenceMedium}},
		"carteloyers": {data: fakeRentEstimator{eurPerM2Cents: 28_00, confidence: ConfidenceHigh}},
	})

	got := RentValue(d)
	wantF := (float64(30_00)*1.0 + float64(28_00)*0.9) / 1.9
	want := int64(wantF)
	if got.EurPerM2Cents != want {
		t.Errorf("EurPerM2Cents = %d, want %d (default weights enc=1.0/carte=0.9)", got.EurPerM2Cents, want)
	}
	// avg conf = (Medium=1 + High=2)/2 = 1.5 → High by computeRentConfidence.
	if got.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %v, want High (avg conf = 1.5)", got.Confidence)
	}
}

func TestRentValue_CustomWeightsOverrideDefaults(t *testing.T) {
	t.Parallel()

	// Caller's Weights map should override DefaultRentWeights.
	d := buildDossier(map[string]fakeEntry{
		"encadrement": {data: fakeRentEstimator{eurPerM2Cents: 30_00, confidence: ConfidenceMedium}},
		"carteloyers": {data: fakeRentEstimator{eurPerM2Cents: 28_00, confidence: ConfidenceMedium}},
	})

	opts := RentOptions{
		Weights: map[string]float64{
			"encadrement": 1.0,
			"carteloyers": 3.0,
		},
	}
	got := RentValue(d, opts)
	// (3000*1 + 2800*3) / 4 = 11400/4 = 2850 cents.
	wantF := (float64(30_00)*1.0 + float64(28_00)*3.0) / 4.0
	want := int64(wantF)
	if got.EurPerM2Cents != want {
		t.Errorf("EurPerM2Cents = %d, want %d (custom weights override defaults)", got.EurPerM2Cents, want)
	}
}

func TestRentValue_BelowMinSourcesDropsConfidence(t *testing.T) {
	t.Parallel()

	d := buildDossier(map[string]fakeEntry{
		"encadrement": {data: fakeRentEstimator{eurPerM2Cents: 30_00, confidence: ConfidenceHigh}},
	})

	got := RentValue(d, RentOptions{MinSources: 3})
	if got.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want Low (1 source, MinSources=3)", got.Confidence)
	}
	if got.EurPerM2Cents != 30_00 {
		t.Errorf("EurPerM2Cents = %d, want %d (value still computed)", got.EurPerM2Cents, 30_00)
	}
}

func TestRentValue_NoEstimators(t *testing.T) {
	t.Parallel()

	d := buildDossier(map[string]fakeEntry{
		"georisques": {data: nonRentData{}},
		"ademe":      {data: nonRentData{}},
	})

	got := RentValue(d)
	if got.EurPerM2Cents != 0 {
		t.Errorf("EurPerM2Cents = %d, want 0 (no implementers)", got.EurPerM2Cents)
	}
	if got.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want Low", got.Confidence)
	}
	if len(got.Inputs) != 0 {
		t.Errorf("Inputs len = %d, want 0", len(got.Inputs))
	}
}

func TestRentValue_OutlierRejection(t *testing.T) {
	t.Parallel()

	// Four sources tightly clustered around 28 + one wild outlier at 200.
	// MAD-based z-score should flag the 200 as an outlier.
	d := buildDossier(map[string]fakeEntry{
		"src_a": {data: fakeRentEstimator{eurPerM2Cents: 27_50, confidence: ConfidenceMedium}},
		"src_b": {data: fakeRentEstimator{eurPerM2Cents: 28_00, confidence: ConfidenceMedium}},
		"src_c": {data: fakeRentEstimator{eurPerM2Cents: 28_50, confidence: ConfidenceMedium}},
		"src_d": {data: fakeRentEstimator{eurPerM2Cents: 29_00, confidence: ConfidenceMedium}},
		"crazy": {data: fakeRentEstimator{eurPerM2Cents: 200_00, confidence: ConfidenceMedium}},
	})

	// Equal weights to keep arithmetic checkable.
	opts := RentOptions{
		Weights: map[string]float64{
			"src_a": 1.0,
			"src_b": 1.0,
			"src_c": 1.0,
			"src_d": 1.0,
			"crazy": 1.0,
		},
	}
	got := RentValue(d, opts)

	var crazy *RentInput
	for i := range got.Inputs {
		if got.Inputs[i].Source == "crazy" {
			crazy = &got.Inputs[i]
			break
		}
	}
	if crazy == nil {
		t.Fatal("crazy source missing from Inputs")
	}
	if !crazy.Excluded {
		t.Errorf("crazy.Excluded = false, want true")
	}
	if crazy.ExcludedWhy != "outlier_z_score" {
		t.Errorf("crazy.ExcludedWhy = %q, want %q", crazy.ExcludedWhy, "outlier_z_score")
	}

	// Mean reflects the 4 sane sources only: (2750+2800+2850+2900)/4 = 2825.
	wantMean := int64((27_50 + 28_00 + 28_50 + 29_00) / 4)
	if got.EurPerM2Cents != wantMean {
		t.Errorf("EurPerM2Cents = %d, want %d (outlier excluded)", got.EurPerM2Cents, wantMean)
	}
}

func TestRentValue_BracketPropagation(t *testing.T) {
	t.Parallel()

	// encadrement supplies a Bracket; carteloyers does not. The
	// consolidated Bracket should match encadrement's (sorted-name
	// iteration: "carteloyers" then "encadrement", but the first
	// NON-EMPTY Bracket wins).
	d := buildDossier(map[string]fakeEntry{
		"carteloyers": {data: fakeRentEstimator{eurPerM2Cents: 28_00, confidence: ConfidenceMedium}},
		"encadrement": {data: fakeRentEstimator{eurPerM2Cents: 30_00, confidence: ConfidenceMedium, bracket: "paris_zone_3"}},
	})

	got := RentValue(d)
	if got.Bracket != "paris_zone_3" {
		t.Errorf("Bracket = %q, want %q", got.Bracket, "paris_zone_3")
	}
}

func TestRentValue_BracketFirstSortedNameWins(t *testing.T) {
	t.Parallel()

	// Two sources both supply a Bracket. The first (in sorted-name
	// order) wins. "a_source" < "z_source" alphabetically.
	d := buildDossier(map[string]fakeEntry{
		"a_source": {data: fakeRentEstimator{eurPerM2Cents: 28_00, bracket: "bracket_alpha"}},
		"z_source": {data: fakeRentEstimator{eurPerM2Cents: 30_00, bracket: "bracket_zeta"}},
	})

	got := RentValue(d)
	if got.Bracket != "bracket_alpha" {
		t.Errorf("Bracket = %q, want %q (first-in-sorted-name-order wins)", got.Bracket, "bracket_alpha")
	}
}

func TestRentValue_EmptyDossier(t *testing.T) {
	t.Parallel()

	d := gazetteer.Dossier{Results: map[string]gazetteer.Result{}}
	got := RentValue(d)
	if got.EurPerM2Cents != 0 {
		t.Errorf("EurPerM2Cents = %d, want 0", got.EurPerM2Cents)
	}
	if got.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want Low", got.Confidence)
	}
	if len(got.Inputs) != 0 {
		t.Errorf("Inputs len = %d, want 0", len(got.Inputs))
	}
	if got.Bracket != "" {
		t.Errorf("Bracket = %q, want empty", got.Bracket)
	}
}

func TestRentValue_FailedSourcesSkipped(t *testing.T) {
	t.Parallel()

	d := buildDossier(map[string]fakeEntry{
		"encadrement": {
			data:   fakeRentEstimator{eurPerM2Cents: 30_00, confidence: ConfidenceMedium},
			status: gazetteer.StatusOK,
		},
		"carteloyers": {
			data:   fakeRentEstimator{eurPerM2Cents: 99_99, confidence: ConfidenceHigh},
			status: gazetteer.StatusFailedAntiBot,
		},
		"broken": {
			data:   fakeRentEstimator{eurPerM2Cents: 88_88, confidence: ConfidenceHigh},
			status: gazetteer.StatusFailedTransient,
		},
	})

	got := RentValue(d)
	if got.EurPerM2Cents != 30_00 {
		t.Errorf("EurPerM2Cents = %d, want %d (failed sources skipped)", got.EurPerM2Cents, 30_00)
	}
	if len(got.Inputs) != 1 {
		t.Errorf("Inputs len = %d, want 1 (only encadrement kept)", len(got.Inputs))
	}
}

func TestRentValue_StatusOKEmptyKept(t *testing.T) {
	t.Parallel()

	// StatusOKEmpty should still be considered if Data implements
	// RentEstimator — the typed payload itself decides what to return.
	d := buildDossier(map[string]fakeEntry{
		"encadrement": {
			data:   fakeRentEstimator{eurPerM2Cents: 30_00, confidence: ConfidenceMedium},
			status: gazetteer.StatusOKEmpty,
		},
		"carteloyers": {
			data:   fakeRentEstimator{eurPerM2Cents: 28_00, confidence: ConfidenceMedium},
			status: gazetteer.StatusOK,
		},
	})

	got := RentValue(d)
	if len(got.Inputs) != 2 {
		t.Errorf("Inputs len = %d, want 2 (StatusOKEmpty kept)", len(got.Inputs))
	}
}

func TestRentValue_UnknownSourceUsesDefaultWeight(t *testing.T) {
	t.Parallel()

	// "mystery" is unknown — falls back to opts.DefaultWeight (0.4 default).
	// encadrement at 30 (w=1.0), mystery at 50 (w=0.4) →
	// (3000*1.0 + 5000*0.4) / 1.4 = 5000/1.4 ≈ 3571.43 cents.
	d := buildDossier(map[string]fakeEntry{
		"encadrement": {data: fakeRentEstimator{eurPerM2Cents: 30_00, confidence: ConfidenceMedium}},
		"mystery":     {data: fakeRentEstimator{eurPerM2Cents: 50_00, confidence: ConfidenceLow}},
	})

	got := RentValue(d)
	wantF := (float64(30_00)*1.0 + float64(50_00)*0.4) / 1.4
	want := int64(wantF)
	if got.EurPerM2Cents != want {
		t.Errorf("EurPerM2Cents = %d, want %d (default-weight fallback)", got.EurPerM2Cents, want)
	}
	// Check the unknown source got the default weight in the Inputs slice.
	var mysteryInput *RentInput
	for i := range got.Inputs {
		if got.Inputs[i].Source == "mystery" {
			mysteryInput = &got.Inputs[i]
			break
		}
	}
	if mysteryInput == nil {
		t.Fatal("mystery source missing from Inputs")
	}
	if mysteryInput.Weight != 0.4 {
		t.Errorf("mystery weight = %v, want 0.4 (default)", mysteryInput.Weight)
	}
}
