package appraisal

import (
	"testing"
	"time"

	"github.com/bpineau/gazetteer"
)

// fakePriceEstimator is a minimal Result.Data type that satisfies
// PriceEstimator. Used by the tests so they don't depend on real Sources.
type fakePriceEstimator struct {
	eurPerM2Cents int64
	confidence    Confidence
	sampleSize    int
	method        string
}

func (f fakePriceEstimator) PriceEstimate() PriceEstimate {
	return PriceEstimate{
		EurPerM2Cents: f.eurPerM2Cents,
		Confidence:    f.confidence,
		SampleSize:    f.sampleSize,
		Method:        f.method,
	}
}

// nonPriceData satisfies neither PriceEstimator nor anything else; it
// exists to verify that non-implementing entries are silently skipped.
type nonPriceData struct{}

// buildDossier is a tiny factory: each entry yields a Result whose Data
// is the value passed and Status is StatusOK (unless overridden).
type fakeEntry struct {
	data   any
	status gazetteer.Status
}

func buildDossier(entries map[string]fakeEntry) gazetteer.Dossier {
	d := gazetteer.Dossier{
		Results:   make(map[string]gazetteer.Result, len(entries)),
		StartedAt: time.Now(),
	}
	for name, e := range entries {
		// Status zero value (== gazetteer.StatusOK) is the natural default
		// for entries that supply Data; tests override only when exercising
		// failure paths.
		d.Results[name] = gazetteer.Result{
			Name:   name,
			Status: e.status,
			Data:   e.data,
		}
	}
	return d
}

func TestPricePerM2_SingleSource(t *testing.T) {
	t.Parallel()

	d := buildDossier(map[string]fakeEntry{
		"dvf": {data: fakePriceEstimator{eurPerM2Cents: 5_000_00, confidence: ConfidenceHigh}},
	})

	got := PricePerM2(d)
	// MinSources defaults to 2, so a single source can't be High.
	if got.EurPerM2Cents != 5_000_00 {
		t.Errorf("EurPerM2Cents = %d, want %d", got.EurPerM2Cents, 5_000_00)
	}
	if got.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want Low (single source below MinSources=2)", got.Confidence)
	}
	if len(got.Inputs) != 1 {
		t.Errorf("Inputs len = %d, want 1", len(got.Inputs))
	}
	if got.Inputs[0].Source != "dvf" {
		t.Errorf("Inputs[0].Source = %q, want \"dvf\"", got.Inputs[0].Source)
	}
}

func TestPricePerM2_MultipleSourcesEqualWeights(t *testing.T) {
	t.Parallel()

	// Three sources at 4000 / 5000 / 6000 €/m², equal weights → 5000.
	d := buildDossier(map[string]fakeEntry{
		"src_a": {data: fakePriceEstimator{eurPerM2Cents: 4_000_00, confidence: ConfidenceMedium}},
		"src_b": {data: fakePriceEstimator{eurPerM2Cents: 5_000_00, confidence: ConfidenceMedium}},
		"src_c": {data: fakePriceEstimator{eurPerM2Cents: 6_000_00, confidence: ConfidenceMedium}},
	})

	opts := PriceOptions{
		Weights: map[string]float64{
			"src_a": 1.0,
			"src_b": 1.0,
			"src_c": 1.0,
		},
	}
	got := PricePerM2(d, opts)
	if got.EurPerM2Cents != 5_000_00 {
		t.Errorf("EurPerM2Cents = %d, want %d (equal-weighted mean)", got.EurPerM2Cents, 5_000_00)
	}
	if got.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %v, want Medium", got.Confidence)
	}
}

func TestPricePerM2_CustomWeights(t *testing.T) {
	t.Parallel()

	// 4000 with weight 1, 6000 with weight 3 → (4000 + 18000) / 4 = 5500.
	d := buildDossier(map[string]fakeEntry{
		"cheap": {data: fakePriceEstimator{eurPerM2Cents: 4_000_00, confidence: ConfidenceMedium}},
		"rich":  {data: fakePriceEstimator{eurPerM2Cents: 6_000_00, confidence: ConfidenceMedium}},
	})

	opts := PriceOptions{
		Weights: map[string]float64{
			"cheap": 1.0,
			"rich":  3.0,
		},
	}
	got := PricePerM2(d, opts)
	if got.EurPerM2Cents != 5_500_00 {
		t.Errorf("EurPerM2Cents = %d, want %d (weighted mean 1:3)", got.EurPerM2Cents, 5_500_00)
	}
}

func TestPricePerM2_DefaultWeightsByName(t *testing.T) {
	t.Parallel()

	// DefaultPriceWeights: meilleursagents=1.0, dvf=0.9.
	// MA at 5000 (w=1.0), dvf at 6000 (w=0.9) →
	// (5000*1.0 + 6000*0.9) / (1.0+0.9) = 10400/1.9 ≈ 5473.68 €/m² → 547368 cents.
	d := buildDossier(map[string]fakeEntry{
		"meilleursagents": {data: fakePriceEstimator{eurPerM2Cents: 5_000_00, confidence: ConfidenceHigh}},
		"dvf":             {data: fakePriceEstimator{eurPerM2Cents: 6_000_00, confidence: ConfidenceHigh}},
	})

	got := PricePerM2(d)
	wantF := (float64(5_000_00)*1.0 + float64(6_000_00)*0.9) / 1.9
	want := int64(wantF)
	if got.EurPerM2Cents != want {
		t.Errorf("EurPerM2Cents = %d, want %d (default weights MA=1.0/dvf=0.9)", got.EurPerM2Cents, want)
	}
	if got.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %v, want High (two high-conf sources, meets MinSources)", got.Confidence)
	}
}

func TestPricePerM2_UnknownSourceUsesDefaultWeight(t *testing.T) {
	t.Parallel()

	// "mystery" is unknown — falls back to opts.DefaultWeight (0.4 default).
	// dvf at 5000 (w=0.9), mystery at 9000 (w=0.4) →
	// (5000*0.9 + 9000*0.4) / 1.3 = 8100/1.3 ≈ 6230.77 €/m².
	d := buildDossier(map[string]fakeEntry{
		"dvf":     {data: fakePriceEstimator{eurPerM2Cents: 5_000_00, confidence: ConfidenceMedium}},
		"mystery": {data: fakePriceEstimator{eurPerM2Cents: 9_000_00, confidence: ConfidenceLow}},
	})

	got := PricePerM2(d)
	wantF := (float64(5_000_00)*0.9 + float64(9_000_00)*0.4) / 1.3
	want := int64(wantF)
	if got.EurPerM2Cents != want {
		t.Errorf("EurPerM2Cents = %d, want %d (default-weight fallback)", got.EurPerM2Cents, want)
	}
	// Check the unknown source got the default weight in the Inputs slice.
	var mysteryInput *PriceInput
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

func TestPricePerM2_BelowMinSourcesDropsConfidence(t *testing.T) {
	t.Parallel()

	d := buildDossier(map[string]fakeEntry{
		"dvf": {data: fakePriceEstimator{eurPerM2Cents: 5_000_00, confidence: ConfidenceHigh}},
	})

	got := PricePerM2(d, PriceOptions{MinSources: 3})
	if got.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want Low (1 source, MinSources=3)", got.Confidence)
	}
	if got.EurPerM2Cents != 5_000_00 {
		t.Errorf("EurPerM2Cents = %d, want %d (value still computed)", got.EurPerM2Cents, 5_000_00)
	}
}

func TestPricePerM2_NoEstimators(t *testing.T) {
	t.Parallel()

	d := buildDossier(map[string]fakeEntry{
		"georisques": {data: nonPriceData{}},
		"ademe":      {data: nonPriceData{}},
	})

	got := PricePerM2(d)
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

func TestPricePerM2_OutlierRejection(t *testing.T) {
	t.Parallel()

	// Four sources tightly clustered around 5000 + one wild outlier at 50000.
	// MAD-based z-score should flag 50000 as an outlier.
	d := buildDossier(map[string]fakeEntry{
		"src_a": {data: fakePriceEstimator{eurPerM2Cents: 4_900_00, confidence: ConfidenceMedium}},
		"src_b": {data: fakePriceEstimator{eurPerM2Cents: 5_000_00, confidence: ConfidenceMedium}},
		"src_c": {data: fakePriceEstimator{eurPerM2Cents: 5_100_00, confidence: ConfidenceMedium}},
		"src_d": {data: fakePriceEstimator{eurPerM2Cents: 5_050_00, confidence: ConfidenceMedium}},
		"crazy": {data: fakePriceEstimator{eurPerM2Cents: 50_000_00, confidence: ConfidenceMedium}},
	})

	// Equal weights to keep arithmetic checkable.
	opts := PriceOptions{
		Weights: map[string]float64{
			"src_a": 1.0,
			"src_b": 1.0,
			"src_c": 1.0,
			"src_d": 1.0,
			"crazy": 1.0,
		},
	}
	got := PricePerM2(d, opts)

	var crazy *PriceInput
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

	// Mean must reflect the 4 sane sources only: (4900+5000+5100+5050)/4 = 5012.5
	wantMean := int64((4_900_00 + 5_000_00 + 5_100_00 + 5_050_00) / 4)
	if got.EurPerM2Cents != wantMean {
		t.Errorf("EurPerM2Cents = %d, want %d (outlier excluded)", got.EurPerM2Cents, wantMean)
	}
}

func TestPricePerM2_EmptyDossier(t *testing.T) {
	t.Parallel()

	d := gazetteer.Dossier{Results: map[string]gazetteer.Result{}}
	got := PricePerM2(d)
	if got.EurPerM2Cents != 0 {
		t.Errorf("EurPerM2Cents = %d, want 0", got.EurPerM2Cents)
	}
	if got.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want Low", got.Confidence)
	}
	if len(got.Inputs) != 0 {
		t.Errorf("Inputs len = %d, want 0", len(got.Inputs))
	}
}

func TestPricePerM2_FailedSourcesSkipped(t *testing.T) {
	t.Parallel()

	d := buildDossier(map[string]fakeEntry{
		"dvf": {
			data:   fakePriceEstimator{eurPerM2Cents: 5_000_00, confidence: ConfidenceHigh},
			status: gazetteer.StatusOK,
		},
		"meilleursagents": {
			data:   fakePriceEstimator{eurPerM2Cents: 9_999_99, confidence: ConfidenceHigh},
			status: gazetteer.StatusFailedAntiBot,
		},
		"pappersimmo": {
			data:   fakePriceEstimator{eurPerM2Cents: 8_888_88, confidence: ConfidenceHigh},
			status: gazetteer.StatusFailedTransient,
		},
	})

	got := PricePerM2(d)
	if got.EurPerM2Cents != 5_000_00 {
		t.Errorf("EurPerM2Cents = %d, want %d (failed sources skipped)", got.EurPerM2Cents, 5_000_00)
	}
	if len(got.Inputs) != 1 {
		t.Errorf("Inputs len = %d, want 1 (only dvf kept)", len(got.Inputs))
	}
}

func TestPricePerM2_StatusOKEmptyKept(t *testing.T) {
	t.Parallel()

	// StatusOKEmpty should still be considered if Data implements
	// PriceEstimator — the typed payload itself decides what to return.
	d := buildDossier(map[string]fakeEntry{
		"dvf": {
			data:   fakePriceEstimator{eurPerM2Cents: 5_000_00, confidence: ConfidenceMedium},
			status: gazetteer.StatusOKEmpty,
		},
		"meilleursagents": {
			data:   fakePriceEstimator{eurPerM2Cents: 5_500_00, confidence: ConfidenceMedium},
			status: gazetteer.StatusOK,
		},
	})

	got := PricePerM2(d)
	if len(got.Inputs) != 2 {
		t.Errorf("Inputs len = %d, want 2 (StatusOKEmpty kept)", len(got.Inputs))
	}
}

func TestPricePerM2_CustomWeightsOverrideDefaults(t *testing.T) {
	t.Parallel()

	// Caller's Weights map should override DefaultPriceWeights.
	d := buildDossier(map[string]fakeEntry{
		"dvf":             {data: fakePriceEstimator{eurPerM2Cents: 5_000_00, confidence: ConfidenceMedium}},
		"meilleursagents": {data: fakePriceEstimator{eurPerM2Cents: 6_000_00, confidence: ConfidenceMedium}},
	})

	opts := PriceOptions{
		Weights: map[string]float64{
			"dvf":             2.0,
			"meilleursagents": 1.0,
		},
	}
	got := PricePerM2(d, opts)
	// (5000*2 + 6000*1) / 3 = 16000/3 ≈ 5333.33
	wantF := (float64(5_000_00)*2.0 + float64(6_000_00)*1.0) / 3.0
	want := int64(wantF)
	if got.EurPerM2Cents != want {
		t.Errorf("EurPerM2Cents = %d, want %d (custom weights override defaults)", got.EurPerM2Cents, want)
	}
}

// Compile-time check: fakePriceEstimator must satisfy PriceEstimator.
var _ PriceEstimator = fakePriceEstimator{}
